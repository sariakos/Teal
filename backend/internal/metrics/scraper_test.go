package metrics

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/containerwatcher"
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// dockerStub returns the same stats every call, recording how many
// times ContainerStats was invoked.
type dockerStub struct {
	stats docker.ContainerStats
	calls int
	mu    sync.Mutex
}

func (d *dockerStub) ListContainers(_ context.Context) ([]docker.Container, error) { return nil, nil }
func (d *dockerStub) ListNetworks(_ context.Context) ([]docker.Network, error)     { return nil, nil }
func (d *dockerStub) ListVolumes(_ context.Context) ([]docker.Volume, error)       { return nil, nil }
func (d *dockerStub) NetworkCreateIfMissing(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (d *dockerStub) ContainerInspect(_ context.Context, _ string) (docker.ContainerInspect, error) {
	return docker.ContainerInspect{}, nil
}
func (d *dockerStub) ContainerStats(_ context.Context, _ string) (docker.ContainerStats, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	return d.stats, nil
}
func (d *dockerStub) StreamContainerLogs(_ context.Context, _ string) (<-chan docker.ContainerLogLine, <-chan error, error) {
	lines := make(chan docker.ContainerLogLine)
	errs := make(chan error, 1)
	close(lines)
	close(errs)
	return lines, errs, nil
}
func (d *dockerStub) VolumeRemove(_ context.Context, _ string, _ bool) error { return nil }
func (d *dockerStub) Ping(_ context.Context) error                            { return nil }
func (d *dockerStub) Close() error                                            { return nil }

type capturePub struct {
	mu   sync.Mutex
	hits []string
}

func (c *capturePub) Publish(topic string, _ any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits = append(c.hits, topic)
}

func newScraperStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestScraperPersistsAndPublishes(t *testing.T) {
	st := newScraperStore(t)
	d := &dockerStub{stats: docker.ContainerStats{
		CPUPercent: 12.0, MemBytes: 1 << 20, MemLimit: 1 << 30,
	}}
	pub := &capturePub{}

	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), d, st.Metrics, pub, Config{
		Interval:    20 * time.Millisecond,
		MinInterval: 1 * time.Millisecond,
	})

	s.OnContainerStarted(context.Background(), containerwatcher.Container{
		ID: "c1", Name: "/svc-blue-web-1", AppSlug: "svc", Color: domain.ColorBlue,
	})
	s.OnContainerStarted(context.Background(), containerwatcher.Container{
		ID: "c2", Name: "/svc-blue-api-1", AppSlug: "svc", Color: domain.ColorBlue,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Wait for at least one tick.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		c := d.calls
		d.mu.Unlock()
		if c >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	rows, err := st.Metrics.LatestForApp(context.Background(), "svc", time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("rows: %d", len(rows))
	}

	pub.mu.Lock()
	hits := append([]string(nil), pub.hits...)
	pub.mu.Unlock()
	if len(hits) < 2 {
		t.Errorf("publishes: %d", len(hits))
	}
	saw := map[string]bool{}
	for _, h := range hits {
		saw[h] = true
	}
	if !saw["metrics.c1"] || !saw["metrics.c2"] {
		t.Errorf("missing topics: %v", hits)
	}
}

func TestScraperOnContainerStoppedExcludes(t *testing.T) {
	st := newScraperStore(t)
	d := &dockerStub{}
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), d, st.Metrics, nil, Config{
		Interval:    10 * time.Millisecond,
		MinInterval: 1 * time.Millisecond,
	})
	s.OnContainerStarted(context.Background(), containerwatcher.Container{ID: "c1", AppSlug: "x", Color: domain.ColorBlue})
	s.OnContainerStopped(context.Background(), "c1")
	s.scrapeOnce(context.Background())
	d.mu.Lock()
	c := d.calls
	d.mu.Unlock()
	if c != 0 {
		t.Errorf("stats called after stop: %d", c)
	}
}
