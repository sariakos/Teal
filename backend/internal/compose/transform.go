package compose

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sariakos/teal/backend/internal/domain"
)

// Labels Teal injects on the primary service so the deploy engine can find
// the right container later via Docker label filters. Names are namespaced
// with "teal." so they cannot collide with user labels.
const (
	LabelApp   = "teal.app"
	LabelColor = "teal.color"
	LabelRole  = "teal.role"

	// LabelPrimary, when set on a user service to "true", overrides the
	// "first service with ports:" heuristic for primary-service detection.
	// Documented for users who have multiple ports-bearing services and need
	// to disambiguate.
	LabelPrimary = "teal.primary"

	RolePrimary = "primary"
)

// PlatformNetworkAlias is the network name attached to routed services. The
// constant lives in package traefik but is duplicated here as a string to
// avoid a dependency from compose -> traefik (the latter knows about Docker;
// compose stays pure YAML).
const PlatformNetworkAlias = "platform_proxy"

// TransformInput bundles the inputs to Transform so the call site stays
// explicit and the function can grow new options without breaking callers.
type TransformInput struct {
	UserYAML    string
	AppSlug     string
	Color       domain.Color
	Domains     []string // may be empty if the App has no routing yet
	EnvFilePath string   // path relative to the working dir (e.g. "deploy.env"); empty disables injection

	// CPULimit / MemoryLimit are Compose-style resource limits applied
	// to EVERY service in the rendered output. Per spec: per-app limits
	// (not per-service) — the platform owner sets one budget for the
	// whole app rather than reasoning about each sidecar separately.
	// Empty values skip injection for that dimension.
	CPULimit    string // e.g. "0.5", "2"
	MemoryLimit string // e.g. "512m", "1g"

	// AttachServices is the list of compose service names that need to
	// be on the platform_proxy network so Traefik can reach them. The
	// per-service routing flow passes one entry per Route. When this is
	// set, the primary-service heuristic is skipped — each named
	// service gets attached + labelled directly.
	AttachServices []string

	// BuildArgs lists every env-var key the engine wants surfaced as a
	// `build.args:` entry on every service that has a `build:` block.
	// The value is set to "${KEY}" so docker compose interpolates it
	// from --env-file at build time. Combined with Dockerfile-side ARG
	// declarations (handled by the deploy package), this makes the
	// user's app see Teal-managed env vars at build time too — not just
	// at runtime.
	BuildArgs []string
}

// TransformOutput is what the caller needs to set up Traefik and inspect
// the result later.
type TransformOutput struct {
	YAML            string
	PrimaryService  string // service name in the compose file; empty if Domains is empty
	PortInContainer int    // primary container's port to route to (best-effort; 0 if undetectable)
	Warnings        []Warning
}

