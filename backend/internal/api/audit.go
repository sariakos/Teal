package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// auditResponse is the wire shape of a single AuditLog row.
type auditResponse struct {
	ID          int64     `json:"id"`
	ActorUserID *int64    `json:"actorUserId,omitempty"`
	Actor       string    `json:"actor"`
	Action      string    `json:"action"`
	TargetType  string    `json:"targetType,omitempty"`
	TargetID    string    `json:"targetId,omitempty"`
	IP          string    `json:"ip,omitempty"`
	Details     string    `json:"details,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func auditToResponse(l domain.AuditLog) auditResponse {
	return auditResponse{
		ID: l.ID, ActorUserID: l.ActorUserID, Actor: l.Actor,
		Action: string(l.Action), TargetType: l.TargetType, TargetID: l.TargetID,
		IP: l.IP, Details: l.Details, CreatedAt: l.CreatedAt,
	}
}

type auditHandler struct {
	logs *store.AuditLogRepo
}

// list returns recent audit logs newest first. Admin only — wired in router.
func (h *auditHandler) list(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be 1..500")
			return
		}
		limit = n
	}
	rows, err := h.logs.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	out := make([]auditResponse, 0, len(rows))
	for _, l := range rows {
		out = append(out, auditToResponse(l))
	}
	writeJSON(w, http.StatusOK, out)
}

// recordAudit is the helper every state-changing handler calls. Failures are
// logged but never returned — losing an audit row must not roll back a
// successful action (the underlying mutation already happened).
//
// actorOverride is non-empty when the actor cannot be inferred from
// Subject — e.g. a failed login (no Subject) or a webhook (Phase 4).
func recordAudit(ctx context.Context, logger *slog.Logger, repo *store.AuditLogRepo,
	action domain.AuditAction, targetType, targetID, ip, details, actorOverride string,
) {
	subj := auth.FromContext(ctx)
	entry := domain.AuditLog{
		Action: action, TargetType: targetType, TargetID: targetID,
		IP: ip, Details: details,
	}
	if actorOverride != "" {
		entry.Actor = actorOverride
	} else if !subj.IsZero() {
		entry.ActorUserID = &subj.UserID
		entry.Actor = subj.Email
	} else {
		entry.Actor = "anonymous"
	}
	if _, err := repo.Append(ctx, entry); err != nil {
		logger.Error("audit append failed", "action", action, "err", err)
	}
}
