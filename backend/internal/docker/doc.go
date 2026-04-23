// Package docker is Teal's only seam to the Docker Engine. Every other
// package receives plain Go structs from this one — they must not import the
// Docker SDK directly.
//
// What it does (Phase 1):
//   - Opens a Docker SDK client against the local daemon (or DOCKER_HOST).
//   - Exposes read-only listings of containers, networks, and volumes that
//     return Teal-shaped structs, not SDK types.
//
// What it does NOT do (yet):
//   - Run docker compose up/down. The compose-driven deployment engine lands
//     in Phase 3 and will live next to this package, not inside it (compose
//     orchestration is a higher-level concern that uses the SDK plus the CLI).
//   - Manage networks/volumes mutatively. Mutation is added per-feature in
//     later phases so that Phase 1 listings can be surfaced safely.
//
// Why a wrapper at all:
//   - The Docker SDK has a sprawling surface and changes shape across
//     versions. Pinning the wire types here means a SDK upgrade is a one-file
//     change, and tests for higher layers can fake this package via its
//     interface.
package docker
