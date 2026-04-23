package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/deploy"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// deploymentResponse is the wire shape of a Deployment. The Phase field is
// populated only when the response goes through the per-deployment GET; the
// list endpoints leave it empty (it's an in-memory engine value, not a DB
// column, and looking it up per row would tie the list to the engine).
type deploymentResponse struct {
	ID                int64      `json:"id"`
	AppID             int64      `json:"appId"`
	Color             string     `json:"color"`
	Status            string     `json:"status"`
	Phase             string     `json:"phase,omitempty"`
	CommitSHA         string     `json:"commitSha"`
	TriggeredByUserID *int64     `json:"triggeredByUserId,omitempty"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	FailureReason     string     `json:"failureReason,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

func deploymentToResponse(d domain.Deployment) deploymentResponse {
	return deploymentResponse{
		ID:                d.ID,
		AppID:             d.AppID,
		Color:             string(d.Color),
		Status:            string(d.Status),
		CommitSHA:         d.CommitSHA,
		TriggeredByUserID: d.TriggeredByUserID,
		StartedAt:         d.StartedAt,
		CompletedAt:       d.CompletedAt,
		FailureReason:     d.FailureReason,
		CreatedAt:         d.CreatedAt,
	}
}

type deploymentsHandler struct {
	deployments *store.DeploymentRepo
	engine      *deploy.Engine
}

// list returns recent deployments across all Apps. Optional query params:
//   - limit (int, 1..200, default 50)
func (h *deploymentsHandler) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit must be an integer between 1 and 200")
			return
		}
		limit = n
	}

	rows, err := h.deployments.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deployments")
		return
	}
	out := make([]deploymentResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, deploymentToResponse(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// get returns a single deployment with its current Phase. Phase is the
// in-memory live progress signal; for terminal deployments it will be
// "succeeded"/"failed"/empty (the row's Status is authoritative once the
// engine has cleared its in-memory state).
func (h *deploymentsHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := h.deployments.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "deployment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	resp := deploymentToResponse(d)
	if h.engine != nil {
		if p := h.engine.CurrentPhase(d.ID); p != "" {
			resp.Phase = string(p)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
