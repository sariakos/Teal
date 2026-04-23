package auth

import (
	"crypto/subtle"
	"net/http"
)

// CSRFHeaderName is the request header the SPA echoes the CSRF token through
// on unsafe-method requests. The backend rejects any unsafe-method request
// with a session unless this header matches the session's csrf_token.
const CSRFHeaderName = "X-Csrf-Token"

// safeMethods do not mutate state on the server side and therefore are
// exempt from CSRF checks. This is the conventional set per RFC 9110.
func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// CSRFMiddleware enforces the synchronizer-token rule for cookie-authed
// browser requests:
//
//   - Safe methods (GET/HEAD/OPTIONS) are always allowed.
//   - Requests without a Session in context (typically bearer-authed) are
//     allowed — bearer auth is not vulnerable to CSRF, since browsers won't
//     attach an Authorization header automatically.
//   - Requests with a Session must carry an X-Csrf-Token header whose value
//     matches the session's stored csrf_token.
//
// Composes after the auth Middleware (which is what populates the Session in
// context).
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		sess := SessionFromContext(r.Context())
		if sess.ID == "" {
			// Not a session-authed request (bearer or anonymous). The auth
			// middleware has already decided whether to allow it; CSRF does
			// not apply.
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get(CSRFHeaderName)
		if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(sess.CSRFToken)) != 1 {
			http.Error(w, `{"error":"csrf token missing or invalid"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
