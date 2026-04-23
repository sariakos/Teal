# Platform Build Prompt — Teal: Docker-Compose CD Platform with Zero-Downtime Deployments

## Product Name

The platform is called **Teal**. This name is intentional and meaningful: in blue-green deployments, the live stack is always the green one — teal sits between blue and green, representing the seamless, always-live nature of the platform. Use this name consistently everywhere: in the codebase, CLI tool name (`teal`), repository name, documentation, UI, installer script, and any generated config files.

---

## Role & Goal

You are a senior full-stack engineer and DevOps architect. Your task is to help me design and build a self-hosted deployment platform called **Teal** from scratch — similar in spirit to Coolify or Dokploy, but purpose-built for **docker-compose applications** on a **single Linux host**, with a primary focus on **zero-downtime deployments** that existing platforms fail to deliver reliably.

We will build this incrementally, phase by phase. For each phase, produce complete, production-quality code — not pseudocode or stubs. When you make architectural decisions, explain your reasoning briefly so I can push back if needed.

---

## Core Problem Being Solved

Coolify and similar platforms run `docker-compose up -d` to deploy, which causes a brief downtime window. This platform will instead use a **blue-green deployment strategy** with **Traefik as a dynamic reverse proxy**, so that:

1. A new "green" stack is spun up alongside the running "blue" stack
2. Health checks are run against the green stack
3. Traefik routing is flipped from blue to green atomically
4. The blue stack is torn down after a configurable grace period

This should work transparently without the user needing to know about blue-green — they just push to GitHub and their app updates with no downtime.

---

## Tech Stack Decisions

Make opinionated choices and stick with them. Suggested defaults — push back if you have strong reasons:

- **Backend**: Go (preferred for single-binary distribution, low memory footprint, excellent Docker SDK support) or Node.js/TypeScript if Go feels too slow to iterate with
- **Frontend**: SvelteKit (preferred — excellent for server-side rendering of initial state, lightweight runtime, great developer experience) or React with Vite as fallback; avoid heavy frameworks like Next.js or Remix for this use case
- **UI component library**: shadcn/ui (if React) or a Svelte equivalent like shadcn-svelte — provides a solid, accessible base without locking into a rigid design system
- **Styling**: Tailwind CSS
- **Real-time**: WebSocket for live log streaming, deployment status updates, and metrics; use a shared connection manager so multiple UI components can subscribe without opening multiple sockets
- **Database**: SQLite via `github.com/mattn/go-sqlite3` (or `better-sqlite3` for Node) — no external DB dependency for the platform itself; migrations via a simple embedded migration runner
- **Reverse proxy**: Traefik v2/v3 with its Docker provider and file-based dynamic config
- **Container runtime**: Docker Engine via the Docker SDK (not shell-exec to `docker` CLI where avoidable)
- **Auth**: Session-based auth with bcrypt passwords; JWT for API tokens; no external auth service dependency
- **Installer**: A single `curl | bash` shell script that bootstraps the entire platform

---

## Feature Specification

### 1. Application Management

Each "App" in the platform represents one docker-compose project. An App has:

- A unique name/slug (used as the Docker Compose project name)
- A linked Git source (see section 3)
- An environment (set of env vars, see section 4)
- A domain/URL configuration (see section 7)
- A deployment history
- A current status: `idle | deploying | running | failed | stopped`

CRUD for apps via both UI and REST API.

### 2. Zero-Downtime Blue-Green Deployment Engine

This is the heart of the platform. Implement it as follows:

**Compose file transformation pipeline** — before any deploy, the platform must:
1. Parse the user's `docker-compose.yml`
2. Inject the platform's shared Traefik network into all services that require external routing (determined by whether they have a port mapping or a domain set)
3. Inject Traefik labels onto the appropriate service for routing, using the domain configured in the UI — users should NOT need to write Traefik labels or network config manually
4. Assign a deployment ID (e.g. a short UUID or incrementing integer) used as the Compose project suffix to distinguish blue from green (e.g. `myapp-blue`, `myapp-green`)
5. Write the transformed compose to a working directory per deployment

