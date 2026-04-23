# Teal

> Zero-downtime docker-compose deploys, self-hosted in a single binary.

Teal sits between **blue** and **green** — every deploy runs as a parallel stack and only takes traffic after a health check passes. Traefik's routing config flips atomically, so requests in flight aren't dropped. One Go binary, embedded SvelteKit UI, SQLite for state. No Redis, no external DB, no Kubernetes.

---

## Install

On a brand-new Linux server (Ubuntu/Debian/etc.):

```sh
curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash
```

That's it. The script:

1. Installs Docker (with your confirmation) if it isn't already present
2. Generates a `PLATFORM_SECRET`
3. Asks two questions — admin email and base domain
4. Writes `/etc/teal/.env` + `/etc/teal/docker-compose.yml`
5. Pulls the Teal image and starts the stack

Open `http://<your-base-domain>` and create the admin account. To enable HTTPS, point your domain's A record at the host, then in the UI go to **Settings → Platform** and set the ACME email — Traefik takes it from there.

**Non-interactive install** (defaults to `teal.localhost` and `admin@teal.localhost`):

```sh
curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash -s -- --yes
```

**Other flags:**

```
--base-domain=DOMAIN     defaults to teal.localhost
--admin-email=EMAIL      defaults to admin@<base-domain>
--data-dir=PATH          defaults to /var/lib/teal
--version=TAG            defaults to "latest"
```

**Update later:**

```sh
docker compose -f /etc/teal/docker-compose.yml pull && \
docker compose -f /etc/teal/docker-compose.yml up -d
```

**Uninstall:**

```sh
curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/uninstall.sh | sudo bash
```

(Keeps your data by default; pass `--purge` to also delete `/var/lib/teal`.)

---

## What you get

- **Blue-green deployments** with atomic Traefik flip and a configurable drain window
- **Git-source apps** — point at a GitHub repo with SSH deploy key or PAT; auto-deploy on webhook push to a configured branch
- **Encrypted env vars**, per-app + opt-in shared secrets
- **Per-app domains** + automatic Let's Encrypt certificates
- **Real-time deploy progress + container logs** over WebSocket
- **Per-container metrics** (CPU, memory, network, disk) with sparklines on the dashboard
- **Notifications** — in-app feed, signed outbound webhooks, optional SMTP failure emails
- **Per-app CPU + memory limits** injected into the compose at deploy time
- **Audit log** of every state-changing action; admin/member/viewer roles + API keys
- **Single binary**, ~19 MB, with the SvelteKit UI embedded

---

## Layout

- `backend/` — Go service (HTTP + WebSocket API, deployment engine, Docker integration)
- `frontend/` — SvelteKit 2 + Svelte 5 SPA, built statically and embedded
- `deploy/` — compose files for the platform itself + local dev
- `docs/` — product spec, ADRs
- `install.sh` / `uninstall.sh` — the bash one-liners above

[`ARCHITECTURE.md`](ARCHITECTURE.md) is the canonical map of the codebase. [`docs/prompts/01_project.md`](docs/prompts/01_project.md) is the original product spec.

---

## Local development

Run the platform's own dev stack (Traefik on `:80` + dashboard on `:8080`):

```sh
make -C backend dev-up
```

Then build and run Teal:

```sh
cd backend
make build      # dev binary, no embedded UI
make build-release  # binary with the embedded SvelteKit UI (needs Node)
make test
make run        # run with dev defaults
```

The dev binary writes its SQLite database to `./backend/var/teal.db` (gitignored).

---

## License

MIT.
