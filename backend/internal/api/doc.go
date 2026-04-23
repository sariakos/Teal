// Package api owns the HTTP surface of Teal. It is the only package allowed
// to import net/http or the chi router; every other internal package
// receives plain Go values.
//
// What it does:
//   - Constructs the chi router with the standard middleware stack
//     (recover, request-id, logging, JSON content-type, auth).
//   - Defines per-resource handler files (apps.go, deployments.go, …) that
//     translate between HTTP and the store/domain layer.
//   - Provides JSON helpers (writeJSON, writeError) so all handlers respond
//     in the same shape.
//
// What it does NOT do:
//   - Implement business logic. Handlers are thin: parse → call repository
//     or engine → render. Logic lives in the domain layer or in dedicated
//     service packages introduced when a handler becomes non-trivial.
//   - Render HTML. The frontend is served as static assets in Phase 2; this
//     package only serves JSON.
package api
