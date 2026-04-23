//go:build !embed_frontend

package api

import "github.com/go-chi/chi/v5"

// registerFrontend is a no-op when the frontend is not embedded into the
// binary. The build tag `embed_frontend` swaps in the real implementation
// (frontend.go) which serves the SvelteKit static build from an embed.FS.
//
// Backend developers can `make build` and `make run` without ever touching
// Node — the API works in isolation, and a missing UI returns the standard
// 404 JSON.
func registerFrontend(_ chi.Router) {}
