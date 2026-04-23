package auth

import (
	"context"
	"net/http"

	"github.com/sariakos/teal/backend/internal/domain"
)

// Subject is the authenticated principal attached to a request. The fields
// are deliberately a small subset of domain.User — handlers should not need
// to consult the database to know who the caller is.
type Subject struct {
	UserID int64
	Email  string
	Role   domain.UserRole
}

// IsZero reports whether the Subject is unset (the zero value), which is how
// "anonymous" is represented in context.
func (s Subject) IsZero() bool {
	return s.UserID == 0 && s.Email == "" && s.Role == ""
}

type subjectKey struct{}

// WithSubject attaches s to ctx.
func WithSubject(ctx context.Context, s Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, s)
}

// FromContext returns the Subject attached to ctx, or the zero Subject if
// none is set.
func FromContext(ctx context.Context) Subject {
	s, _ := ctx.Value(subjectKey{}).(Subject)
	return s
}

// RequireRole returns middleware that 403s any request whose Subject does
// not have the minimum role. Designed to be composed under the main auth
// Middleware.
func RequireRole(min domain.UserRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := FromContext(r.Context())
			if !roleAtLeast(s.Role, min) {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// roleAtLeast implements the strict ordering admin > member > viewer.
func roleAtLeast(have, min domain.UserRole) bool {
	return roleOrdinal(have) >= roleOrdinal(min)
}

func roleOrdinal(r domain.UserRole) int {
	switch r {
	case domain.UserRoleAdmin:
		return 3
	case domain.UserRoleMember:
		return 2
	case domain.UserRoleViewer:
		return 1
	default:
		return 0
	}
}
