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

// Up runs `docker compose -p <project> -f <compose> up -d --remove-orphans`.
func (r *ComposeRunner) Up(ctx context.Context, project, composePath string, logSink io.Writer) error {
	return r.run(ctx, logSink, "compose", "-p", project, "-f", composePath, "up", "-d", "--remove-orphans")
}

// Down runs `docker compose -p <project> -f <compose> down --timeout 30 --remove-orphans`.
// timeout is the per-container shutdown grace before SIGKILL.
func (r *ComposeRunner) Down(ctx context.Context, project, composePath string, logSink io.Writer) error {
	return r.run(ctx, logSink, "compose", "-p", project, "-f", composePath, "down", "--timeout", "30", "--remove-orphans")
}

// Pull runs `docker compose -p <project> -f <compose> pull` to refresh
// images for non-build services.
func (r *ComposeRunner) Pull(ctx context.Context, project, composePath string, logSink io.Writer) error {
	return r.run(ctx, logSink, "compose", "-p", project, "-f", composePath, "pull")
}

// Build runs `docker compose -p <project> -f <compose> build` for services
// with a build: directive.
func (r *ComposeRunner) Build(ctx context.Context, project, composePath string, logSink io.Writer) error {
	return r.run(ctx, logSink, "compose", "-p", project, "-f", composePath, "build")
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
