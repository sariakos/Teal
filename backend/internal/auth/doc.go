// Package auth owns Teal's authentication and access-control primitives.
// Phase 2 ships the full surface:
//
//   - Subject + context (subject.go) — what handlers receive as "the
//     authenticated principal".
//   - Password hashing (password.go) — bcrypt at cost ≥ 12.
//   - Session management (session.go) — server-side sessions stored in
//     SQLite, opaque ID in an HttpOnly cookie, sliding expiry.
//   - API keys (apikey.go) — generate / validate; only the SHA-256 hash is
//     persisted, the raw key is shown once at creation.
//   - CSRF (csrf.go) — synchronizer-token-bound-to-session pattern using a
//     non-HttpOnly cookie that the frontend echoes via X-Csrf-Token.
//   - Rate limiting (ratelimit.go) — in-memory token-bucket per IP for
//     login attempts.
//   - Bootstrap detection (bootstrap.go) — "are there any users yet?"
//   - Middleware (middleware.go) — wires it all together; replaces the
//     Phase 1 stub.
//
// What this package does NOT do:
//   - Persist anything on its own. Sessions and API keys live in
//     internal/store; this package owns the policy (when to issue, when to
//     expire) and the cookie/header plumbing.
//   - Hash anything other than passwords (SHA-256 of API keys lives here as
//     it's a crypto-trivial op, but symmetric encryption is in
//     internal/crypto).
package auth
