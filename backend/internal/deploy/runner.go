package deploy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// ComposeRunner shells out to `docker compose` for the heavy lifting. We
// don't reimplement compose's dependency graph / build / healthcheck merge
// in Go — that would be reproducing years of edge cases for no platform
// benefit.
//
// Operations are cancellable via the context passed in. Stdout and stderr
// are written line-by-line to logSink so the deploy log captures progress
// in real time (Phase 6 will tee this to a WebSocket).
type ComposeRunner struct {
	// DockerBin is the binary to invoke. Default "docker"; tests can
	// override to point at a fake.
	DockerBin string
}

// NewComposeRunner returns a ComposeRunner using the standard docker binary.
func NewComposeRunner() *ComposeRunner {
	return &ComposeRunner{DockerBin: "docker"}
}

// ComposeOptions describes how to invoke `docker compose` for one
// deployment. ProjectDir, when non-empty, sets `--project-directory`
// so build contexts and bind-mount paths in the user's compose
// resolve relative to it. For git-source apps this should be the
// checkout dir (where `build: ./app` is meant to find <checkout>/app).
//
// EnvFilePath, when non-empty, is passed as docker compose's top-level
// `--env-file`. This is what makes `${VAR}` interpolation in the
// user's compose YAML resolve from Teal's per-app env vars. The
// per-service `env_file:` directive (added by the compose transform)
// covers the runtime container env; --env-file covers parse-time
// interpolation. Both point at the same deploy.env so user-set vars
// satisfy both layers.
type ComposeOptions struct {
	Project     string
	ComposePath string
	ProjectDir  string // empty → docker compose defaults to dir of -f file
	EnvFilePath string // empty → docker compose tries .env in project dir
}

func (o ComposeOptions) baseArgs() []string {
	args := []string{"compose"}
	if o.EnvFilePath != "" {
		args = append(args, "--env-file", o.EnvFilePath)
	}
	args = append(args, "-p", o.Project, "-f", o.ComposePath)
	if o.ProjectDir != "" {
		args = append(args, "--project-directory", o.ProjectDir)
	}
	return args
}

// Up runs `docker compose ... up -d --remove-orphans`.
func (r *ComposeRunner) Up(ctx context.Context, opts ComposeOptions, logSink io.Writer) error {
	args := append(opts.baseArgs(), "up", "-d", "--remove-orphans")
	return r.run(ctx, logSink, args...)
}

// Down runs `docker compose ... down --timeout 30 --remove-orphans`.
// timeout is the per-container shutdown grace before SIGKILL.
func (r *ComposeRunner) Down(ctx context.Context, opts ComposeOptions, logSink io.Writer) error {
	args := append(opts.baseArgs(), "down", "--timeout", "30", "--remove-orphans")
	return r.run(ctx, logSink, args...)
}

// TeardownByProject removes every container + network labelled with
// the given compose project name, without needing the original
// compose file. Used during app deletion: by then the previous
// compose may be gone (the workdir was cleaned up, or the file path
// is unknown) but the running stack is still tagged with the
// standard `com.docker.compose.project` label compose attaches at
// `up` time.
//
// Volumes are intentionally NOT removed — they often hold the
// app's primary state (postgres data, uploaded files) and silent
// data loss on delete is the wrong default. Operator can prune
// volumes explicitly via the Volumes UI.
func (r *ComposeRunner) TeardownByProject(ctx context.Context, project string, logSink io.Writer) error {
	if project == "" {
		return fmt.Errorf("runner: empty project")
	}
	label := "com.docker.compose.project=" + project

	// Containers first (also stops them — `rm -f` does both).
	ids, err := r.dockerListByLabel(ctx, "ps", label)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, id := range ids {
		if err := r.run(ctx, logSink, "rm", "-f", "-v", id); err != nil {
			fmt.Fprintf(logSink, "[warn] rm container %s: %v\n", id, err)
		}
	}

	// Networks the project owned. The platform_proxy network is
	// labelled `teal.managed=true` (NOT the compose-project label)
	// so this filter naturally skips it.
	netIDs, err := r.dockerListByLabel(ctx, "network", label)
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	for _, id := range netIDs {
		if err := r.run(ctx, logSink, "network", "rm", id); err != nil {
			fmt.Fprintf(logSink, "[warn] rm network %s: %v\n", id, err)
		}
	}
	return nil
}

