package api

import (
	"encoding/json"
	"net/http"
)

// errorBody is the shape of every error response. Keeping a single shape
// means clients (the SvelteKit UI, scripts, the future CLI) can write one
// error path that handles all endpoints.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON serialises v as JSON with the given status code. Encoding errors
// after the header has been written are logged-and-swallowed: there is
// nothing useful to send to the client at that point.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeError sends an errorBody at the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}