// Transform is the pure mutation step. Given user YAML + context, it
// returns a new YAML string ready to feed to `docker compose up`.
//
// What it does:
//   - Parses the input.
//   - Lints (warnings forwarded to the caller; not blocking).
//   - Strips conflicting fields per service: container_name; user-supplied
//     teal.* labels (we own those); user-supplied platform_proxy network
//     entries (we add ours).
//   - Identifies the primary service (LabelPrimary opt-in OR first service
//     with a ports mapping).
//   - Attaches platform_proxy to the primary service's networks.
//   - Injects teal.app/color/role labels onto the primary service.
//   - Declares platform_proxy as an external network at the top level.
//   - Appends EnvFilePath (if non-empty) to every service's env_file list.
//   - Renders YAML.
func Transform(in TransformInput) (TransformOutput, error) {
	if in.AppSlug == "" {
		return TransformOutput{}, errors.New("compose: AppSlug required")
	}
	if in.Color != domain.ColorBlue && in.Color != domain.ColorGreen {
		return TransformOutput{}, fmt.Errorf("compose: invalid color %q", in.Color)
	}

	doc, err := Parse(in.UserYAML)
	if err != nil {
		return TransformOutput{}, err
	}

	out := TransformOutput{Warnings: doc.Lint()}
	services := doc.services()
	if len(services) == 0 {
		return TransformOutput{}, errors.New("compose: no services declared")
	}

	// First pass: container_name + platform-network ref + env_file. Label
	// scrubbing is deferred until AFTER pickPrimary because the latter
	// honours an explicit teal.primary opt-in label that would otherwise be
	// stripped before it could be read.
	for name, svc := range services {
		stripContainerName(svc)
		stripPlatformNetworkRef(svc)
		appendEnvFile(svc, in.EnvFilePath)
		injectResourceLimits(svc, in.CPULimit, in.MemoryLimit)
		services[name] = svc
	}

	// Three routing modes:
	//   - AttachServices set: per-service multi-route. Each named
	//     service gets platform_proxy + canonical labels; no primary
	//     heuristic.
	//   - AttachServices empty + Domains set: legacy single-domain.
	//     Pick a primary heuristically (which honours an explicit
	//     teal.primary opt-in label) and attach it.
	//   - Both empty: no routing; just scrub labels.
	if len(in.AttachServices) > 0 {
		// Multi-route: strip user-supplied teal.* labels first so the
		// engine's canonical ones don't conflict with stale user values.
		for name, svc := range services {
			stripTealLabels(svc)
			services[name] = svc
		}
		for _, svcName := range in.AttachServices {
			svc, ok := services[svcName]
			if !ok {
				return TransformOutput{}, fmt.Errorf("compose: routed service %q not declared in compose", svcName)
			}
			attachPlatformNetwork(svc)
			setTealLabels(svc, in.AppSlug, in.Color)
			services[svcName] = svc
		}
		out.PrimaryService = in.AttachServices[0]
		declareExternalPlatformNetwork(doc.root)
	} else if len(in.Domains) > 0 {
		// Legacy: pickPrimary needs to see user teal.primary labels, so
		// we strip teal.* labels AFTER picking, not before.
		primary, port, err := pickPrimary(services)
		if err != nil {
			return TransformOutput{}, err
		}
		out.PrimaryService = primary
		out.PortInContainer = port

		for name, svc := range services {
			stripTealLabels(svc)
			services[name] = svc
		}
		svc := services[primary]
		attachPlatformNetwork(svc)
		setTealLabels(svc, in.AppSlug, in.Color)
		services[primary] = svc

		declareExternalPlatformNetwork(doc.root)
	} else {
		for name, svc := range services {
			stripTealLabels(svc)
			services[name] = svc
		}
	}

	if len(in.BuildArgs) > 0 {
		for name, svc := range services {
			injectBuildArgs(svc, in.BuildArgs)
			services[name] = svc
		}
	}

	// Pin top-level named volumes so they survive blue/green flips.
	// Without this, compose's project-name prefix gives blue and
	// green their own copies (`<slug>-blue_db-data` vs `<slug>-green
	// _db-data`) and the database "loses" its rows on every other
	// deploy. Set `name: <slug>_<vol>` (color-stripped) so both
	// projects mount the same Docker volume.
	pinSharedVolumes(doc.root, in.AppSlug)

	doc.putServices(services)

	rendered, err := Render(doc)
	if err != nil {
		return TransformOutput{}, err
	}
	out.YAML = rendered
	return out, nil
}

// injectBuildArgs adds `args:` entries to the service's `build:` block
// for every key in keys. Each value is set to "${KEY}" so docker
// compose interpolates from --env-file at build time. Services without
// a build: block are left alone (image-only services don't run a
// Dockerfile).
//
// Compose's `build:` accepts two shapes:
//   - shorthand string: "build: ./app"  → promote to map form
//   - long form map: "build: { context: ./app, args: {...} }"
//
// User-supplied args are preserved unless a key collides with one of
// ours; the user's value wins (they presumably set it for a reason).
func injectBuildArgs(svc map[string]any, keys []string) {
	raw, ok := svc["build"]
	if !ok {
		return
	}
	build, ok := raw.(map[string]any)
	if !ok {
		// Shorthand "build: ./path" — promote to map form so we can
		// attach args.
		if ctx, isStr := raw.(string); isStr {
			build = map[string]any{"context": ctx}
		} else {
			return
		}
	}
	args, _ := build["args"].(map[string]any)
	if args == nil {
		// args might also be in list form (KEY=VALUE strings); we
		// don't try to merge into list form — switch to map.
		args = map[string]any{}
	}
	for _, k := range keys {
		if _, exists := args[k]; exists {
			continue
		}
		args[k] = "${" + k + "}"
	}
	build["args"] = args
	svc["build"] = build
}