// dockerListByLabel runs `docker <kind> ls -aq --filter label=...`
// and returns the IDs (one per line). kind is "ps" (containers),
// "network", or "volume" — anything that supports `ls -aq --filter
// label=...`. "ps" is special-cased because `docker ps -a` already
// includes stopped containers; the others use `ls -a`.
func (r *ComposeRunner) dockerListByLabel(ctx context.Context, kind, label string) ([]string, error) {
	var args []string
	switch kind {
	case "ps":
		args = []string{"ps", "-aq", "--filter", "label=" + label}
	default:
		args = []string{kind, "ls", "-q", "--filter", "label=" + label}
	}
	cmd := exec.CommandContext(ctx, r.DockerBin, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		s := strings.TrimSpace(line)
		if s != "" {
			ids = append(ids, s)
		}
	}
	return ids, nil
}

// Pull runs `docker compose ... pull` to refresh images for non-build
// services.
func (r *ComposeRunner) Pull(ctx context.Context, opts ComposeOptions, logSink io.Writer) error {
	args := append(opts.baseArgs(), "pull")
	return r.run(ctx, logSink, args...)
}

// Build runs `docker compose ... build` for services with a build:
// directive.
func (r *ComposeRunner) Build(ctx context.Context, opts ComposeOptions, logSink io.Writer) error {
	args := append(opts.baseArgs(), "build")
	return r.run(ctx, logSink, args...)
}

// ContainerIDByService finds a container in the given compose project
// by service name. Used by the multi-route flow to look up a specific
// service's container without relying on Teal's own teal.role labels.
// Returns empty string + nil error when no match.
func (r *ComposeRunner) ContainerIDByService(ctx context.Context, project, service string) (string, error) {
	cmd := exec.CommandContext(ctx, r.DockerBin, "ps", "--quiet",
		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service="+service,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("runner: docker ps by service %q: %w", service, err)
	}
	id := strings.TrimSpace(string(out))
	if i := strings.IndexByte(id, '\n'); i >= 0 {
		id = id[:i]
	}
	return id, nil
}

// PrimaryContainerID returns the container ID of the service Teal labelled
// as primary for this color. Looks up via Docker labels (teal.app + teal.color).
// Returns empty string with nil error if no matching container found.
func (r *ComposeRunner) PrimaryContainerID(ctx context.Context, app, color string) (string, error) {
	cmd := exec.CommandContext(ctx, r.DockerBin, "ps", "--quiet",
		"--filter", "label=teal.app="+app,
		"--filter", "label=teal.color="+color,
		"--filter", "label=teal.role=primary",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("runner: docker ps: %w", err)
	}
	id := strings.TrimSpace(string(out))
	// `docker ps -q` may return multiple lines; primary should be unique
	// per (app, color), but trim defensively.
	if i := strings.IndexByte(id, '\n'); i >= 0 {
		id = id[:i]
	}
	return id, nil
}

// HasBuildDirective reports whether the user's transformed compose declares
// a `build:` for any service. Used by the engine to decide between Pull
// and Build before Up.
func HasBuildDirective(composeYAML string) bool {
	// A naive substring check is fine: `build:` is a top-of-line key under a
	// service block. False positives on strings that happen to contain
	// "build:" are unlikely and harmless (we'd run docker compose build
	// which is a no-op when nothing has a build directive).
	return strings.Contains(composeYAML, "\n    build:") ||
		strings.Contains(composeYAML, "\n  build:") ||
		strings.HasPrefix(composeYAML, "build:")
}

// run executes a docker subcommand and tees stdout+stderr line-by-line to
// logSink. Returns an error if the process exits non-zero or context is
// cancelled.
func (r *ComposeRunner) run(ctx context.Context, logSink io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, r.DockerBin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("runner: start docker %v: %w", args, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go pipe(&wg, stdout, logSink, "stdout")
	go pipe(&wg, stderr, logSink, "stderr")
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("runner: docker %s exited: %w", args[0], err)
	}
	return nil
}

func pipe(wg *sync.WaitGroup, src io.Reader, dst io.Writer, tag string) {
	defer wg.Done()
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		_, _ = fmt.Fprintf(dst, "[%s] %s\n", tag, scanner.Text())
	}
}
