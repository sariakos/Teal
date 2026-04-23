// Package domain holds the canonical Go types for the nouns Teal manages:
// App, Deployment, User, EnvVar, AuditLog. These are pure data definitions —
// no database tags, no JSON tags (see note below), no behaviour.
//
// What it does:
//   - Defines the shape of the core entities exactly once, so every other
//     package agrees on what an App or a Deployment is.
//   - Defines the small enums (DeploymentStatus, Color, UserRole) used across
//     the codebase, again exactly once.
//
// What it does NOT do:
//   - Persist anything. That's internal/store.
//   - Serialise anything. The api package owns its own request/response types
//     and translates to/from these. We accept the duplication because the wire
//     format and the storage format will diverge over time, and coupling them
//     here would force one to follow the other.
//   - Validate. Validation lives where the value enters the system (API
//     handlers for HTTP input, repositories for storage invariants).
//
// Why no JSON tags here: the spec demands consistent naming across DB, Go,
// API, and UI, but it does NOT demand the same Go struct be serialised
// directly to the wire. Wire types live next to handlers so a future API
// version doesn't force a domain-type change.
package domain