// stripContainerName removes container_name; Compose's project-name prefix
// gives us blue/green isolation, which container_name would override.
func stripContainerName(svc map[string]any) {
	delete(svc, "container_name")
}

// stripTealLabels removes any user-supplied teal.* labels. Whatever Teal
// attaches must be the source of truth for label-based lookups.
func stripTealLabels(svc map[string]any) {
	labels := normalizeLabels(svc)
	if labels == nil {
		return
	}
	for k := range labels {
		if strings.HasPrefix(k, "teal.") {
			delete(labels, k)
		}
	}
	svc["labels"] = labels
}

// stripPlatformNetworkRef removes any pre-existing reference to our
// platform network. We add the right one ourselves, attached only to the
// primary service.
func stripPlatformNetworkRef(svc map[string]any) {
	nets := normalizeNetworks(svc)
	if nets == nil {
		return
	}
	out := nets[:0]
	for _, n := range nets {
		if n != PlatformNetworkAlias {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		delete(svc, "networks")
	} else {
		svc["networks"] = out
	}
}

// injectResourceLimits writes deploy.resources.limits.{cpus,memory}
// onto the service. If the user already declared a `deploy:` block we
// merge into it; otherwise we create the canonical structure. Empty
// values skip injection for that dimension. Compose's `deploy:` block
// is honoured by `docker compose up` outside swarm mode for resource
// limits (the docs are confusing on this — see compose-spec).
func injectResourceLimits(svc map[string]any, cpu, memory string) {
	if cpu == "" && memory == "" {
		return
	}
	deploy, _ := svc["deploy"].(map[string]any)
	if deploy == nil {
		deploy = map[string]any{}
		svc["deploy"] = deploy
	}
	resources, _ := deploy["resources"].(map[string]any)
	if resources == nil {
		resources = map[string]any{}
		deploy["resources"] = resources
	}
	limits, _ := resources["limits"].(map[string]any)
	if limits == nil {
		limits = map[string]any{}
		resources["limits"] = limits
	}
	if cpu != "" {
		limits["cpus"] = cpu
	}
	if memory != "" {
		limits["memory"] = memory
	}
}

// appendEnvFile adds path to the service's env_file list. If env_file is
// already a list of strings, append. If it's a single string, promote to
// list and append. Compose merges env_file values left-to-right, so our
// platform-managed values land last and override the user's.
func appendEnvFile(svc map[string]any, path string) {
	if path == "" {
		return
	}
	switch existing := svc["env_file"].(type) {
	case nil:
		svc["env_file"] = []any{path}
	case string:
		svc["env_file"] = []any{existing, path}
	case []any:
		svc["env_file"] = append(existing, path)
	default:
		// Unrecognised shape (e.g. compose v3 env_file objects): preserve
		// the user's structure, append a flat path. yaml.Marshal will
		// produce a heterogeneous list which compose accepts.
		svc["env_file"] = []any{existing, path}
	}
}

// pickPrimary chooses the service Traefik should route to.
// Order of precedence:
//  1. The single service with label teal.primary=true.
//  2. The first service (alphabetical) with a non-empty ports list.
//  3. The single service with a build: section (Coolify-style compose
//     strips ports, so the "service with source code" is the best
//     remaining signal).
//  4. The only service, if there's exactly one.
// Returns the service name and an in-container port to use as the routing
// target. Port detection is best-effort — when no ports: block exists the
// engine's TCP port probe (see deploy.CommonHTTPPorts) takes over.
func pickPrimary(services map[string]map[string]any) (string, int, error) {
	// Stable order so the heuristic is deterministic across runs.
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)

	// Pass 1: explicit opt-in.
	var explicit []string
	for _, n := range names {
		labels := normalizeLabels(services[n])
		if labels[LabelPrimary] == "true" {
			explicit = append(explicit, n)
		}
	}
	if len(explicit) == 1 {
		port, _ := firstContainerPort(services[explicit[0]])
		return explicit[0], port, nil
	}
	if len(explicit) > 1 {
		return "", 0, fmt.Errorf("compose: multiple services have label %s=true (%v); pick exactly one", LabelPrimary, explicit)
	}

	// Pass 2: first service with a ports mapping.
	for _, n := range names {
		if port, ok := firstContainerPort(services[n]); ok {
			return n, port, nil
		}
	}

	// Pass 3: single service with build:. Coolify-style composes drop
	// ports: altogether, so "the one building from source" is the
	// best remaining signal that this is the user's app. Port=0 lets
	// the engine's TCP probe decide.
	var withBuild []string
	for _, n := range names {
		if _, ok := services[n]["build"]; ok {
			withBuild = append(withBuild, n)
		}
	}
	if len(withBuild) == 1 {
		return withBuild[0], 0, nil
	}

	// Pass 4: single-service compose.
	if len(names) == 1 {
		return names[0], 0, nil
	}

	return "", 0, fmt.Errorf(
		"compose: cannot determine which of %d services to route to — add `labels: { %s: \"true\" }` to one, or configure per-service Routes in the app's Settings tab",
		len(names), LabelPrimary,
	)
}

