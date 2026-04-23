// Package containerwatcher polls Docker for the set of containers Teal
// considers "platform-managed" and fans out start/stop events to
// subscribers. Both the metrics scraper and the log tailer registry use
// it so the discovery logic lives in exactly one place.
//
// A container is platform-managed iff its `com.docker.compose.project`
// label matches `<slug>-blue` or `<slug>-green` for some App slug. We
// derive both AppSlug and Color from the label rather than calling the
// store, so the watcher does not depend on persistence and can be tested
// with just a fake docker.Client.
package containerwatcher

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
)

// DefaultInterval is the poll cadence when EngineConfig.Interval is zero.
// Two seconds is a balance between catching short-lived containers and
// not hammering the Docker socket for long-lived ones.
const DefaultInterval = 2 * time.Second

// Container is the watcher's view of one platform-managed container. It
// carries just the fields downstream subscribers need; if a subscriber
// later wants more, add it here rather than passing the raw SDK type.
type Container struct {
	ID    string
	Name  string // with leading "/" — Docker's convention
	Image string

	AppSlug string
	Color   domain.Color

	// LabelProject is the raw Compose project label we matched on. Kept
	// for diagnostics — log lines like "saw new container in project X".
	LabelProject string
}

// Subscriber receives start/stop callbacks. Both run on the watcher's
// goroutine — keep them fast (forward to channels if work is heavy).
type Subscriber interface {
	OnContainerStarted(ctx context.Context, c Container)
	OnContainerStopped(ctx context.Context, id string)
}

// Watcher diffs the docker container set every Interval and notifies
// subscribers. Construct with New, register subscribers, then call Run.
type Watcher struct {
	docker   docker.Client
	logger   *slog.Logger
	interval time.Duration

	mu          sync.Mutex
	subscribers []Subscriber
	known       map[string]Container // ID -> last-seen container snapshot
}

// New constructs a Watcher. Interval defaults to DefaultInterval if zero.
func New(logger *slog.Logger, dock docker.Client, interval time.Duration) *Watcher {
	if interval == 0 {
		interval = DefaultInterval
	}
	return &Watcher{
		docker:   dock,
		logger:   logger,
		interval: interval,
		known:    map[string]Container{},
	}
}

// Subscribe registers a subscriber. Safe to call before Run; calling
// during Run is supported but the subscriber will not receive Started
// events for already-known containers — register subscribers up front.
func (w *Watcher) Subscribe(s Subscriber) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subscribers = append(w.subscribers, s)
}

// Run polls until ctx is cancelled. Returns nil on clean shutdown.
func (w *Watcher) Run(ctx context.Context) error {
	t := time.NewTicker(w.interval)
	defer t.Stop()

	w.tick(ctx) // immediate first scan so subscribers wake up without waiting an interval
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// tick is one poll. Errors are logged but never fatal — a transient
// docker outage shouldn't kill the watcher, which would silently halt
// metrics + log streaming.
func (w *Watcher) tick(ctx context.Context) {
	raw, err := w.docker.ListContainers(ctx)
	if err != nil {
		w.logger.Warn("containerwatcher: list failed", "err", err)
		return
	}

	current := map[string]Container{}
	for _, c := range raw {
		project := c.Labels["com.docker.compose.project"]
		slug, color, ok := splitProject(project)
		if !ok {
			continue
		}
		// Skip non-running for the started-set; we still want to surface
		// stops for them, which the diff below handles by absence.
		if c.State != "running" {
			continue
		}
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		current[c.ID] = Container{
			ID:           c.ID,
			Name:         name,
			Image:        c.Image,
			AppSlug:      slug,
			Color:        color,
			LabelProject: project,
		}
	}

	w.mu.Lock()
	prev := w.known
	w.known = current
	subs := append([]Subscriber(nil), w.subscribers...)
	w.mu.Unlock()

	for id, c := range current {
		if _, was := prev[id]; !was {
			for _, s := range subs {
				s.OnContainerStarted(ctx, c)
			}
		}
	}
	for id := range prev {
		if _, still := current[id]; !still {
			for _, s := range subs {
				s.OnContainerStopped(ctx, id)
			}
		}
	}
}

// Snapshot returns a copy of the current known set. Used by API handlers
// that need the live container list without polling Docker themselves.
func (w *Watcher) Snapshot() []Container {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Container, 0, len(w.known))
	for _, c := range w.known {
		out = append(out, c)
	}
	return out
}

// splitProject parses "<slug>-<color>" into its parts. Returns ok=false
// for any project label that doesn't end in `-blue` or `-green` or whose
// slug component is empty. Slugs can themselves contain hyphens
// (validSlug allows it), so we only split on the suffix.
func splitProject(project string) (slug string, color domain.Color, ok bool) {
	const blueSuffix = "-blue"
	const greenSuffix = "-green"
	switch {
	case strings.HasSuffix(project, blueSuffix):
		s := project[:len(project)-len(blueSuffix)]
		if s == "" {
			return "", "", false
		}
		return s, domain.ColorBlue, true
	case strings.HasSuffix(project, greenSuffix):
		s := project[:len(project)-len(greenSuffix)]
		if s == "" {
			return "", "", false
		}
		return s, domain.ColorGreen, true
	}
	return "", "", false
}
