package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// authHandler bundles the dependencies of the login/logout/me/bootstrap
// endpoints.
type authHandler struct {
	logger      *slog.Logger
	store       *store.Store
	authn       *auth.Authenticator
	rateLimiter *auth.LoginRateLimiter
}

// loginRequest is the JSON body shape for POST /api/v1/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// meResponse describes the currently authenticated principal. CSRF token is
// included so the frontend can read it without re-parsing the cookie (it
// also reads the cookie for POSTs).
type meResponse struct {
	User      userResponse `json:"user"`
	CSRFToken string       `json:"csrfToken,omitempty"`
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !h.rateLimiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.store.Users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Constant-time compare against a known hash to flatten timing.
			_ = auth.ComparePassword(dummyBcryptHash, req.Password)
			recordAudit(r.Context(), h.logger, h.store.AuditLogs,
				domain.AuditActionUserLogin, "user", "", ip, "unknown email: "+req.Email, req.Email)
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := auth.ComparePassword(user.PasswordHash, req.Password); err != nil {
		recordAudit(r.Context(), h.logger, h.store.AuditLogs,
			domain.AuditActionUserLogin, "user", "", ip, "wrong password", user.Email)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	sess, err := h.authn.Sessions.Issue(r.Context(), w, r, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue session")
		return
	}
	uid := user.ID
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserLogin, "user", "", ip, "login success", user.Email)
	_ = uid // kept for future: link audit row directly to user via FK already set in recordAudit

	writeJSON(w, http.StatusOK, meResponse{
		User:      userToResponse(user),
		CSRFToken: sess.CSRFToken,
	})
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.authn.Sessions.Destroy(r.Context(), w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserLogout, "user", "", clientIP(r), "", "")
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	if subj.UserID == 0 {
		// Bearer/dev-bypass paths land here; return a minimal stub since we
		// don't have a real DB user. The frontend uses /me to bootstrap, so
		// only the cookie-authed path needs a full User row.
		writeJSON(w, http.StatusOK, meResponse{
			User: userResponse{Email: subj.Email, Role: string(subj.Role)},
		})
		return
	}
	user, err := h.store.Users.Get(r.Context(), subj.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	resp := meResponse{User: userToResponse(user)}
	if sess := auth.SessionFromContext(r.Context()); sess.ID != "" {
		resp.CSRFToken = sess.CSRFToken
	}
	writeJSON(w, http.StatusOK, resp)
}

// registerBootstrap creates the first admin user. Succeeds only when the
// users table is empty — once any user exists, this endpoint always
// returns 409. The newly created user is logged in immediately.
// setupStatus reports whether a bootstrap admin needs to be created. Public
// (no auth) by design: anyone reaching the install in the no-users state can
// claim the admin account anyway, so the boolean leaks no exploitable info.
func (h *authHandler) setupStatus(w http.ResponseWriter, r *http.Request) {
	none, err := auth.NoUsersYet(r.Context(), h.store.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"noUsersYet": none})
}

func (h *authHandler) registerBootstrap(w http.ResponseWriter, r *http.Request) {
	none, err := auth.NoUsersYet(r.Context(), h.store.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !none {
		writeError(w, http.StatusConflict, "an admin already exists; use /login")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
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
		Email: req.Email, PasswordHash: hash, Role: domain.UserRoleAdmin,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "email already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	sess, err := h.authn.Sessions.Issue(r.Context(), w, r, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue session")
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionUserCreate, "user", "", clientIP(r), "bootstrap admin", user.Email)

	writeJSON(w, http.StatusCreated, meResponse{
		User:      userToResponse(user),
		CSRFToken: sess.CSRFToken,
	})
}

// clientIP mirrors auth.clientIP for use in handlers (kept here since auth's
// version is unexported on purpose — it's a private internal helper of the
// session manager).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}

// dummyBcryptHash is used to flatten timing on unknown-email login attempts.
// It is a valid bcrypt hash of a random string at the same cost as live
// hashes, so the wall-clock cost of comparing it matches a real lookup.
// The value is fixed at build time — never serialised back to a client.
var dummyBcryptHash = []byte("$2a$12$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