// attachPlatformNetwork ensures platform_proxy is in the service's
// networks list. When the user's compose left the service's networks
// block empty (the common case), Compose puts the service on the
// project's implicit `default` network so it can reach its siblings
// (postgres, redis, etc.) by service name.
//
// The moment we declare ANY networks: list explicitly, Compose drops
// that implicit default — so the primary service loses DNS for its
// peers and intra-app calls start failing with EAI_AGAIN. Re-add
// `default` here so platform_proxy is additive, not replacing.
//
// If the user already declared their own networks, we respect their
// choice and add only platform_proxy — they opted out of the default
// deliberately.
func attachPlatformNetwork(svc map[string]any) {
	userHadNoNetworks := svc["networks"] == nil
	nets := normalizeNetworks(svc)
	for _, n := range nets {
		if n == PlatformNetworkAlias {
			return
		}
	}
	if userHadNoNetworks {
		nets = append(nets, "default")
	}
	svc["networks"] = append(nets, PlatformNetworkAlias)
}

// setTealLabels writes the per-deploy identity labels onto the service.
func setTealLabels(svc map[string]any, slug string, color domain.Color) {
	labels := normalizeLabels(svc)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[LabelApp] = slug
	labels[LabelColor] = string(color)
	labels[LabelRole] = RolePrimary
	svc["labels"] = labels
}

// declareExternalPlatformNetwork ensures the top-level networks map
// carries platform_proxy as an external network (so docker compose
// attaches services to it instead of trying to create it) AND declares
// the project's `default` network explicitly.
//
// Why the explicit `default`: once we set networks: [default,
// platform_proxy] on the primary, some compose versions stop
// auto-creating the project default unless it's named in the top-level
// networks map. Siblings like `postgres` (declared without their own
// networks: block) then fail DNS lookups with EAI_AGAIN. Declaring
// `default: {}` pins the behaviour across compose versions and is a
// no-op when compose would have created it anyway.
//
// User-supplied top-level networks are preserved — only the two keys
// we manage are written.
// pinSharedVolumes rewrites the top-level `volumes:` map so each
// managed (non-external) volume gets an explicit `name: <slug>_<key>`.
// That makes both blue and green compose projects mount the SAME
// Docker volume — without the pin, compose prefixes the volume with
// the project name (which includes the color) and the two stacks
// silently diverge: postgres on blue has data, postgres on green
// starts empty.
//
// User-managed cases left alone:
//   - external: true     → operator already owns naming; respect it
//   - explicit name: ... → operator already pinned; respect it
//   - bind mounts        → string-form, no project prefixing applies
//
// New volumes get name=<slug>_<key>, NOT <slug>-<color>_<key>. The
// color is intentionally stripped so the volume persists across the
// flip.
func pinSharedVolumes(root map[string]any, slug string) {
	rawVols, ok := root["volumes"]
	if !ok {
		return
	}
	vols, ok := rawVols.(map[string]any)
	if !ok {
		return
	}
	for name, raw := range vols {
		// nil entry (`db-data:` with no body) → promote to map.
		spec, _ := raw.(map[string]any)
		if spec == nil {
			spec = map[string]any{}
		}
		if ext, _ := spec["external"].(bool); ext {
			continue
		}
		if existing, _ := spec["name"].(string); existing != "" {
			continue
		}
		spec["name"] = slug + "_" + name
		vols[name] = spec
	}
	root["volumes"] = vols
}

