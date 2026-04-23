package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// metricsHandler serves persisted metric samples for the Overview chart
// and the per-container details. Live updates come over WS via the
// metrics.<container-id> topic.
type metricsHandler struct {
	logger *slog.Logger
	store  *store.Store
}

// metricSampleResponse mirrors domain.MetricSample but renames Color to
// "color" and ts as ISO-8601 (the JSON encoder handles the time field).
type metricSampleResponse struct {
	ContainerID   string    `json:"containerId"`
	ContainerName string    `json:"containerName"`
	AppSlug       string    `json:"appSlug"`
	Color         string    `json:"color"`
	TS            time.Time `json:"ts"`

	CPUPercent float64 `json:"cpuPct"`
	MemBytes   int64   `json:"memBytes"`
	MemLimit   int64   `json:"memLimit"`
	NetRx      int64   `json:"netRx"`
	NetTx      int64   `json:"netTx"`
	BlkRx      int64   `json:"blkRx"`
	BlkTx      int64   `json:"blkTx"`
}

func sampleToResponse(s domain.MetricSample) metricSampleResponse {
	return metricSampleResponse{
		ContainerID: s.ContainerID, ContainerName: s.ContainerName,
		AppSlug: s.AppSlug, Color: string(s.Color), TS: s.TS,
		CPUPercent: s.CPUPercent, MemBytes: s.MemBytes, MemLimit: s.MemLimit,
		NetRx: s.NetRx, NetTx: s.NetTx, BlkRx: s.BlkRx, BlkTx: s.BlkTx,
	}
}

// list returns samples for an App since the given cutoff. Default
// window is 30 minutes. Per-container series can be derived by the UI
// (group by ContainerID); the response is one flat list to keep the
// wire shape simple.
func (h *metricsHandler) list(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := h.store.Apps.GetBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "since: "+err.Error())
		return
	}
	if since.IsZero() {
		since = time.Now().UTC().Add(-30 * time.Minute)
	}
	resolution := r.URL.Query().Get("resolution")
	var (
		rows []domain.MetricSample
	)
	switch resolution {
	case "", "raw":
		rows, err = h.store.Metrics.LatestForApp(r.Context(), slug, since)
	case "1m":
		rows, err = h.store.Metrics.LatestForAppAggregated(r.Context(), slug, since)
	default:
		writeError(w, http.StatusBadRequest, "resolution must be raw or 1m")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read metrics: "+err.Error())
		return
	}
	out := make([]metricSampleResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, sampleToResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}
