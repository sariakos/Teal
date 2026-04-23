# Teal

Self-hosted CD platform for docker-compose apps on a single Linux host, with **zero-downtime blue-green deployments** via Traefik.

The name "Teal" sits between blue and green — the platform always keeps one of those two stacks live and flips traffic between them atomically, so deploys never drop a request.

## Status

Phase 1 — foundation. The platform is not usable yet; this is scaffolding only.

See [`docs/prompts/01_project.md`](docs/prompts/01_project.md) for the full product spec and [`ARCHITECTURE.md`](ARCHITECTURE.md) for the system design.

## Layout

- `backend/` — Go service (HTTP API, deployment engine, Docker integration)
- `frontend/` — SvelteKit UI (lands in Phase 2)
- `deploy/` — compose files and installer for self-hosting (lands in Phase 7)
- `docs/` — spec, ADRs

## Local development

```sh
cd backend
make build      # compile the binary
make test       # run unit tests
make run        # run with dev defaults
```

The dev binary listens on `:3000` by default and writes its SQLite database to `./var/teal.db` (gitignored).