func declareExternalPlatformNetwork(root map[string]any) {
	nets, _ := root["networks"].(map[string]any)
	if nets == nil {
		nets = map[string]any{}
	}
	nets[PlatformNetworkAlias] = map[string]any{
		"external": true,
		"name":     PlatformNetworkAlias,
	}
	if _, ok := nets["default"]; !ok {
		nets["default"] = map[string]any{}
	}
	root["networks"] = nets
}

// normalizeLabels returns the service's labels as map[string]string,
// converting from Compose's two accepted forms (map of k:v, or list of
// "K=V" strings). Writes the normalised form back so subsequent code
// always sees a map.
func normalizeLabels(svc map[string]any) map[string]string {
	switch raw := svc["labels"].(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]string, len(raw))
		for k, v := range raw {
			out[k] = stringify(v)
		}
		svc["labels"] = out
		return out
	case []any:
		out := make(map[string]string, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				if i := strings.IndexByte(s, '='); i >= 0 {
					out[s[:i]] = s[i+1:]
				} else {
					out[s] = ""
				}
			}
		}
		svc["labels"] = out
		return out
	case map[string]string:
		return raw
	default:
		return nil
	}
}

// normalizeNetworks coerces the service's networks into a []string. Compose
// accepts list-form ("net1") and map-form (with per-network options); for
// v1 we treat both as a list of names — services using map-form for
// aliases/IPs are an edge case Phase 5 can revisit.
func normalizeNetworks(svc map[string]any) []string {
	switch raw := svc["networks"].(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), raw...)
	case map[string]any:
		out := make([]string, 0, len(raw))
		for k := range raw {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

// firstContainerPort extracts the in-container port from a service's first
// ports entry. Compose accepts several forms:
//
//	"80"                         -> 80
//	"8080:80"                    -> 80 (right side, the container side)
//	"127.0.0.1:8080:80"          -> 80
//	"80/tcp"                     -> 80
//	{ "target": 80, ... }        -> 80
//
// We do best-effort parsing; on failure we return ok=false so the caller
// can decide whether to error or fall back.
func firstContainerPort(svc map[string]any) (int, bool) {
	ports, _ := svc["ports"].([]any)
	if len(ports) == 0 {
		return 0, false
	}
	switch first := ports[0].(type) {
	case string:
		s := first
		if slash := strings.IndexByte(s, '/'); slash >= 0 {
			s = s[:slash]
		}
		// container side is whatever follows the LAST ':'.
		if colon := strings.LastIndexByte(s, ':'); colon >= 0 {
			s = s[colon+1:]
		}
		var n int
		_, err := fmt.Sscanf(s, "%d", &n)
		if err == nil && n > 0 {
			return n, true
		}
	case int:
		return first, true
	case map[string]any:
		if t, ok := first["target"]; ok {
			switch v := t.(type) {
			case int:
				return v, true
			case string:
				var n int
				if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
					return n, true
				}
			}
		}
	}
	return 0, false
}

// stringify renders a value as the string Compose would have produced. Used
// when normalising labels from the map form (which permits any scalar).
func stringify(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
