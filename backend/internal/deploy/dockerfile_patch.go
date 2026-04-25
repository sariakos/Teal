package deploy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// reservedBuildEnvKeys is the small denylist of env-var names Teal
// will NOT inject as ARG/ENV during build. They're either:
//
//   - framework-controlled (NODE_ENV is the canonical example: Next/
//     Vite/Node interpret a non-standard value as "not production"
//     and degrade — Next.js falls back to its Pages-Router /_error
//     template, which breaks the build with the misleading "<Html>
//     should not be imported outside of pages/_document" error)
//   - container-runtime-controlled (PORT, HOSTNAME, USER are set by
//     Docker / the orchestrator; overriding via ENV at build time
//     can break the runtime image)
//   - shell-internals (PATH, HOME, SHELL — wrong values brick the
//     build container)
//
// These vars are still written to deploy.env (so the running
// containers see them at compose `environment:` resolution time),
// they're just not passed as build args.
var reservedBuildEnvKeys = map[string]struct{}{
	"NODE_ENV": {},
	"PATH":     {},
	"HOME":     {},
	"SHELL":    {},
	"USER":     {},
	"HOSTNAME": {},
	"PORT":     {},
}

// filterStandardBuildEnv returns keys minus reservedBuildEnvKeys.
// Stable order (input is already sorted by hydrateEnv).
func filterStandardBuildEnv(keys []string) []string {
	out := keys[:0:0]
	for _, k := range keys {
		if _, reserved := reservedBuildEnvKeys[k]; reserved {
			continue
		}
		out = append(out, k)
	}
	return out
}

// composeServicesMap re-parses a rendered compose YAML and returns
// the top-level services: map. Returns an error if the YAML is
// malformed or has no services key. Used by the engine to walk
// services for Dockerfile patching without dragging the parsed
// compose document through Transform's return.
func composeServicesMap(rendered string) (map[string]any, error) {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &root); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	svcs, ok := root["services"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("services: missing or wrong shape")
	}
	return svcs, nil
}

// patchDockerfilesForArgs walks every service in the transformed
// compose YAML's services map, finds each service's build context +
// Dockerfile, and rewrites the Dockerfile to declare `ARG <KEY>` and
// `ENV <KEY>=$<KEY>` after every `FROM` directive for each key.
//
// This is the second half of the build-time env-var delivery
// machinery. compose.injectBuildArgs adds `build.args:` to each
// service so docker compose passes --build-arg KEY=value at build
// time, but those values only land in the build container's env if
// the Dockerfile DECLARES `ARG KEY`. Most user Dockerfiles don't
// (and shouldn't have to) declare ARGs for every Teal-managed env
// var. We patch them in transparently.
//
// Important: the patches live inside the workdir's checkout dir, NOT
// the user's git repo. The checkout is re-cloned on every deploy, so
// the original Dockerfile is never modified on disk in any persistent
// location.
//
// Returns the list of (service, dockerfilePath) pairs we patched, for
// the deploy log.
func patchDockerfilesForArgs(projectDir string, services map[string]any, keys []string) ([]string, error) {
	if projectDir == "" || len(keys) == 0 {
		return nil, nil
	}
	sort.Strings(keys)
	patched := []string{}
	for name, raw := range services {
		svc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ctx, dockerfile := buildPaths(svc)
		if ctx == "" {
			continue
		}
		// Resolve relative to projectDir (which is the dir containing
		// the compose file, matching docker compose's own resolution).
		absCtx := filepath.Join(projectDir, ctx)
		absDockerfile := filepath.Join(absCtx, dockerfile)
		// If build.dockerfile is absolute or escapes the build context
		// (e.g. "../shared/Dockerfile"), filepath.Join still works.
		// Verify the file exists before trying to patch — silently skip
		// otherwise so a bad config doesn't break the deploy.
		info, err := os.Stat(absDockerfile)
		if err != nil || info.IsDir() {
			continue
		}
		if err := patchDockerfile(absDockerfile, keys); err != nil {
			return patched, fmt.Errorf("patch %s (%s): %w", name, absDockerfile, err)
		}
		// report path relative to projectDir for log readability
		rel, rerr := filepath.Rel(projectDir, absDockerfile)
		if rerr != nil {
			rel = absDockerfile
		}
		patched = append(patched, rel)
	}
	sort.Strings(patched)
	return patched, nil
}

