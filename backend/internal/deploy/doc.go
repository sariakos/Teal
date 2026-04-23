// Package deploy is Teal's blue-green deployment engine. It owns the state
// machine that drives an App from "current color is X" to "current color is
// Y", coordinating compose transformation, docker compose invocation,
// health checks, and Traefik dynamic-config flips.
//
// File layout:
//   - workdir.go     working-directory layout + retention pruning
//   - lock.go        per-app deploy lock (DB-backed)
//   - runner.go      shell-out to `docker compose` with cancellable context
//   - healthcheck.go three readiness strategies (docker, HTTP, fallback)
//   - engine.go      the orchestrator: state machine + Phase event broadcast
//
// What this package does NOT do:
//   - Mutate user YAML. internal/compose owns that.
//   - Touch Traefik directly. internal/traefik does — this package calls
//     into it at the flip step.
//   - Implement scheduling. The engine accepts deploy requests and either
//     starts immediately or returns ErrLocked. Phase 7 may add queueing.
package deploy
