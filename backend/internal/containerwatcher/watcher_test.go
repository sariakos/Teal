package containerwatcher

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
)

// fakeDocker is a stub Client that returns whatever its `set` field
// holds. Other methods panic so a watcher accidentally calling them
// shows up immediately.
type fakeDocker struct {
	mu  sync.Mutex
	set []docker.Container
}

func (f *fakeDocker) update(cs []docker.Container) {
	f.mu.Lock()
	f.set = cs
	f.mu.Unlock()
}

func (f *fakeDocker) ListContainers(_ context.Context) ([]docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]docker.Container, len(f.set))
	copy(out, f.set)
	return out, nil
}
func (f *fakeDocker) ListNetworks(_ context.Context) ([]docker.Network, error) { return nil, nil }
func (f *fakeDocker) ListVolumes(_ context.Context) ([]docker.Volume, error)   { return nil, nil }
func (f *fakeDocker) NetworkCreateIfMissing(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (f *fakeDocker) ContainerInspect(_ context.Context, _ string) (docker.ContainerInspect, error) {
	return docker.ContainerInspect{}, nil
}
func (f *fakeDocker) ContainerStats(_ context.Context, _ string) (docker.ContainerStats, error) {
	return docker.ContainerStats{}, nil
}
func (f *fakeDocker) StreamContainerLogs(_ context.Context, _ string) (<-chan docker.ContainerLogLine, <-chan error, error) {
	lines := make(chan docker.ContainerLogLine)
	errs := make(chan error, 1)
	close(lines)
	close(errs)
	return lines, errs, nil
}
func (f *fakeDocker) TailContainerLogs(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (f *fakeDocker) VolumeRemove(_ context.Context, _ string, _ bool) error { return nil }
func (f *fakeDocker) Ping(_ context.Context) error                            { return nil }
func (f *fakeDocker) Close() error                                            { return nil }

// captureSub records every callback. Goroutine-safe.
type captureSub struct {
	mu       sync.Mutex
	started  []Container
	stopped  []string
	totalEvt int
}

func (c *captureSub) OnContainerStarted(_ context.Context, ct Container) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started = append(c.started, ct)
	c.totalEvt++
}
func (c *captureSub) OnContainerStopped(_ context.Context, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = append(c.stopped, id)
	c.totalEvt++
}

func TestSplitProject(t *testing.T) {
	cases := []struct {
		in       string
		slug     string
		color    domain.Color
		ok       bool
	}{
		{"myapp-blue", "myapp", domain.ColorBlue, true},
		{"my-app-green", "my-app", domain.ColorGreen, true},
		{"-blue", "", "", false},  // empty slug
		{"-green", "", "", false}, // empty slug
		{"myapp", "", "", false},
		{"myapp-yellow", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		slug, color, ok := splitProject(c.in)
		if ok != c.ok || slug != c.slug || color != c.color {
			t.Errorf("splitProject(%q) = (%q,%q,%v) want (%q,%q,%v)",
				c.in, slug, color, ok, c.slug, c.color, c.ok)
		}
	}
}

func TestWatcherDetectsStartAndStop(t *testing.T) {
	d := &fakeDocker{}
	w := New(slog.New(slog.NewTextHandler(io.Discard, nil)), d, 10*time.Millisecond)
	sub := &captureSub{}
	w.Subscribe(sub)

	d.update([]docker.Container{
		{ID: "c1", State: "running", Names: []string{"/svc-blue-web-1"}, Image: "nginx",
			Labels: map[string]string{"com.docker.compose.project": "svc-blue"}},
		{ID: "noise", State: "running", Names: []string{"/teal"},
			Labels: map[string]string{}}, // no compose project → skipped
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	// Wait for first event.
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		sub.mu.Lock()
		n := len(sub.started)
		sub.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	sub.mu.Lock()
	if len(sub.started) != 1 || sub.started[0].ID != "c1" {
		t.Fatalf("expected start event for c1, got %+v", sub.started)
	}
	if sub.started[0].AppSlug != "svc" || sub.started[0].Color != domain.ColorBlue {
		t.Errorf("split parsed wrong: %+v", sub.started[0])
	}
	sub.mu.Unlock()

	// Now drop the container — should fire stop.
	d.update([]docker.Container{})
	deadline = time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		sub.mu.Lock()
		n := len(sub.stopped)
		sub.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	sub.mu.Lock()
	if len(sub.stopped) != 1 || sub.stopped[0] != "c1" {
		t.Errorf("expected stop event for c1, got %+v", sub.stopped)
	}
	sub.mu.Unlock()
}

func TestWatcherSnapshotMirrorsKnown(t *testing.T) {
	d := &fakeDocker{set: []docker.Container{
		{ID: "a", State: "running", Names: []string{"/x-blue-web-1"},
			Labels: map[string]string{"com.docker.compose.project": "x-blue"}},
	}}
	w := New(slog.New(slog.NewTextHandler(io.Discard, nil)), d, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(w.Snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := w.Snapshot()
	if len(snap) != 1 || snap[0].ID != "a" {
		t.Errorf("Snapshot: %+v", snap)
	}
}