// buildPaths returns (build context, Dockerfile filename) for a
// service. Handles both shorthand ("build: ./app") and long form
// ("build: { context: ./app, dockerfile: Dockerfile.prod }"). Returns
// ("", "") when the service has no build directive.
func buildPaths(svc map[string]any) (ctx, dockerfile string) {
	raw, ok := svc["build"]
	if !ok {
		return "", ""
	}
	switch b := raw.(type) {
	case string:
		return b, "Dockerfile"
	case map[string]any:
		if c, ok := b["context"].(string); ok {
			ctx = c
		}
		if d, ok := b["dockerfile"].(string); ok {
			dockerfile = d
		} else {
			dockerfile = "Dockerfile"
		}
		return ctx, dockerfile
	}
	return "", ""
}

// patchDockerfile rewrites a Dockerfile so every FROM stage declares
// an ARG + ENV mapping for each key. Idempotent: if a stage already
// has the exact ARG line we'd add, we skip it (so re-patching a
// previously-patched file is a no-op).
//
// The patch goes immediately after each `FROM` line so the ARGs are
// scoped to that stage (Docker treats stages as independent — ARGs
// declared before any FROM only apply to FROMs themselves, not to
// subsequent RUNs).
func patchDockerfile(path string, keys []string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	patched := injectArgsIntoDockerfile(string(src), keys)
	if patched == string(src) {
		return nil // nothing changed (fully idempotent or no FROM lines)
	}
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// injectArgsIntoDockerfile is the pure-string core of the patch step
// so we can unit-test without touching the filesystem.
func injectArgsIntoDockerfile(src string, keys []string) string {
	lines := splitLines(src)
	// Pre-compute the patch block we'll insert after each FROM.
	var patchBlock strings.Builder
	patchBlock.WriteString("# teal-managed: ARG/ENV exposing platform env vars to RUN steps\n")
	for _, k := range keys {
		patchBlock.WriteString("ARG ")
		patchBlock.WriteString(k)
		patchBlock.WriteString("\n")
		patchBlock.WriteString("ENV ")
		patchBlock.WriteString(k)
		patchBlock.WriteString("=$")
		patchBlock.WriteString(k)
		patchBlock.WriteString("\n")
	}

	var out strings.Builder
	for i, line := range lines {
		out.WriteString(line)
		if i < len(lines)-1 {
			out.WriteString("\n")
		}
		if !isFromLine(line) {
			continue
		}
		// Skip if the next non-blank line is already our marker —
		// makes the patch idempotent when re-running on the same file.
		if alreadyPatched(lines, i+1) {
			continue
		}
		// Insert after FROM (a newline was already written above, so
		// the patch block lands on its own lines).
		if i == len(lines)-1 {
			out.WriteString("\n")
		}
		out.WriteString(patchBlock.String())
	}
	return out.String()
}

// splitLines splits on \n preserving empties; reverse of strings.Join
// with "\n". Doesn't handle \r\n specifically — Dockerfiles in the
// wild are LF.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	hasTrailingNL := strings.HasSuffix(s, "\n")
	parts := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if hasTrailingNL {
		parts = append(parts, "")
	}
	return parts
}

// isFromLine reports whether a Dockerfile line is a FROM directive.
// Tolerates leading whitespace and case (Docker's parser is
// case-insensitive on instructions).
func isFromLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 5 {
		return false
	}
	upper := strings.ToUpper(trimmed[:4])
	if upper != "FROM" {
		return false
	}
	// Next char must be whitespace to disambiguate from words like FROMAGE.
	c := trimmed[4]
	return c == ' ' || c == '\t'
}

// alreadyPatched returns true when the next non-blank line at or
// after start is the teal-managed marker we'd otherwise insert.
func alreadyPatched(lines []string, start int) bool {
	for i := start; i < len(lines); i++ {
		s := strings.TrimSpace(lines[i])
		if s == "" {
			continue
		}
		return s == "# teal-managed: ARG/ENV exposing platform env vars to RUN steps"
	}
	return false
}

// scanDockerfileFROMs is exported for tests and diagnostics — counts
// the FROM stages in a Dockerfile so we can report "patched N stages"
// in the deploy log.
func scanDockerfileFROMs(src string) int {
	count := 0
	scan := bufio.NewScanner(strings.NewReader(src))
	for scan.Scan() {
		if isFromLine(scan.Text()) {
			count++
		}
	}
	return count
}
