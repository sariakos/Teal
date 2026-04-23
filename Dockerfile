# syntax=docker/dockerfile:1.7
#
# Teal — single-binary self-hosted CD platform.
#
# Multi-stage build:
#   1. node-builder    builds the SvelteKit static SPA into frontend/build/
#   2. go-builder      copies that build into the embed staging dir, then
#                      `go build -tags embed_frontend` produces a single
#                      static binary
#   3. runtime         minimal image carrying the binary + the docker
#                      compose plugin Teal shells out to
#
# Cross-compilation: `--platform` and the BuildKit-provided TARGETOS /
# TARGETARCH env vars let the same Dockerfile produce linux/amd64 and
# linux/arm64 from any host. CI builds both via `docker buildx`.

# ----- Stage 1: build the SvelteKit static SPA -----
FROM --platform=$BUILDPLATFORM node:22-alpine AS node-builder
WORKDIR /src/frontend

# Lockfile + manifest first for layer caching.
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --no-audit --no-fund

COPY frontend/ ./
RUN npm run build

# ----- Stage 2: build the Go binary with the embedded UI -----
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS go-builder
WORKDIR /src/backend

# Modules first so dep changes don't bust the source-cache layer.
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

# Drop the SvelteKit build into the embed staging dir so the
# `embed_frontend` tag picks it up. Mirrors what `make build-release`
# does on a dev machine.
COPY --from=node-builder /src/frontend/build/ ./internal/api/frontend_build/

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
RUN go build \
    -tags embed_frontend \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/teal ./cmd/teal

# ----- Stage 3: minimal runtime -----
# Teal shells out to `docker compose` for user apps; the runtime needs the
# Docker CLI + the compose plugin. docker:cli is a slim official image
# that ships both. /var/run/docker.sock is bind-mounted by the host
# compose so this container can drive the host's daemon.
FROM docker:27-cli

# tzdata is useful for timestamped log lines + ACME renewal scheduling.
# ca-certificates ships in the docker:cli base.
RUN apk add --no-cache tzdata curl

# Non-root would be ideal but Teal needs read+write on the docker socket
# (whose group differs by host). Stay root for v1; revisit when the
# installer lands a `teal` group + sudoers entry.

WORKDIR /app
COPY --from=go-builder /out/teal /usr/local/bin/teal

# The platform compose mounts /var/lib/teal here.
VOLUME ["/var/lib/teal"]
EXPOSE 3000

# Healthcheck mirrors the in-app /healthz endpoint so docker compose can
# show a healthy/unhealthy state for the platform container itself.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
    CMD curl -fsS http://127.0.0.1:3000/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/teal"]
