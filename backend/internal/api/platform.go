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
	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// platformSummaryResponse drives the dashboard top widget.
type platformSummaryResponse struct {
	AppCount           int                       `json:"appCount"`
	RunningContainers  int                       `json:"runningContainers"`
	TotalDiskBytes     int64                     `json:"totalDiskBytes"`
	WorkdirDiskBytes   int64                     `json:"workdirDiskBytes"`
	RecentFailures     []recentFailureResponse   `json:"recentFailures"`
}

type recentFailureResponse struct {
	AppSlug       string    `json:"appSlug"`
	DeploymentID  int64     `json:"deploymentId"`
	FailureReason string    `json:"failureReason"`
	CompletedAt   time.Time `json:"completedAt"`
}

// platformHandler serves the dashboard summary and the self-update
// endpoint. Both are admin-only at the router layer.
type platformHandler struct {
	logger      *slog.Logger
	store       *store.Store
	docker      docker.Client
	watcher     *containerwatcher.Watcher
	workdirRoot string

	// exit is the function called by the self-update handler. Defined
	// as a field so tests can substitute a no-op (otherwise the test
	// process would terminate).
	exit func(int)
}

// summary aggregates counts the dashboard renders. Best-effort: if the
// Docker call fails we still return everything else.
func (h *platformHandler) summary(w http.ResponseWriter, r *http.Request) {
	apps, err := h.store.Apps.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list apps: "+err.Error())
		return
	}
	resp := platformSummaryResponse{AppCount: len(apps)}

	if h.watcher != nil {
		resp.RunningContainers = len(h.watcher.Snapshot())
	} else if h.docker != nil {
		// Fallback: ask Docker directly. Filter to running only.
		all, err := h.docker.ListContainers(r.Context())
		if err == nil {
			for _, c := range all {
				if c.State == "running" {
					resp.RunningContainers++
				}
			}
		}
	}

	if h.docker != nil {
		vols, err := h.docker.ListVolumes(r.Context())
		if err == nil {
			// We can't ask the daemon for per-volume size cheaply (the
			// API doesn't return Usage unless you call SystemDf); for
			// v1 just count volumes. Real disk usage comes from
			// du -s on the workdir below.
			_ = vols
		}
	}
	if h.workdirRoot != "" {
		resp.WorkdirDiskBytes = dirSize(h.workdirRoot)
		resp.TotalDiskBytes = resp.WorkdirDiskBytes
	}

	// Recent failures: scan the last 50 deployments for any failed.
	recent, err := h.store.Deployments.List(r.Context(), 50)
	if err == nil {
		appByID := map[int64]domain.App{}
		for _, a := range apps {
			appByID[a.ID] = a
		}
		for _, d := range recent {
			if d.Status != domain.DeploymentStatusFailed {
				continue
			}
			ts := time.Time{}
			if d.CompletedAt != nil {
				ts = *d.CompletedAt
			}
			a := appByID[d.AppID]
			resp.RecentFailures = append(resp.RecentFailures, recentFailureResponse{
				AppSlug: a.Slug, DeploymentID: d.ID, FailureReason: d.FailureReason, CompletedAt: ts,
			})
			if len(resp.RecentFailures) >= 5 {
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// selfUpdate writes a marker file inside the workdir and exits the
// process so the supervisor (systemd / docker `restart: unless-stopped`)
// brings the new image up. Requires ?confirm=update-platform to mirror
// volume delete's typed confirmation.
func (h *platformHandler) selfUpdate(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("confirm") != "update-platform" {
		writeError(w, http.StatusBadRequest, "confirm query parameter must equal 'update-platform'")
		return
	}
	markerDir := filepath.Join(h.workdirRoot, "platform")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "marker dir: "+err.Error())
		return
	}
	marker := filepath.Join(markerDir, "restart.requested")
	if err := os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "write marker: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionPlatformSettingSet, "platform", "self_update",
		clientIP(r), "self-update requested", "")
	_, _ = h.store.Notifications.Insert(r.Context(), domain.Notification{
		Level: domain.NotificationInfo, Kind: domain.NotificationKindPlatformUpdate,
		Title: "Platform restart requested",
		Body:  "Marker written. Supervisor will restart the platform momentarily.",
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "restart requested",
		"marker":  marker,
		"message": "the platform will exit in ~1s; your supervisor (systemd / docker compose) is expected to restart it",
	})
	// Schedule the exit so the response actually flushes.
	go func() {
		time.Sleep(1 * time.Second)
		if h.exit != nil {
			h.exit(0)
			return
		}
		os.Exit(0)
	}()
}

// rotateNotificationSecret issues a fresh outbound webhook secret and
// returns the raw value once. Mirrors the github-webhook secret pattern.
func (h *appsHandler) rotateNotificationSecret(w http.ResponseWriter, r *http.Request) {
	app, ok := h.lookupAppFromRequest(w, r)
	if !ok {
		return
	}
	raw, enc, err := h.newWebhookSecret(app.ID) // reuse the same generator shape
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate secret: "+err.Error())
		return
	}
	// Re-seal under the OUTBOUND purpose and AAD so the inbound and
	// outbound secrets can't accidentally share a derived key.
	enc, err = h.codec.Seal("webhook.outbound", "app:"+strconv.FormatInt(app.ID, 10)+":notify", []byte(raw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "seal secret: "+err.Error())
		return
	}
	app.NotificationWebhookSecretEncrypted = enc
	if err := h.store.Apps.Update(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "persist secret: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionAppUpdate, "app", strconv.FormatInt(app.ID, 10),
		clientIP(r), "rotated notification webhook secret", "")
	writeJSON(w, http.StatusOK, map[string]string{"webhookSecret": raw})
}

// lookupAppFromRequest is the helper apps endpoints share to resolve
// {slug}. Encapsulates the not-found / internal-error error mapping.
func (h *appsHandler) lookupAppFromRequest(w http.ResponseWriter, r *http.Request) (domain.App, bool) {
	slug := chi.URLParam(r, "slug")
	app, err := h.store.Apps.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return domain.App{}, false
	}
	return app, true
}

// dirSize returns the total bytes used by a directory tree. Best-
// effort: errors return the partial total. Used for the dashboard's
// platform-disk widget.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
