package api

import "net/http"

// Health is a liveness probe. It reports only that the process is up and
// responding to HTTP — it intentionally does NOT touch the database or
// Docker. A separate readiness endpoint will be added when orchestrators
// need it.
//
// Unauthenticated by design: liveness probes from external monitors should
// not require credentials.
func Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
