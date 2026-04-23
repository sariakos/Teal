// Package traefik owns the platform's reverse-proxy integration.
//
// What it does:
//   - Manages the platform_proxy bridge network: created idempotently at
//     startup so every App's stack can attach to it for routing.
//   - Writes per-App dynamic-config files (one YAML per App) under
//     <dir>/<slug>.yml. The file declares one router and one service that
//     point at the **active** color's primary container. The deploy engine
//     atomically rewrites the file when flipping colors.
//
// What it does NOT do:
//   - Run Traefik. Traefik is a separate container in the platform's own
//     compose stack (see deploy/docker-compose.dev.yml for local dev).
//   - Provide TLS configuration. ACME / Let's Encrypt wiring lands in
//     Phase 5; for now the file-based provider just exposes HTTP routers.
//   - Generate Docker labels. We use file-based config exclusively (see
//     ARCHITECTURE.md "Key architectural decisions") because labels would
//     make blue+green both register routers and Traefik would round-robin
//     during the overlap, which defeats the atomic flip the spec requires.
//
// Why a separate package: keeping all Traefik knowledge in one place means
// swapping to a different proxy (Caddy, nginx) is a one-package change.
package traefik
