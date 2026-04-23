package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/containerwatcher"
	"github.com/sariakos/teal/backend/internal/logbuffer"
	"github.com/sariakos/teal/backend/internal/store"
)

// logsHandler exposes persisted historical logs:
//   - GET /apps/{slug}/deployments/{id}/log         deploy.log file
//   - GET /apps/{slug}/containers                   live container set
//   - GET /containers/{id}/logs?since=&limit=       persisted container lines
//
// Live streams come through /ws (subscribe to deploy.<id> or
// containerlogs.<id>); these endpoints serve the historical replay
// the UI shows BEFORE attaching to live updates.
type logsHandler struct {
	logger      *slog.Logger
	store       *store.Store
	logbuf      *logbuffer.Registry
	watcher     *containerwatcher.Watcher
	workdirRoot string
}

// containerSummary is the wire shape returned by GET /apps/{slug}/containers.
// Used by the Logs tab to populate the container selector.
type containerSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	Color string `json:"color"`
}

// listContainers returns the current platform containers belonging to an
// App. Read from the container watcher's snapshot — no extra docker call.
func (h *logsHandler) listContainers(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := h.store.Apps.GetBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.watcher == nil {
		writeJSON(w, http.StatusOK, []containerSummary{})
		return
	}
	out := make([]containerSummary, 0, 4)
	for _, c := range h.watcher.Snapshot() {
		if c.AppSlug != slug {
			continue
		}
		out = append(out, containerSummary{
			ID: c.ID, Name: c.Name, Image: c.Image, Color: string(c.Color),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// containerLogs returns persisted log lines for one container. The UI
// calls this on tab open; for live updates it subscribes via /ws.
func (h *logsHandler) containerLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing container id")
		return
	}
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "since: "+err.Error())
		return
	}
	limit := 1000
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10000 {
			writeError(w, http.StatusBadRequest, "limit must be 1..10000")
			return
		}
		limit = n
	}
	if h.logbuf == nil {
		writeJSON(w, http.StatusOK, []logbuffer.Line{})
		return
	}
	buf := h.logbuf.Buffer(id)
	if buf == nil {
		writeJSON(w, http.StatusOK, []logbuffer.Line{})
		return
	}
	rows, err := buf.Tail(since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read logs: "+err.Error())
		return
	}
	if rows == nil {
		rows = []logbuffer.Line{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// deploymentLog streams the persisted deploy.log file for a finished
// (or in-progress) deployment. Plain text — the UI renders it in a
// monospace block.
func (h *logsHandler) deploymentLog(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	idStr := chi.URLParam(r, "id")
	depID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "deployment id must be numeric")
		return
	}
	dep, err := h.store.Deployments.Get(r.Context(), depID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "deployment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dep.AppID != app.ID {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	path := filepath.Join(h.workdirRoot, "deploys", app.Slug, strconv.FormatInt(dep.ID, 10), "deploy.log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "deploy log not available (workdir pruned or never written)")
			return
		}
		writeError(w, http.StatusInternalServerError, "open log: "+err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := copyToResponse(w, f); err != nil {
		h.logger.Debug("deploy log stream interrupted", "deployment_id", depID, "err", err)
	}
}

// parseSince accepts either an RFC3339 timestamp or a duration ("5m",
// "1h"). Duration is interpreted as "since now-d". Empty returns the
// zero time (no filter).
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC().Add(-d), nil
}
