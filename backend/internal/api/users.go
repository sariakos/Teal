package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// userResponse is the wire shape of a User. Critically it omits
// PasswordHash and TOTPSecretEncrypted — those must never leave the server.
type userResponse struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	HasTOTP   bool      `json:"hasTotp"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func userToResponse(u domain.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Email:     u.Email,
		Role:      string(u.Role),
		HasTOTP:   len(u.TOTPSecretEncrypted) > 0,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// usersHandler holds dependencies for the admin User CRUD endpoints. Every
// route under this handler is wrapped with auth.RequireRole(admin) in the
// router.
type usersHandler struct {
	logger *slog.Logger
	store  *store.Store
}

// list returns every User.
func (h *usersHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.Users.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	out := make([]userResponse, 0, len(rows))
	for _, u := range rows {
		out = append(out, userToResponse(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (h *usersHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	role := domain.UserRole(req.Role)
	if role == "" {
		role = domain.UserRoleViewer
	}
	if err := validateRole(role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordTooShort) {
			writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	user, err := h.store.Users.Create(r.Context(), domain.User{
		Email: req.Email, PasswordHash: hash, Role: role,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "email already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserCreate, "user", strconv.FormatInt(user.ID, 10),
		clientIP(r), "created user "+user.Email, "")
	writeJSON(w, http.StatusCreated, userToResponse(user))
}

type updateUserRequest struct {
	Email    *string `json:"email"`
	Password *string `json:"password"`
	Role     *string `json:"role"`
}

func (h *usersHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	user, err := h.store.Users.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if req.Email != nil {
		user.Email = strings.TrimSpace(strings.ToLower(*req.Email))
	}
	if req.Role != nil {
		role := domain.UserRole(*req.Role)
		if err := validateRole(role); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user.Role = role
	}
	if req.Password != nil {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrPasswordTooShort) {
				writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		user.PasswordHash = hash
		// Password change revokes every existing session for this user.
		_ = h.store.Sessions.DeleteForUser(r.Context(), user.ID)
	}

	if err := h.store.Users.Update(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "email already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserUpdate, "user", strconv.FormatInt(user.ID, 10),
		clientIP(r), "updated user "+user.Email, "")
	writeJSON(w, http.StatusOK, userToResponse(user))
}

func (h *usersHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	subj := auth.FromContext(r.Context())
	if subj.UserID == id {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	if err := h.store.Users.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserDelete, "user", strconv.FormatInt(id, 10),
		clientIP(r), "", "")
	w.WriteHeader(http.StatusNoContent)
}

func validateRole(r domain.UserRole) error {
	switch r {
	case domain.UserRoleAdmin, domain.UserRoleMember, domain.UserRoleViewer:
		return nil
	default:
		return errors.New("role must be one of admin, member, viewer")
	}
}
