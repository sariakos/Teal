// Package compose owns Teal's docker-compose YAML manipulation. It is the
// only place that reads or writes user-supplied compose files.
//
// What it does:
//   - Parse + validate user YAML (`docker compose -f - config` shell-out
//     for the authoritative check).
//   - Lint for blue-green-incompatible patterns (container_name, host
//     network, host pid). Reported as warnings, not blocking errors.
//   - Transform: a pure function that takes the user YAML plus context
//     (slug, color, domains, env file) and returns a new YAML string with
//     platform_proxy attached, Teal-managed labels injected, and the
//     primary service identified for routing.
//   - Render: write the transformed YAML to the deployment's working dir.
//
// What it does NOT do:
//   - Run docker compose. That's internal/deploy/runner.go.
//   - Decide whether to deploy. The engine in internal/deploy owns
//     scheduling and locking.
//   - Manage Traefik dynamic config. internal/traefik does, off the back of
//     the primary-service identity returned by Transform.
//
// Why a separate package: keeping every YAML mutation here means upgrading
// the compose-spec or swapping engines (e.g. to podman-compose) is a
// one-package change.
package compose
