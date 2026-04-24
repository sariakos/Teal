package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Container is the Teal-shaped representation of a Docker container. We
// expose only the fields the platform actually uses; new fields are added
// here as new features need them, not pre-emptively.
type Container struct {
	ID      string            // full container ID
	Names   []string          // raw names from the daemon (with leading "/")
	Image   string            // image reference as scheduled
	State   string            // "running", "exited", etc.
	Status  string            // human-readable status, e.g. "Up 3 minutes"
	Created time.Time         // creation time on the daemon
	Labels  map[string]string // includes Compose labels we'll key off later
}

// Network is the Teal-shaped representation of a Docker network.
type Network struct {
	ID     string
	Name   string
	Driver string
	Scope  string
	Labels map[string]string
}

// Volume is the Teal-shaped representation of a named Docker volume.
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	CreatedAt  time.Time
	Labels     map[string]string
}

// Client is the Docker interface used by the rest of the codebase. Defining
// it as an interface (not just exposing the struct) lets higher layers fake
// it in tests without spinning up Docker.
//
// Mutations are added per-feature: Phase 3 introduces NetworkCreateIfMissing
// so the traefik package can ensure platform_proxy at startup.
type Client interface {
	ListContainers(ctx context.Context) ([]Container, error)
	ListNetworks(ctx context.Context) ([]Network, error)
	ListVolumes(ctx context.Context) ([]Volume, error)
	NetworkCreateIfMissing(ctx context.Context, name string, labels map[string]string) error
	ContainerInspect(ctx context.Context, id string) (ContainerInspect, error)
	ContainerStats(ctx context.Context, id string) (ContainerStats, error)
	StreamContainerLogs(ctx context.Context, id string) (<-chan ContainerLogLine, <-chan error, error)
	// TailContainerLogs returns the last `tail` lines of the
	// container's stdout+stderr as a single combined string. Used by
	// the deploy engine to dump a crash trace into the deploy log
	// before tearing down a failed container.
	TailContainerLogs(ctx context.Context, id string, tail int) (string, error)
	VolumeRemove(ctx context.Context, name string, force bool) error
	Ping(ctx context.Context) error
	Close() error
}

// ContainerLogLine is one line of demuxed container output from the
// streaming logs API. Stream is "stdout" or "stderr"; Line is the raw
// text without the trailing newline.
type ContainerLogLine struct {
	Stream string
	Line   string
}

// ContainerStats is one snapshot from `docker stats`. CPU usage is
// already converted to a percentage of host capacity (matches what the
// CLI prints). Memory and IO numbers are raw bytes.
type ContainerStats struct {
	CPUPercent float64
	MemBytes   int64
	MemLimit   int64
	NetRx      int64
	NetTx      int64
	BlkRead    int64
	BlkWrite   int64
}

// ErrVolumeInUse is returned by VolumeRemove when the volume is mounted
// by a running container and force=false. Callers should map to 409.
var ErrVolumeInUse = fmt.Errorf("docker: volume is in use")

// ContainerInspect carries just the fields the deploy/healthcheck path
// needs from a `docker inspect`. Trimmed deliberately — adding more fields
// is cheap, but exposing the full SDK type would couple callers to the SDK.
type ContainerInspect struct {
	ID            string
	Name          string
	State         string         // "running", "exited", ...
	ExitCode      int            // process exit code; only meaningful when State == "exited"
	Health        string         // "healthy", "unhealthy", "starting", "" (no healthcheck)
	NetworkIPs    map[string]string // network name -> IPv4 address
	ExposedPorts  []string       // e.g. "80/tcp", "443/tcp"
	Labels        map[string]string
}

// realClient is the production implementation backed by the Docker SDK.
type realClient struct {
	c *client.Client
}

// NewClient connects to the Docker daemon. host == "" uses the SDK's default
// resolution (DOCKER_HOST env or local socket); a non-empty host overrides
// it.
//
// The returned client negotiates the API version with the daemon so we don't
// need to hard-code it; Docker 20.10+ supports negotiation.
func NewClient(host string) (Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker: new client: %w", err)
	}
	return &realClient{c: c}, nil
}

