package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// notificationResponse is the wire shape for the notification feed.
type notificationResponse struct {
	ID        int64      `json:"id"`
	Level     string     `json:"level"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	AppSlug   string     `json:"appSlug,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	UserScope string     `json:"userScope"` // "you" | "broadcast"
}

func notificationToResponse(n domain.Notification, viewerID int64) notificationResponse {
	scope := "broadcast"
	if n.UserID != nil && *n.UserID == viewerID {
		scope = "you"
	}
	return notificationResponse{
		ID: n.ID, Level: string(n.Level), Kind: string(n.Kind),
		Title: n.Title, Body: n.Body, AppSlug: n.AppSlug,
		CreatedAt: n.CreatedAt, ReadAt: n.ReadAt, UserScope: scope,
	}
}

// notificationsHandler exposes the user's feed. Admins additionally
// see broadcast notifications (user_id IS NULL); non-admins do not.
type notificationsHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func (h *notificationsHandler) list(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	if subj.IsZero() {
		writeError(w, http.StatusUnauthorized, "auth required")
		return
	}
	includeBroadcasts := subj.Role == domain.UserRoleAdmin
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit must be 1..200")
			return
		}
		limit = n
	}
	rows, err := h.store.Notifications.ListForUser(r.Context(), subj.UserID, includeBroadcasts, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list notifications: "+err.Error())
		return
	}
	out := make([]notificationResponse, 0, len(rows))
	for _, n := range rows {
		out = append(out, notificationToResponse(n, subj.UserID))
	}
	unread, _ := h.store.Notifications.CountUnreadForUser(r.Context(), subj.UserID, includeBroadcasts)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"unread": unread,
	})
}

func (h *notificationsHandler) markRead(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	if subj.IsZero() {
		writeError(w, http.StatusUnauthorized, "auth required")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be numeric")
		return
	}
	includeBroadcasts := subj.Role == domain.UserRoleAdmin
	if err := h.store.Notifications.MarkRead(r.Context(), id, subj.UserID, includeBroadcasts); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notification not found or already read")
			return
		}
		writeError(w, http.StatusInternalServerError, "mark read: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *notificationsHandler) markAllRead(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	if subj.IsZero() {
		writeError(w, http.StatusUnauthorized, "auth required")
		return
	}
	includeBroadcasts := subj.Role == domain.UserRoleAdmin
	if err := h.store.Notifications.MarkAllReadForUser(r.Context(), subj.UserID, includeBroadcasts); err != nil {
		writeError(w, http.StatusInternalServerError, "mark all: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
