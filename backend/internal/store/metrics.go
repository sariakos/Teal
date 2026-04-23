package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

// MetricsRepo persists domain.MetricSample. Inserts run in a single
// transaction so a slow scraper doesn't fan out into one round-trip per
// container.
type MetricsRepo struct {
	db *sql.DB
}

// Insert writes a batch of samples. Empty batch is a no-op.
func (r *MetricsRepo) Insert(ctx context.Context, batch []domain.MetricSample) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO metrics_samples
		    (container_id, container_name, app_slug, color, ts,
		     cpu_pct, mem_bytes, mem_limit, net_rx, net_tx, blk_rx, blk_tx)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("metrics: prepare: %w", err)
	}
	defer stmt.Close()

	for _, s := range batch {
		if _, err := stmt.ExecContext(ctx,
			s.ContainerID, s.ContainerName, s.AppSlug, string(s.Color),
			formatTime(s.TS),
			s.CPUPercent, s.MemBytes, s.MemLimit,
			s.NetRx, s.NetTx, s.BlkRx, s.BlkTx,
		); err != nil {
			return fmt.Errorf("metrics: insert: %w", err)
		}
	}
	return tx.Commit()
}

// LatestForApp returns samples for an App since the given cutoff, oldest
// first so the UI can render a left-to-right chart without re-sorting.
func (r *MetricsRepo) LatestForApp(ctx context.Context, slug string, since time.Time) ([]domain.MetricSample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+metricColumns+`
		FROM metrics_samples
		WHERE app_slug = ? AND ts >= ?
		ORDER BY ts ASC`,
		slug, formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("metrics: list for app: %w", err)
	}
	defer rows.Close()
	return scanMetrics(rows)
}

// LatestForContainer is the per-container read path used by the per-app
// view (one series per container).
func (r *MetricsRepo) LatestForContainer(ctx context.Context, containerID string, since time.Time) ([]domain.MetricSample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+metricColumns+`
		FROM metrics_samples
		WHERE container_id = ? AND ts >= ?
		ORDER BY ts ASC`,
		containerID, formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("metrics: list for container: %w", err)
	}
	defer rows.Close()
	return scanMetrics(rows)
}

