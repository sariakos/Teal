package store

import (
	"context"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

func TestMetricsInsertAndRead(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	batch := []domain.MetricSample{
		{ContainerID: "c1", ContainerName: "/svc-blue-web-1", AppSlug: "svc", Color: domain.ColorBlue,
			TS: now.Add(-30 * time.Second), CPUPercent: 12.5, MemBytes: 1 << 20, MemLimit: 1 << 30},
		{ContainerID: "c1", ContainerName: "/svc-blue-web-1", AppSlug: "svc", Color: domain.ColorBlue,
			TS: now, CPUPercent: 13.0, MemBytes: 2 << 20, MemLimit: 1 << 30},
		{ContainerID: "c2", ContainerName: "/other-blue-api-1", AppSlug: "other", Color: domain.ColorBlue,
			TS: now, CPUPercent: 1.0, MemBytes: 100, MemLimit: 1 << 30},
	}
	if err := st.Metrics.Insert(ctx, batch); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rows, err := st.Metrics.LatestForApp(ctx, "svc", now.Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("LatestForApp: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("LatestForApp len: %d", len(rows))
	}
	if !rows[0].TS.Before(rows[1].TS) {
		t.Errorf("rows not oldest-first: %+v", rows)
	}
	if rows[1].CPUPercent != 13.0 {
		t.Errorf("CPU read-back: %v", rows[1].CPUPercent)
	}

	per, _ := st.Metrics.LatestForContainer(ctx, "c1", now.Add(-1*time.Minute))
	if len(per) != 2 {
		t.Errorf("per-container: %d", len(per))
	}

	// Prune everything older than `now` — keeps only the second row for c1
	// and c2's row at `now`.
	n, err := st.Metrics.Prune(ctx, now)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Errorf("Prune removed: %d (want 1)", n)
	}

	rows, _ = st.Metrics.LatestForApp(ctx, "svc", now.Add(-1*time.Minute))
	if len(rows) != 1 {
		t.Errorf("after prune: %d", len(rows))
	}
}

func TestMetricsInsertEmptyIsNoop(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if err := st.Metrics.Insert(ctx, nil); err != nil {
		t.Fatalf("Insert nil: %v", err)
	}
	if err := st.Metrics.Insert(ctx, []domain.MetricSample{}); err != nil {
		t.Fatalf("Insert empty: %v", err)
	}
}

func TestMetricsDownsampleAndAggregateRead(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	base := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	// Seed 5 raw samples within the same minute → expect 1 bucket avg.
	for i := 0; i < 5; i++ {
		_ = st.Metrics.Insert(ctx, []domain.MetricSample{{
			ContainerID: "c1", ContainerName: "/svc-blue-web-1",
			AppSlug: "svc", Color: domain.ColorBlue,
			TS:      base.Add(time.Duration(i*10) * time.Second),
			CPUPercent: 10.0 + float64(i),
			MemBytes: int64(1024 * (i + 1)),
		}})
	}
	// And 2 samples in the NEXT minute → expect a second bucket.
	for i := 0; i < 2; i++ {
		_ = st.Metrics.Insert(ctx, []domain.MetricSample{{
			ContainerID: "c1", ContainerName: "/svc-blue-web-1",
			AppSlug: "svc", Color: domain.ColorBlue,
			TS:      base.Add(time.Minute + time.Duration(i*30)*time.Second),
			CPUPercent: 30.0,
		}})
	}

	from := base.Add(-time.Hour)
	to := base.Add(2 * time.Minute)
	n, err := st.Metrics.Downsample(ctx, from, to)
	if err != nil {
		t.Fatalf("Downsample: %v", err)
	}
	if n != 2 {
		t.Errorf("buckets = %d, want 2", n)
	}

	// Raw rows for that window should be gone.
	raw, _ := st.Metrics.LatestForApp(ctx, "svc", from)
	if len(raw) != 0 {
		t.Errorf("raw remained after downsample: %d", len(raw))
	}

	// Aggregate read returns 2 rows.
	agg, err := st.Metrics.LatestForAppAggregated(ctx, "svc", from)
	if err != nil {
		t.Fatalf("LatestForAppAggregated: %v", err)
	}
	if len(agg) != 2 {
		t.Fatalf("aggregated rows: %d", len(agg))
	}
	// First bucket avg CPU = (10+11+12+13+14)/5 = 12.0
	if agg[0].CPUPercent != 12.0 {
		t.Errorf("first bucket cpu avg = %v, want 12.0", agg[0].CPUPercent)
	}

	// Re-running is a no-op (raw rows already gone for that window).
	n2, err := st.Metrics.Downsample(ctx, from, to)
	if err != nil {
		t.Fatalf("Downsample twice: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second pass returned %d, want 0", n2)
	}
}
