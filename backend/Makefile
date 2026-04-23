# Teal backend — common dev tasks. Keep targets short and shell-portable;
# real CI/CD lives elsewhere.

BIN_DIR := bin
BIN     := $(BIN_DIR)/teal
PKG     := ./...

FRONTEND_DIR    := ../frontend
FRONTEND_BUILD  := $(FRONTEND_DIR)/build
EMBED_DIR       := internal/api/frontend_build

.PHONY: all build build-release frontend-build run test test-race lint vet fmt tidy clean dev-up dev-down dev-logs dev-traefik-prep

all: build

# Default build: dev-friendly, no UI. The embed_frontend tag is NOT set, so
# `frontend_build/` doesn't need to exist (and the noembed shim is used).
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) ./cmd/teal

# Release build: produces a single binary with the SvelteKit UI embedded.
# Requires Node + npm in PATH for the frontend-build step.
build-release: frontend-build
	@mkdir -p $(BIN_DIR)
	rm -rf $(EMBED_DIR)/_app $(EMBED_DIR)/*.js $(EMBED_DIR)/*.css $(EMBED_DIR)/*.png $(EMBED_DIR)/*.ico $(EMBED_DIR)/*.svg 2>/dev/null || true
	cp -R $(FRONTEND_BUILD)/. $(EMBED_DIR)/
	go build -tags embed_frontend -o $(BIN) ./cmd/teal

# Build the frontend SPA into frontend/build/. Idempotent.
frontend-build:
	cd $(FRONTEND_DIR) && npm install --no-audit --no-fund && npm run build

run:
	go run ./cmd/teal

test:
	go test $(PKG)

test-race:
	go test -race $(PKG)

vet:
	go vet $(PKG)

# `lint` is intentionally `vet` for now — we'll add golangci-lint when the
# codebase is large enough to justify another tool.
lint: vet

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)

# --- Local development: bring up Traefik on :80 and the dashboard on :8080 ---
# The platform_proxy network must exist first; running the binary at least
# once creates it, but we also create it here so first-time setup works.
dev-up: dev-traefik-prep
	docker network inspect platform_proxy >/dev/null 2>&1 || docker network create --label teal.managed=true platform_proxy
	docker compose -f ../deploy/docker-compose.dev.yml up -d

# dev-traefik-prep ensures the host-side files Docker bind-mounts exist as
# regular files, not directories. Without this Docker silently creates an
# empty directory at the bind-mount source on first `up`, which then breaks
# Traefik's static-config load.
dev-traefik-prep:
	mkdir -p var/traefik/dynamic var/traefik/acme
	[ -f var/traefik/traefik.yml ] || printf 'api:\n  dashboard: true\n  insecure: true\nentryPoints:\n  web:\n    address: ":80"\nproviders:\n  file:\n    directory: /etc/traefik/dynamic\n    watch: true\nlog:\n  level: INFO\n' > var/traefik/traefik.yml
	[ -f var/traefik/acme/acme.json ] || (touch var/traefik/acme/acme.json && chmod 600 var/traefik/acme/acme.json)

dev-down:
	docker compose -f ../deploy/docker-compose.dev.yml down

dev-logs:
	docker compose -f ../deploy/docker-compose.dev.yml logs -f