**Deploy sequence:**
1. Lock the app for deployment (prevent concurrent deploys — queue or reject, configurable)
2. Determine current active color (blue or green); new deploy gets the other
3. Pull latest image / build if needed
4. Run `docker compose -p <app>-<color> up -d` with the transformed compose
5. Wait for health checks to pass (configurable timeout; use Docker health checks if defined, or HTTP probe against the service's exposed port)
6. Update Traefik dynamic config to route to the new stack
7. Wait for a configurable drain period (default: 10s)
8. Run `docker compose -p <app>-<oldcolor> down` to tear down the old stack
9. Record the deployment result, duration, and any output to the DB

**Rollback**: Keep the previous deployment's compose config stored so a one-click rollback can re-run the previous green stack as the new deployment target.

**Failure handling**: If health checks fail, tear down the new stack without touching the running one. Alert the user. Store the failed deployment logs.

### 3. Git Source Integration

- Support **GitHub** via GitHub App (preferred — no OAuth token expiry, works for private repos, supports webhooks natively) and optionally a simple personal access token for quick setup
- Store the repo URL, deploy key or GitHub App installation token per app
- Receive **GitHub webhooks** on push events; validate HMAC signature; trigger deployment only if the push branch matches the app's configured auto-deploy branch
- **Per-app branch configuration**: each app has an explicit `auto_deploy_branch` setting (e.g. `main`, `production`, `staging`). Pushes to any other branch are received but ignored for that app. This must be configurable from the UI and API at any time without redeploying
- **Auto-deploy toggle**: auto-deployment from branch pushes can be enabled or disabled per app independently of the branch setting — so you can temporarily pause auto-deploys without losing the branch configuration
- Support **manual deploy** trigger from the UI (pull latest and deploy now), always available regardless of the auto-deploy toggle
- Store the last deployed commit SHA and branch per app; show it in the UI
- Support private repositories via SSH deploy keys (generate a key pair per app, show the public key for the user to add to GitHub)

### 4. Environment Variable Management

- Per-app key-value store for environment variables
- Values encrypted at rest (AES-256-GCM with a master key derived from a startup secret)
- UI: a clean env editor with add/edit/delete, masked values by default with a "reveal" toggle
- Injected into the compose deployment as an env file at deploy time (never written to the compose file itself)
- Support for "shared env vars" that can be referenced across multiple apps (useful for shared secrets like DB passwords)
- Version history: record which env var set was used for each deployment (store a hash, not the values themselves)

### 5. User Management & Security

- Multi-user support with role-based access:
  - `admin`: full access including user management and platform settings
  - `member`: can manage and deploy apps but cannot manage users or platform settings
  - `viewer`: read-only access to app status, logs, and metrics
- Local accounts with bcrypt-hashed passwords (cost factor ≥ 12)
- Session management: secure HTTP-only cookies with CSRF protection; configurable session TTL
- API key support per user for programmatic access (CI/CD webhooks, scripts)
- Optional TOTP-based two-factor authentication (store TOTP secret encrypted; show QR code on setup)
- Login rate limiting (max N attempts per IP per window)
- Audit log: record all significant actions (deploys, config changes, user changes) with timestamp, user, and IP

### 6. Container Logs

- Real-time log streaming per container via WebSocket (stream `docker logs --follow`)
- Log viewer in the UI with:
  - Container selector (if the app has multiple services)
  - Line limit / scroll buffer
  - Timestamp display toggle
  - Search/filter within the buffer
- Deployment logs (the output of the deploy sequence itself) stored per deployment and accessible from deployment history
- Log retention policy: configurable max lines stored per app per deployment

### 7. Domain & URL Management

- Per-app domain configuration: set one or more domains/subdomains
- Platform auto-generates Traefik routing rules (Host matchers, TLS config)
- Automatic TLS via Let's Encrypt through Traefik's ACME resolver — the user just sets a domain and HTTPS is provisioned automatically
- Support for wildcard subdomains if a wildcard cert is configured
- For local/testing use: support a `*.localhost` or custom base domain with self-signed certs

### 8. Autonomous Network Management

- The platform owns a dedicated Docker bridge network (e.g., `platform_proxy`) that Traefik is attached to
- At deploy time, the compose transformation pipeline (see section 2) automatically:
  - Adds `platform_proxy` as an external network in the compose file
  - Attaches the correct service(s) to it based on the app's domain config
  - Removes any conflicting or unnecessary network declarations the user may have written
- Services that don't need external routing (e.g., internal databases) are intentionally NOT attached to the proxy network
- The user's compose file stays clean — no Traefik labels, no network declarations required

### 9. Volume Management

- Per-app view of all Docker named volumes belonging to that app (identified by Compose project label)
- Actions: list, inspect (size, mount path), delete (with confirmation warning)
- Warning shown in the UI if a deploy would recreate or remove a volume
- Platform's own data (database, configs) stored in a named volume so it survives container restarts

### 10. Metrics

- Per-container live metrics scraped from `docker stats` API on a configurable polling interval (default: 15s)
- Stored as a lightweight time-series in SQLite (keep last N hours of data, configurable)
- Display in the UI: CPU %, memory usage/limit, network I/O, block I/O
- Simple sparkline charts; no Prometheus/Grafana dependency in v1
- Platform-level metrics: total apps, running containers, disk usage of Docker volumes

### 11. Easy Linux Installation

- Single installer script: `curl -fsSL https://<platform-domain>/install.sh | bash`
- Installer checks for and optionally installs Docker Engine if missing
- Generates a random `PLATFORM_SECRET` for encryption and session signing on first run
- Pulls the platform's own `docker-compose.yml` and starts it
- Prompts for: initial admin email + password, base domain (optional), port (default 3000)
- Creates a systemd service or Docker restart policy so the platform survives reboots
- Produces an uninstall script that cleanly removes all platform containers, volumes, and config

### 12. Notifications & Webhooks (v1 scope: basic)

- Per-app configurable webhook URL: POST a JSON payload on deploy success or failure
- Email notification on deploy failure (SMTP config in platform settings; optional)
- In-app notification bell showing recent deploy events

### 13. Web UI

Teal must have a polished, fully functional web interface as its primary interaction surface. The UI should feel modern and purposeful — take visual and UX inspiration from Coolify and Dokploy, but aim for a cleaner, less cluttered layout. The teal colour palette should be used deliberately as the primary brand accent.

**Overall structure:**

A persistent left sidebar for navigation, a top bar showing the current context (project name, status, logged-in user, notification bell), and a main content area. The layout should be responsive enough to use on a laptop — full mobile responsiveness is a nice-to-have, not a v1 requirement.

**Pages and views to implement:**

- **Dashboard** — overview of all apps with their current status (running / deploying / failed / stopped), last deployed commit, last deploy time, and a quick-action deploy button per app. A platform health summary at the top (total apps, running containers, disk usage)

- **App detail page** — the main view for a single app, with tabs for:
  - *Overview*: status, live metrics sparklines (CPU, RAM), current deployment info, quick actions (deploy, stop, restart, rollback)
  - *Deployments*: full deployment history with status, commit SHA, triggered by, duration, and a link to full logs for each deployment
  - *Logs*: real-time log viewer with container selector, timestamp toggle, and search
  - *Environment*: env var editor with masked values and reveal toggle
  - *Settings*: git source config, branch and auto-deploy settings, domain/URL config, resource limits, volume overview, danger zone (delete app)

- **New app wizard** — a step-by-step flow: name the app → connect git source → configure branch → paste or upload compose file → set domain → set env vars → review and deploy. Each step validates before allowing progression

- **Volumes page** — platform-wide list of all named Docker volumes, grouped by app, with size, last used, and delete action

- **Settings page** — platform-wide config: SMTP settings, GitHub App config, base domain, TLS settings, platform update button

- **User management page** (admin only) — list users, invite new users, change roles, revoke access, view API keys

- **Audit log page** (admin only) — paginated, searchable table of all recorded actions

**UX details that matter:**

- Deployment status must update in real time without a page refresh — use WebSocket events to push status changes to the UI
- A deploy triggered from the UI should immediately show a live log stream in a slide-over or modal panel without navigating away from the current page
- Destructive actions (delete app, delete volume, rollback) always require a confirmation step — use a modal with the resource name typed as confirmation for irreversible deletes
- Empty states should be helpful: a new installation with no apps should show a clear call-to-action to create the first app, not a blank screen
- Error states should be human-readable: if a deploy fails, the error shown in the UI should explain what went wrong in plain language, not just a stack trace
- The active deployment indicator (spinner, progress steps) should show which phase the deploy is in: pulling → building → starting → health check → switching traffic → cleanup

---

## Additional Requirements (Important)

### API-First Design

Every action available in the UI must also be available via a documented REST API. Design the API before the UI where possible. Use consistent conventions: RESTful resource paths, JSON bodies, standard HTTP status codes. Provide an OpenAPI spec (can be auto-generated).

### Deployment Locking & Queuing

An app can only have one active deployment at a time. If a deploy is triggered while one is in progress:
- Default behavior: reject the new trigger with a `409 Conflict` response
- Optional (configurable per app): queue the new deploy to run immediately after the current one finishes

### Compose File Validation

Before accepting a compose file (on upload or on deploy), validate it:
- Must be parseable as valid YAML
- Must pass `docker compose config` validation
- Warn (don't block) on patterns that are incompatible with blue-green (e.g., `container_name` hardcoded, `host` network mode)

### Build Support

Support both pre-built images and apps with a `build:` directive in their compose file. For build-based apps, run `docker compose build` as the first step of the deploy sequence and stream build logs to the deployment log.

### Resource Limits

Allow optional CPU and memory limits to be set per app in the UI. Inject these as `deploy.resources.limits` into the compose file transformation pipeline.

### Graceful Platform Updates

The platform itself should be updatable: a one-click "Update Platform" action that pulls the latest platform image and restarts it (using the same blue-green principle where possible, or at minimum a fast restart with minimal downtime).

---

## Implementation Phases

Work through these phases in order. Do not skip ahead. Deliver complete, tested code for each phase before moving to the next.

**Phase 1 — Foundation**
- Project scaffolding, directory structure, build system
- Database setup with migration runner
- Core data models: App, Deployment, User, EnvVar, AuditLog
- Basic REST API skeleton with auth middleware
- Docker SDK integration: can list containers, networks, volumes

**Phase 2 — Auth & User Management**
- Full auth flow: register, login, logout, session management
- Role-based middleware
- API key generation and validation
- Basic admin UI for user management
- Establish the UI shell: sidebar navigation, top bar, layout components, colour theme, and empty states — all subsequent phases add pages into this shell

**Phase 3 — Core Deployment Engine**
- Compose file parser and transformation pipeline
- Blue-green deployment sequence (happy path)
- Traefik integration: managed network, dynamic config file updates
- Deployment locking
- Health check runner

**Phase 4 — Git & GitHub Integration**
- App git source configuration
- GitHub webhook receiver with HMAC validation
- SSH deploy key generation per app
- Manual deploy trigger

**Phase 5 — Env Vars, Domains, Volumes**
- Encrypted env var store
- Domain configuration and Traefik rule generation
- Automatic TLS via Traefik ACME
- Volume listing and management UI

**Phase 6 — Logs & Metrics**
- Real-time log streaming via WebSocket
- Deployment log storage and retrieval
- Docker stats scraping and time-series storage
- Metrics UI components

**Phase 7 — Polish & Installer**
- Failure handling, rollback, error states
- Notifications and webhooks
- Installer shell script
- Compose file validation and warnings
- Resource limits injection
- Platform self-update mechanism

---

## Constraints & Principles

- **No downtime as a first-class concern**: every architectural decision should be evaluated against whether it preserves the zero-downtime guarantee
- **Single binary / minimal dependencies**: the platform itself should be deployable as a single Docker Compose stack with no external services required (no Redis, no external Postgres, no message broker)
- **The user's compose files stay clean**: never require users to add platform-specific annotations to their compose files
- **Fail safe**: if any part of a deployment fails, the running production stack must be left untouched
- **Audit everything**: every state change should be traceable to a user, timestamp, and cause
- **Test as you go**: write unit tests for the deployment engine logic and integration tests for the API; don't defer testing to the end

---

## AI-First Development Standards

The primary developer of this codebase is an AI assistant (Claude Opus). This is not a shortcut — it is a deliberate architectural constraint that shapes how the code must be written. As the codebase grows, new AI sessions will have no memory of previous ones. The code itself must therefore carry all necessary context. Apply the following standards rigorously and without exception:

### Code must be self-documenting at every layer

- Every package/module begins with a comment block explaining: what it does, what it does NOT do, and how it fits into the broader system
- Every exported function or type has a docstring that explains its purpose, its inputs, its outputs, and any important side effects or invariants
- Every non-obvious decision in the code has an inline comment explaining WHY, not just what. Example: `// We drain for 10s before tearing down the old stack to allow in-flight requests to complete, not just to wait for the proxy to update`
- Magic numbers and constants are always named and annotated with their rationale

### Strict module boundaries with explicit contracts

- The codebase must be divided into clearly named, single-responsibility modules/packages. No "utils" or "helpers" dumping grounds
- Each module exposes a clean interface and hides its implementation. Modules communicate through defined interfaces, not by reaching into each other's internals
- Dependency direction must be explicit and consistent: higher-level modules depend on lower-level ones, never the reverse. Document the dependency graph in a top-level `ARCHITECTURE.md`

### An always-accurate `ARCHITECTURE.md` at the project root

This file is as important as the code itself. It must be updated in the same commit as any structural change. It must contain:
- A plain-English description of the system and its main components
- A diagram (ASCII or Mermaid) of how modules relate and how data flows through the system
- The reasoning behind key architectural decisions (why blue-green, why SQLite, why Traefik)
- A glossary of domain terms used consistently throughout the codebase (e.g. what exactly is an "App", a "Deployment", a "Stack", a "Color")
- A map of where to find things: "to add a new deployment step, go here; to add a new API endpoint, go here"

### Consistent, predictable naming throughout

- Domain concepts are named the same everywhere: in the database schema, in Go structs/TypeScript types, in API JSON fields, in UI labels, and in comments. Never use synonyms for the same concept
- File and directory names directly reflect their contents. A file named `deploy.go` contains the deployment engine. A file named `traefik.go` contains Traefik integration. No ambiguity
- Error messages are descriptive enough to locate the source without a stack trace

### Layered, incremental complexity

- The simplest working version of each feature is built first, then extended. Never start with a generalised abstraction
- Abstractions are only introduced when the same logic appears in three or more places — not speculatively
- When a new abstraction is introduced, a comment must explain what specific repetition it was introduced to eliminate

### Tests as specification

- Unit tests describe the intended behaviour of each component in plain language (test names read as sentences)
- Integration tests cover the full deployment sequence end-to-end with a real Docker environment where possible
- A failing test must produce an error message that tells a future AI session exactly what contract was violated and where to look

### Change safety

- No file should be so large that it cannot be fully held in a single AI context window (rough guideline: keep files under 400 lines; split if approaching this)
- Refactoring is allowed and encouraged, but every refactor commit must include a short note explaining what was restructured and why, so a future session can understand the git history without reading every diff

---

## How to Work With Me

- Start each phase by proposing the file/module structure and asking for my sign-off before writing code
- When you hit a meaningful decision point (e.g., "should health check timeout be per-service or per-app?"), ask rather than assume
- After each phase, summarize what was built and what the next phase will cover
- If you see a simpler or better approach than what's specified here, say so — this spec is a starting point, not a contract

Let's begin with **Phase 1**. Propose the project structure.