// Prune deletes samples older than `before`. Returns the number of rows
// removed so the caller can log it.
func (r *MetricsRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM metrics_samples WHERE ts < ?`, formatTime(before))
	if err != nil {
		return 0, fmt.Errorf("metrics: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Downsample folds raw samples in [from, to) into 1-minute averages and
// upserts them into metrics_samples_1m. After a successful run the raw
// rows in that window are deleted.
//
// Aggregation rules:
//   - cpu_pct, mem_bytes: arithmetic mean
//   - mem_limit, net_*, blk_*: last value in the window (cumulative
//     counters; the most recent sample is the cleanest summary)
//   - sample_count: COUNT(*)
//
// Buckets are minute-aligned: ts truncated to minute. Idempotent:
// running twice over the same window is a no-op (ON CONFLICT DO UPDATE
// matches the unique index).
func (r *MetricsRepo) Downsample(ctx context.Context, from, to time.Time) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT
		    container_id, container_name, app_slug, color,
		    strftime('%Y-%m-%dT%H:%M:00.000000000Z', ts) AS bucket,
		    AVG(cpu_pct), AVG(mem_bytes),
		    MAX(mem_limit),
		    MAX(net_rx), MAX(net_tx), MAX(blk_rx), MAX(blk_tx),
		    COUNT(*)
		FROM metrics_samples
		WHERE ts >= ? AND ts < ?
		GROUP BY container_id, bucket`,
		formatTime(from), formatTime(to))
	if err != nil {
		return 0, fmt.Errorf("metrics: downsample query: %w", err)
	}
	type aggRow struct {
		ContainerID, ContainerName, AppSlug, Color, Bucket string
		CPU, Mem                                           float64
		MemLimit, NetRx, NetTx, BlkRx, BlkTx, Count        int64
	}
	var aggs []aggRow
	for rows.Next() {
		var a aggRow
		if err := rows.Scan(&a.ContainerID, &a.ContainerName, &a.AppSlug, &a.Color, &a.Bucket,
			&a.CPU, &a.Mem, &a.MemLimit, &a.NetRx, &a.NetTx, &a.BlkRx, &a.BlkTx, &a.Count); err != nil {
			rows.Close()
			return 0, err
		}
		aggs = append(aggs, a)
	}
	rows.Close()
	if len(aggs) == 0 {
		// Nothing to aggregate — still drop the (empty) raw window so
		// callers can use this as a vacuum step too.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM metrics_samples WHERE ts >= ? AND ts < ?`,
			formatTime(from), formatTime(to)); err != nil {
			return 0, err
		}
		return 0, tx.Commit()
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO metrics_samples_1m
		    (container_id, container_name, app_slug, color, bucket_ts,
		     cpu_pct_avg, mem_bytes_avg, mem_limit, net_rx, net_tx, blk_rx, blk_tx, sample_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(container_id, bucket_ts) DO UPDATE SET
		    cpu_pct_avg = excluded.cpu_pct_avg,
		    mem_bytes_avg = excluded.mem_bytes_avg,
		    mem_limit = excluded.mem_limit,
		    net_rx = excluded.net_rx,
		    net_tx = excluded.net_tx,
		    blk_rx = excluded.blk_rx,
		    blk_tx = excluded.blk_tx,
		    sample_count = excluded.sample_count`)
	if err != nil {
		return 0, fmt.Errorf("metrics: downsample prepare: %w", err)
	}
	defer stmt.Close()

	for _, a := range aggs {
		if _, err := stmt.ExecContext(ctx,
			a.ContainerID, a.ContainerName, a.AppSlug, a.Color, a.Bucket,
			a.CPU, int64(a.Mem), a.MemLimit, a.NetRx, a.NetTx, a.BlkRx, a.BlkTx, a.Count); err != nil {
			return 0, fmt.Errorf("metrics: downsample insert: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM metrics_samples WHERE ts >= ? AND ts < ?`,
		formatTime(from), formatTime(to)); err != nil {
		return 0, fmt.Errorf("metrics: downsample drop raw: %w", err)
	}
	return int64(len(aggs)), tx.Commit()
}

// PruneAggregates drops 1-minute buckets older than `before`. Same
// shape as Prune for the raw table.
func (r *MetricsRepo) PruneAggregates(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM metrics_samples_1m WHERE bucket_ts < ?`, formatTime(before))
	if err != nil {
		return 0, fmt.Errorf("metrics: prune aggregates: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// LatestForAppAggregated reads from the 1m table — used when the API
// requests resolution=1m for a longer window than the raw retention
// would cover.
func (r *MetricsRepo) LatestForAppAggregated(ctx context.Context, slug string, since time.Time) ([]domain.MetricSample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, container_id, container_name, app_slug, color, bucket_ts,
		       cpu_pct_avg, mem_bytes_avg, mem_limit, net_rx, net_tx, blk_rx, blk_tx
		FROM metrics_samples_1m
		WHERE app_slug = ? AND bucket_ts >= ?
		ORDER BY bucket_ts ASC`,
		slug, formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("metrics: list 1m for app: %w", err)
	}
	defer rows.Close()
	return scanMetrics(rows)
}

const metricColumns = `id, container_id, container_name, app_slug, color, ts,
	cpu_pct, mem_bytes, mem_limit, net_rx, net_tx, blk_rx, blk_tx`

func scanMetrics(rows *sql.Rows) ([]domain.MetricSample, error) {
	var out []domain.MetricSample
	for rows.Next() {
		var (
			s         domain.MetricSample
			colorStr  string
			tsStr     string
		)
		if err := rows.Scan(&s.ID, &s.ContainerID, &s.ContainerName, &s.AppSlug,
			&colorStr, &tsStr,
			&s.CPUPercent, &s.MemBytes, &s.MemLimit,
			&s.NetRx, &s.NetTx, &s.BlkRx, &s.BlkTx); err != nil {
			return nil, err
		}
		s.Color = domain.Color(colorStr)
		t, err := parseTime(tsStr)
		if err != nil {
			return nil, err
		}
		s.TS = t
		out = append(out, s)
	}
	return out, rows.Err()
}
