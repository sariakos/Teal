// Package metrics polls Docker for per-container resource samples,
// persists them to SQLite via store.MetricsRepo, and republishes each
// sample on the realtime hub for live UI subscribers.
//
// Scope: only "platform-managed" containers (those whose Compose
// project label matches an App slug). The container watcher tells us
// which IDs to track; we don't list independently.
package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sariakos/teal/backend/internal/containerwatcher"
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// Publisher mirrors the realtime hub's Publish signature; defined here
// so tests can capture publishes without the real hub.
type Publisher interface {
	Publish(topic string, data any)
}

// Topic returns the realtime topic for a container's metric stream.
// Must agree with the API layer's WS topic naming.
func Topic(containerID string) string { return "metrics." + containerID }

// Config tunes the scraper. Zero values fall back to documented
// defaults.
type Config struct {
	Interval        time.Duration // poll cadence; default 15s
	Retention       time.Duration // prune cutoff; default 6h
	PruneInterval   time.Duration // how often to prune; default 5min
	MinInterval     time.Duration // floor on Interval; default 5s (safety)
	StatsTimeout    time.Duration // per-container stats call timeout; default 5s
}

const (
	defaultInterval      = 15 * time.Second
	defaultRetention     = 6 * time.Hour
	defaultPruneInterval = 5 * time.Minute
	defaultMinInterval   = 5 * time.Second
	defaultStatsTimeout  = 5 * time.Second
)

// Scraper drives the per-tick fan-out. Construct with New, register on
// a containerwatcher, then call Run.
type Scraper struct {
	cfg    Config
	docker docker.Client
	repo   *store.MetricsRepo
	hub    Publisher
	logger *slog.Logger

	mu         sync.Mutex
	containers map[string]containerwatcher.Container // id -> latest snapshot
}

// New constructs a Scraper. cfg is normalised in place; pass &Config{}
// for full defaults.
func New(logger *slog.Logger, dock docker.Client, repo *store.MetricsRepo, hub Publisher, cfg Config) *Scraper {
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Retention == 0 {
		cfg.Retention = defaultRetention
	}
	if cfg.PruneInterval == 0 {
		cfg.PruneInterval = defaultPruneInterval
	}
	if cfg.MinInterval == 0 {
		cfg.MinInterval = defaultMinInterval
	}
	if cfg.Interval < cfg.MinInterval {
		cfg.Interval = cfg.MinInterval
	}
	if cfg.StatsTimeout == 0 {
		cfg.StatsTimeout = defaultStatsTimeout
	}
	return &Scraper{
		cfg:        cfg,
		docker:     dock,
		repo:       repo,
		hub:        hub,
		logger:     logger,
		containers: map[string]containerwatcher.Container{},
	}
}

// OnContainerStarted satisfies containerwatcher.Subscriber.
func (s *Scraper) OnContainerStarted(_ context.Context, c containerwatcher.Container) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers[c.ID] = c
}

// OnContainerStopped satisfies containerwatcher.Subscriber.
func (s *Scraper) OnContainerStopped(_ context.Context, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.containers, id)
}

// Run polls every Interval until ctx is cancelled. A separate ticker
// drives prune at PruneInterval.
func (s *Scraper) Run(ctx context.Context) error {
	scrapeT := time.NewTicker(s.cfg.Interval)
	defer scrapeT.Stop()
	pruneT := time.NewTicker(s.cfg.PruneInterval)
	defer pruneT.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-scrapeT.C:
			s.scrapeOnce(ctx)
		case <-pruneT.C:
			s.pruneOnce(ctx)
		}
	}
}

// scrapeOnce takes a snapshot of the current container set, queries
// stats for each, persists in one transaction, and publishes per-id.
func (s *Scraper) scrapeOnce(ctx context.Context) {
	s.mu.Lock()
	snapshot := make([]containerwatcher.Container, 0, len(s.containers))
	for _, c := range s.containers {
		snapshot = append(snapshot, c)
	}
	s.mu.Unlock()

	if len(snapshot) == 0 {
		return
	}

	now := time.Now().UTC()
	batch := make([]domain.MetricSample, 0, len(snapshot))
	for _, c := range snapshot {
		stats, err := s.statsWithTimeout(ctx, c.ID)
		if err != nil {
			s.logger.Debug("metrics: stats failed", "container", c.ID, "err", err)
			continue
		}
		sample := domain.MetricSample{
			ContainerID:   c.ID,
			ContainerName: c.Name,
			AppSlug:       c.AppSlug,
			Color:         c.Color,
			TS:            now,
			CPUPercent:    stats.CPUPercent,
			MemBytes:      stats.MemBytes,
			MemLimit:      stats.MemLimit,
			NetRx:         stats.NetRx,
			NetTx:         stats.NetTx,
			BlkRx:         stats.BlkRead,
			BlkTx:         stats.BlkWrite,
		}
		batch = append(batch, sample)
		if s.hub != nil {
			s.hub.Publish(Topic(c.ID), sample)
		}
	}
	if err := s.repo.Insert(ctx, batch); err != nil {
		s.logger.Warn("metrics: persist failed", "err", err)
	}
}

func (s *Scraper) statsWithTimeout(ctx context.Context, id string) (docker.ContainerStats, error) {
	c, cancel := context.WithTimeout(ctx, s.cfg.StatsTimeout)
	defer cancel()
	return s.docker.ContainerStats(c, id)
}

func (s *Scraper) pruneOnce(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-s.cfg.Retention)
	if n, err := s.repo.Prune(ctx, cutoff); err != nil {
		s.logger.Warn("metrics: prune failed", "err", err)
	} else if n > 0 {
		s.logger.Debug("metrics: pruned samples", "count", n)
	}
}
