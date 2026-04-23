package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// Authenticator is the request-scoped principal resolver used by the API
// router. It owns the policy of "how do we figure out who is calling?":
//
//  1. If DevBypass is enabled, attach a synthetic admin Subject and
//     short-circuit. Only allowed in dev (config validation rejects it
//     elsewhere), used for backend-only iteration.
//  2. Try the session cookie. On hit, attach Subject AND Session (the
//     latter so CSRFMiddleware can find the token).
//  3. Try the Authorization: Bearer header. On hit, attach Subject only
//     (no Session — bearer requests are not subject to CSRF).
//  4. None of the above → 401.
//
// The Authenticator is stateless after construction; safe to share.
type Authenticator struct {
	Sessions  *SessionManager
	APIKeys   *APIKeyManager
	Users     *store.UserRepo
	DevBypass bool
}

// Middleware returns the http.Handler middleware that performs the lookups
// described above. It does NOT enforce CSRF — CSRFMiddleware handles that
// in a separate layer so it can compose differently per route group.
func (a *Authenticator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a.DevBypass {
				ctx := WithSubject(r.Context(), Subject{
					UserID: 0, Email: "dev@local", Role: domain.UserRoleAdmin,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			ctx, ok := a.tryCookie(r.Context(), w, r)
			if ok {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			ctx, ok = a.tryBearer(r.Context(), r)
			if ok {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		})
	}
}

func (a *Authenticator) tryCookie(ctx context.Context, w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	sess, err := a.Sessions.Validate(ctx, r)
	if err != nil {
		// Not a session-authed request; let the caller try other methods.
		// Don't log here — falling through to bearer is the normal path.
		return ctx, false
	}
	user, err := a.Users.Get(ctx, sess.UserID)
	if err != nil {
		// Session points at a user that no longer exists. Best-effort
		// clean up and clear the cookie so the client stops sending it.
		_ = a.Sessions.Sessions.Delete(ctx, sess.ID)
		a.Sessions.clearCookies(w)
		return ctx, false
	}
	// Sliding expiry — Touch is internally throttled.
	_ = a.Sessions.Touch(ctx, sess)

	ctx = WithSubject(ctx, Subject{UserID: user.ID, Email: user.Email, Role: user.Role})
	ctx = WithSession(ctx, sess)
	return ctx, true
}

func (a *Authenticator) tryBearer(ctx context.Context, r *http.Request) (context.Context, bool) {
	raw, ok := ParseBearer(r)
	if !ok {
		return ctx, false
	}
	key, err := a.APIKeys.Validate(ctx, raw)
	if err != nil {
		if errors.Is(err, ErrInvalidAPIKey) {
			return ctx, false
		}
		// DB error; treat as no-auth and fall through to 401.
		return ctx, false
	}
	user, err := a.Users.Get(ctx, key.UserID)
	if err != nil {
		return ctx, false
	}
	ctx = WithSubject(ctx, Subject{UserID: user.ID, Email: user.Email, Role: user.Role})
	return ctx, true
}