func (r *realClient) Close() error {
	if r.c == nil {
		return nil
	}
	return r.c.Close()
}

func (r *realClient) Ping(ctx context.Context) error {
	_, err := r.c.Ping(ctx)
	return err
}

func (r *realClient) ListContainers(ctx context.Context) ([]Container, error) {
	// All == true so we also see stopped containers; the UI distinguishes
	// state itself.
	raw, err := r.c.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("docker: list containers: %w", err)
	}
	out := make([]Container, 0, len(raw))
	for _, c := range raw {
		out = append(out, Container{
			ID:      c.ID,
			Names:   c.Names,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: time.Unix(c.Created, 0).UTC(),
			Labels:  c.Labels,
		})
	}
	return out, nil
}

func (r *realClient) ListNetworks(ctx context.Context) ([]Network, error) {
	raw, err := r.c.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker: list networks: %w", err)
	}
	out := make([]Network, 0, len(raw))
	for _, n := range raw {
		out = append(out, Network{
			ID:     n.ID,
			Name:   n.Name,
			Driver: n.Driver,
			Scope:  n.Scope,
			Labels: n.Labels,
		})
	}
	return out, nil
}

// NetworkCreateIfMissing creates a user-defined bridge network with the
// given labels if no network with that name exists. Idempotent: existing
// networks are left untouched, and label drift is NOT corrected here (we
// don't want platform code to change a user's hand-edited network).
func (r *realClient) NetworkCreateIfMissing(ctx context.Context, name string, labels map[string]string) error {
	existing, err := r.c.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("docker: list networks: %w", err)
	}
	for _, n := range existing {
		if n.Name == name {
			return nil
		}
	}
	_, err = r.c.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("docker: create network %q: %w", name, err)
	}
	return nil
}

// ContainerInspect returns the trimmed inspection data the deploy engine
// needs (state, health, network IPs, exposed ports).
func (r *realClient) ContainerInspect(ctx context.Context, id string) (ContainerInspect, error) {
	raw, err := r.c.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerInspect{}, fmt.Errorf("docker: inspect %q: %w", id, err)
	}
	out := ContainerInspect{
		ID:     raw.ID,
		Name:   raw.Name,
		Labels: raw.Config.Labels,
	}
	if raw.State != nil {
		out.State = raw.State.Status
		out.ExitCode = raw.State.ExitCode
		if raw.State.Health != nil {
			out.Health = raw.State.Health.Status
		}
	}
	if raw.NetworkSettings != nil {
		out.NetworkIPs = make(map[string]string, len(raw.NetworkSettings.Networks))
		for name, ep := range raw.NetworkSettings.Networks {
			if ep != nil {
				out.NetworkIPs[name] = ep.IPAddress
			}
		}
	}
	if raw.Config != nil {
		for p := range raw.Config.ExposedPorts {
			out.ExposedPorts = append(out.ExposedPorts, string(p))
		}
	}
	return out, nil
}

// VolumeRemove deletes a named volume. Returns ErrVolumeInUse when the
// daemon refuses because a container is using it (and force == false).
// Other errors are wrapped.
func (r *realClient) VolumeRemove(ctx context.Context, name string, force bool) error {
	err := r.c.VolumeRemove(ctx, name, force)
	if err == nil {
		return nil
	}
	// The daemon error message format is "Error response from daemon:
	// remove <name>: volume is in use - [container ids]". We pattern-match
	// on the substring rather than depend on the SDK's typed error (which
	// is internal to the engine package).
	if strings.Contains(err.Error(), "volume is in use") {
		return ErrVolumeInUse
	}
	return fmt.Errorf("docker: remove volume %q: %w", name, err)
}

