package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// Cookie names. Two cookies are issued at login:
//
//   - SessionCookieName: HttpOnly, holds the opaque session ID.
//   - CSRFCookieName:    NOT HttpOnly, holds the CSRF token for this session
//     (the SPA reads it and echoes it via X-Csrf-Token).
const (
	SessionCookieName = "teal_session"
	CSRFCookieName    = "teal_csrf"
)

// Session policy defaults.
const (
	DefaultSessionTTL          = 7 * 24 * time.Hour
	DefaultSessionSlideMinimum = 1 * time.Minute // only Touch if last_seen older than this
)

// SessionManager owns session lifecycle and cookie plumbing. One instance
// shared across all requests; safe for concurrent use.
type SessionManager struct {
	Sessions      *store.SessionRepo
	TTL           time.Duration
	SlideMinimum  time.Duration
	Secure        bool   // true in prod (TEAL_ENV=prod) so cookies are only sent over HTTPS
	CookieDomain  string // empty => defaults to request host
}

// NewSessionManager constructs a manager with sensible defaults. Pass
// secure=true in production.
func NewSessionManager(sessions *store.SessionRepo, secure bool) *SessionManager {
	return &SessionManager{
		Sessions:     sessions,
		TTL:          DefaultSessionTTL,
		SlideMinimum: DefaultSessionSlideMinimum,
		Secure:       secure,
	}
}

// Issue creates a new server-side session for userID, sets the session and
// CSRF cookies on the response, and returns the persisted Session.
func (m *SessionManager) Issue(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) (domain.Session, error) {
	id, err := newRandomToken()
	if err != nil {
		return domain.Session{}, err
	}
	csrf, err := newRandomToken()
	if err != nil {
		return domain.Session{}, err
	}

	now := time.Now().UTC()
	sess, err := m.Sessions.Create(ctx, domain.Session{
		ID:        id,
		UserID:    userID,
		CSRFToken: csrf,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		ExpiresAt: now.Add(m.TTL),
	})
	if err != nil {
		return domain.Session{}, err
	}

	m.setCookies(w, sess)
	return sess, nil
}

// Validate looks up the session referenced by the cookie on r, returning the
// row if it exists and has not expired. Returns ErrNoSession (a typed nil-ish
// signal) when there is no cookie or the cookie does not match a row.
func (m *SessionManager) Validate(ctx context.Context, r *http.Request) (domain.Session, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return domain.Session{}, ErrNoSession
	}
	sess, err := m.Sessions.Get(ctx, cookie.Value)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.Session{}, ErrNoSession
		}
		return domain.Session{}, err
	}
	if sess.IsExpired(time.Now()) {
		// Best-effort cleanup; ignore error.
		_ = m.Sessions.Delete(ctx, sess.ID)
		return domain.Session{}, ErrNoSession
	}
	return sess, nil
}

// Touch advances last_seen_at and slides expires_at forward, but only if
// the previous LastSeenAt is older than SlideMinimum. This caps DB writes to
// roughly one per minute per active session under heavy traffic.
func (m *SessionManager) Touch(ctx context.Context, sess domain.Session) error {
	now := time.Now().UTC()
	if now.Sub(sess.LastSeenAt) < m.SlideMinimum {
		return nil
	}
	return m.Sessions.Touch(ctx, sess.ID, now, now.Add(m.TTL))
}

// Destroy revokes the current session (if any) and clears both cookies on
// the response. Idempotent.
func (m *SessionManager) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		if err := m.Sessions.Delete(ctx, cookie.Value); err != nil {
			return err
		}
	}
	m.clearCookies(w)
	return nil
}

func (m *SessionManager) setCookies(w http.ResponseWriter, sess domain.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		Domain:   m.CookieDomain,
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   m.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    sess.CSRFToken,
		Path:     "/",
		Domain:   m.CookieDomain,
		Expires:  sess.ExpiresAt,
		HttpOnly: false, // SPA must be able to read this
		Secure:   m.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *SessionManager) clearCookies(w http.ResponseWriter) {
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:    name,
			Value:   "",
			Path:    "/",
			Domain:  m.CookieDomain,
			Expires: time.Unix(0, 0),
			MaxAge:  -1,
		})
	}
}

// ErrNoSession is returned by Validate when the request carries no usable
// session (no cookie, expired cookie, deleted row). It is NOT wrapped in
// errors.Is from store.ErrNotFound so callers can distinguish "auth absent"
// from "DB error".
var ErrNoSession = errors.New("auth: no session")

// session context key
type sessionKey struct{}

// WithSession attaches a Session to ctx. Used by the auth middleware so
// downstream middleware (CSRF) and handlers can inspect it.
func WithSession(ctx context.Context, s domain.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFromContext returns the Session in ctx, or the zero value.
func SessionFromContext(ctx context.Context) domain.Session {
	s, _ := ctx.Value(sessionKey{}).(domain.Session)
	return s
}

// newRandomToken returns a 52-character base32 string (32 random bytes).
// base32 is chosen over base64 so the token is URL- and cookie-safe without
// any escaping.
func newRandomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b[:]), "="), nil
}

// clientIP returns the best-guess client IP for storage on the session row.
// We trust X-Forwarded-For only when present (operators put us behind their
// own reverse proxy in prod) and fall back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First entry is the original client; trim any trailing entries the
		// proxy may have appended.
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}
