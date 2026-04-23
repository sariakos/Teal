package domain

import "time"

// MetricSample is one point on a container's time-series. Captured by the
// metrics scraper goroutine on a fixed interval (default 15s) and stored
// in the metrics_samples table. v1 stores raw samples only — downsampling
// is deferred to Phase 7.
type MetricSample struct {
	ID            int64
	ContainerID   string
	ContainerName string

	AppSlug string
	Color   Color

	TS time.Time

	CPUPercent float64
	MemBytes   int64
	MemLimit   int64
	NetRx      int64
	NetTx      int64
	BlkRx      int64
	BlkTx      int64
}
