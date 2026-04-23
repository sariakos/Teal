package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/sariakos/teal/backend/internal/store"
)

// Downsampler folds raw metric samples older than RawRetention into
// 1-minute buckets in metrics_samples_1m, then prunes 1m buckets older
// than TotalRetention.
//
// Default: raw 6h, total 24h. Aggregator runs every 5min.
type Downsampler struct {
	logger *slog.Logger
	repo   *store.MetricsRepo

	cfg DownsampleConfig
}

// DownsampleConfig tunes the aggregator.
type DownsampleConfig struct {
	RawRetention   time.Duration // raw samples older than this get aggregated; default 6h
	TotalRetention time.Duration // 1m buckets older than this are dropped; default 24h
	Interval       time.Duration // how often to run; default 5min
}

const (
	defaultRawRetention   = 6 * time.Hour
	defaultTotalRetention = 24 * time.Hour
	defaultDownInterval   = 5 * time.Minute
)

// NewDownsampler wires up the aggregator. The Scraper handles raw
// inserts + raw prune; the Downsampler handles raw→1m + 1m prune.
// Two goroutines, two responsibilities — keeps each loop small.
func NewDownsampler(logger *slog.Logger, repo *store.MetricsRepo, cfg DownsampleConfig) *Downsampler {
	if cfg.RawRetention == 0 {
		cfg.RawRetention = defaultRawRetention
	}
	if cfg.TotalRetention == 0 {
		cfg.TotalRetention = defaultTotalRetention
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultDownInterval
	}
	return &Downsampler{logger: logger, repo: repo, cfg: cfg}
}

// Run drives the aggregate + prune loop.
func (d *Downsampler) Run(ctx context.Context) error {
	d.tick(ctx)
	t := time.NewTicker(d.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			d.tick(ctx)
		}
	}
}

func (d *Downsampler) tick(ctx context.Context) {
	now := time.Now().UTC()
	// Aggregate everything older than RawRetention up to a minute ago
	// (don't touch the very recent edge while the scraper is still
	// writing). Window: (-inf, now - RawRetention]
	cutoff := now.Add(-d.cfg.RawRetention)
	farPast := now.Add(-d.cfg.TotalRetention)

	if n, err := d.repo.Downsample(ctx, farPast, cutoff); err != nil {
		d.logger.Warn("metrics: downsample failed", "err", err)
	} else if n > 0 {
		d.logger.Debug("metrics: downsampled buckets", "count", n)
	}
	if n, err := d.repo.PruneAggregates(ctx, farPast); err != nil {
		d.logger.Warn("metrics: prune aggregates failed", "err", err)
	} else if n > 0 {
		d.logger.Debug("metrics: pruned 1m buckets", "count", n)
	}
}
