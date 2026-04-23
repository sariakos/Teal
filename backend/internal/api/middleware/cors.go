package middleware

import "net/http"

// DevCORS returns middleware that allows the SvelteKit dev server (default
// http://localhost:5173) to call the API with credentials. Wired into the
// router only when TEAL_ENV=dev — in prod the frontend is served from the
// same origin as the API and CORS is not relevant.
//
// Allowed origins are taken from the caller so tests can plug in their own.
// Empty list disables CORS entirely (the middleware becomes a no-op).
func DevCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || !allowed[origin] {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Content-Type, X-Csrf-Token, Authorization")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
