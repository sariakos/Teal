package logbuffer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/sariakos/teal/backend/internal/containerwatcher"
	"github.com/sariakos/teal/backend/internal/docker"
)

// Publisher is the subset of the realtime hub that the registry uses to
// fan out live log lines. Defined as an interface so tests can capture
// publishes without spinning up the real hub.
type Publisher interface {
	Publish(topic string, data any)
}

// Topic returns the realtime topic name for a container's logs.
// Exposed so the API layer (which serves both the historical-tail
// endpoint and the WS subscribe op) names topics identically.
func Topic(containerID string) string { return "containerlogs." + containerID }

// Config tunes the registry's prune cadence + retention. Zero values
// fall back to documented defaults.
type Config struct {
	Root          string        // root directory for per-container files
	Retention     time.Duration // drop lines older than this; default 6h
	PruneInterval time.Duration // how often the prune goroutine runs; default 5min
	// IdleDelete is how long a container can be absent before its file
	// is deleted. Defaults to Retention. Set higher to keep dead-container
	// logs around longer (e.g. for postmortem of a crash).
	IdleDelete time.Duration
}

const (
	defaultRetention     = 6 * time.Hour
	defaultPruneInterval = 5 * time.Minute
)

// Registry holds every active Tailer and the on-disk Buffers backing
// them. One Registry per process; subscribe it to a containerwatcher
// before Run.
type Registry struct {
	cfg    Config
	docker docker.Client
	hub    Publisher
	logger *slog.Logger

	mu      sync.Mutex
	bufs    map[string]*Buffer        // containerID -> buffer (live OR retained-after-stop)
	cancels map[string]context.CancelFunc // containerID -> per-tailer cancel
	gone    map[string]time.Time      // containerID -> when it disappeared (for IdleDelete)
}

// NewRegistry constructs a Registry. cfg.Root is required; everything
// else gets defaults.
func NewRegistry(logger *slog.Logger, dock docker.Client, hub Publisher, cfg Config) *Registry {
	if cfg.Retention == 0 {
		cfg.Retention = defaultRetention
	}
	if cfg.PruneInterval == 0 {
		cfg.PruneInterval = defaultPruneInterval
	}
	if cfg.IdleDelete == 0 {
		cfg.IdleDelete = cfg.Retention
	}
	return &Registry{
		cfg:     cfg,
		docker:  dock,
		hub:     hub,
		logger:  logger,
		bufs:    map[string]*Buffer{},
		cancels: map[string]context.CancelFunc{},
		gone:    map[string]time.Time{},
	}
}

// Buffer returns the on-disk buffer for a container (or nil if none).
// Used by the HTTP tail endpoint.
func (r *Registry) Buffer(containerID string) *Buffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bufs[containerID]
}

// OnContainerStarted satisfies containerwatcher.Subscriber. Spawns a
// tailer goroutine the first time a container is seen; on subsequent
// observations of the same ID (e.g. after a transient docker hiccup)
// it's a no-op because the previous tailer is still running.
func (r *Registry) OnContainerStarted(parentCtx context.Context, c containerwatcher.Container) {
	r.mu.Lock()
	if _, running := r.cancels[c.ID]; running {
		r.mu.Unlock()
		return
	}
	buf, ok := r.bufs[c.ID]
	if !ok {
		buf = NewBuffer(r.cfg.Root, c.ID)
		r.bufs[c.ID] = buf
	}
	delete(r.gone, c.ID)
	tctx, cancel := context.WithCancel(parentCtx)
	r.cancels[c.ID] = cancel
	r.mu.Unlock()

	go r.tail(tctx, c.ID, buf)
}

// OnContainerStopped satisfies containerwatcher.Subscriber. Cancels the
// tailer; the Buffer file remains for the IdleDelete window so users
// can still read recent logs from a stopped container.
func (r *Registry) OnContainerStopped(_ context.Context, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel, ok := r.cancels[id]; ok {
		cancel()
		delete(r.cancels, id)
	}
	r.gone[id] = time.Now().UTC()
}

// tail is one tailer goroutine. Reads from docker.StreamContainerLogs,
// appends each line to the on-disk buffer, and publishes on the hub.
// Exits when ctx is cancelled or the stream ends.
func (r *Registry) tail(ctx context.Context, containerID string, buf *Buffer) {
	lines, errs, err := r.docker.StreamContainerLogs(ctx, containerID)
	if err != nil {
		r.logger.Warn("logbuffer: stream open failed", "container", containerID, "err", err)
		return
	}
	topic := Topic(containerID)
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-lines:
			if !ok {
				if e, ok := <-errs; ok && e != nil && !errors.Is(e, context.Canceled) {
					r.logger.Debug("logbuffer: stream ended with error", "container", containerID, "err", e)
				}
				return
			}
			line := Line{Timestamp: time.Now().UTC(), Stream: l.Stream, Line: l.Line}
			if err := buf.Append(line); err != nil {
				r.logger.Warn("logbuffer: append failed", "container", containerID, "err", err)
			}
			if r.hub != nil {
				r.hub.Publish(topic, line)
			}
		}
	}
}

// Run drives the prune + idle-delete loop. Exits on context cancel.
// Runs once at startup so retention is enforced even if the process is
// restarted with a long-existing buffer directory.
func (r *Registry) Run(ctx context.Context) error {
	r.pruneOnce()
	t := time.NewTicker(r.cfg.PruneInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			r.pruneOnce()
		}
	}
}

// pruneOnce iterates every known buffer, drops lines older than
// retention, and deletes buffers whose container has been gone past
// IdleDelete.
func (r *Registry) pruneOnce() {
	cutoff := time.Now().UTC().Add(-r.cfg.Retention)

	r.mu.Lock()
	bufs := make(map[string]*Buffer, len(r.bufs))
	for id, b := range r.bufs {
		bufs[id] = b
	}
	gone := make(map[string]time.Time, len(r.gone))
	for id, t := range r.gone {
		gone[id] = t
	}
	r.mu.Unlock()

	for id, b := range bufs {
		if dropped, err := b.Prune(cutoff); err != nil {
			r.logger.Warn("logbuffer: prune failed", "container", id, "err", err)
		} else if dropped > 0 {
			r.logger.Debug("logbuffer: pruned lines", "container", id, "dropped", dropped)
		}
	}

	idleCutoff := time.Now().UTC().Add(-r.cfg.IdleDelete)
	for id, when := range gone {
		if when.After(idleCutoff) {
			continue
		}
		if b, ok := bufs[id]; ok {
			if err := b.Delete(); err != nil {
				r.logger.Warn("logbuffer: delete failed", "container", id, "err", err)
			}
		}
		r.mu.Lock()
		delete(r.bufs, id)
		delete(r.gone, id)
		r.mu.Unlock()
	}
}

// MarshalLine renders a Line to the on-wire JSON shape the HTTP
// historical-tail endpoint and the WS publisher both use. Exposed so
// tests can build expected payloads without re-deriving the format.
func MarshalLine(l Line) ([]byte, error) { return json.Marshal(l) }