// StreamContainerLogs follows the container's stdout + stderr from the
// beginning of its current run. The returned channel emits one
// ContainerLogLine per logical line; the error channel fires once with
// the terminal error (or nil) when the stream ends. Both channels are
// closed when the stream terminates (container exits, context cancels).
//
// Demux is via stdcopy — Docker frames stdout/stderr with an 8-byte
// header per chunk. We split chunks into lines on '\n' so a partial
// chunk that doesn't end in newline is buffered until the next chunk.
func (r *realClient) StreamContainerLogs(ctx context.Context, id string) (<-chan ContainerLogLine, <-chan error, error) {
	rc, err := r.c.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "0", // start fresh — historical lines come from logbuffer's file
	})
	if err != nil {
		return nil, nil, fmt.Errorf("docker: container logs: %w", err)
	}

	lines := make(chan ContainerLogLine, 64)
	errs := make(chan error, 1)

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	// Demux goroutine: stdcopy into two pipes, then close them so the
	// scanner goroutines below see EOF cleanly.
	go func() {
		_, copyErr := stdcopy.StdCopy(stdoutW, stderrW, rc)
		_ = stdoutW.Close()
		_ = stderrW.Close()
		_ = rc.Close()
		errs <- copyErr
		close(errs)
	}()

	scan := func(stream string, src io.Reader, done chan<- struct{}) {
		s := bufio.NewScanner(src)
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1 MiB max line
		for s.Scan() {
			select {
			case lines <- ContainerLogLine{Stream: stream, Line: s.Text()}:
			case <-ctx.Done():
				done <- struct{}{}
				return
			}
		}
		done <- struct{}{}
	}
	stdoutDone := make(chan struct{}, 1)
	stderrDone := make(chan struct{}, 1)
	go scan("stdout", stdoutR, stdoutDone)
	go scan("stderr", stderrR, stderrDone)

	// Closer goroutine: when both scanners finish, close the lines chan.
	go func() {
		<-stdoutDone
		<-stderrDone
		close(lines)
	}()

	return lines, errs, nil
}

// TailContainerLogs reads the last `tail` lines of the container's
// stdout+stderr in a single shot (no follow). The two streams are
// demuxed and merged in arrival order. Lines longer than 1 MiB are
// truncated.
func (r *realClient) TailContainerLogs(ctx context.Context, id string, tail int) (string, error) {
	if tail <= 0 {
		tail = 80
	}
	rc, err := r.c.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
		Timestamps: false,
		Tail:       fmt.Sprintf("%d", tail),
	})
	if err != nil {
		return "", fmt.Errorf("docker: container logs: %w", err)
	}
	defer rc.Close()

	var stdout, stderr strings.Builder
	if _, err := stdcopy.StdCopy(&stdout, &stderr, rc); err != nil {
		// Some daemons return io.EOF here even on success; treat any
		// data we already collected as authoritative.
	}
	// Combine: stderr first (where crash traces usually go), then
	// stdout. A perfectly time-merged view would need timestamps;
	// callers want "what does the container say it died from", and
	// stderr-first answers that ~90% of the time.
	if stderr.Len() == 0 {
		return stdout.String(), nil
	}
	if stdout.Len() == 0 {
		return stderr.String(), nil
	}
	return "[stderr]\n" + stderr.String() + "\n[stdout]\n" + stdout.String(), nil
}

// ContainerStats returns one snapshot of container resource usage. The
// docker daemon supports a streaming variant; we one-shot here so the
// scraper can throttle without us managing a per-container goroutine.
func (r *realClient) ContainerStats(ctx context.Context, id string) (ContainerStats, error) {
	resp, err := r.c.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return ContainerStats{}, fmt.Errorf("docker: stats %q: %w", id, err)
	}
	defer resp.Body.Close()
	return decodeStatsJSON(resp.Body)
}

func (r *realClient) ListVolumes(ctx context.Context) ([]Volume, error) {
	resp, err := r.c.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker: list volumes: %w", err)
	}
	out := make([]Volume, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		// CreatedAt comes back as RFC3339 from the daemon; if parsing fails
		// (older daemons can omit it) we keep the zero time rather than
		// erroring the whole list.
		var created time.Time
		if v.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, v.CreatedAt); err == nil {
				created = t
			}
		}
		out = append(out, Volume{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			CreatedAt:  created,
			Labels:     v.Labels,
		})
	}
	return out, nil
}
