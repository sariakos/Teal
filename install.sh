#!/usr/bin/env bash
#
# Teal installer — bootstraps the platform on a fresh Linux host.
#
# What this script does (in order):
#   1. Confirms it can do its work (root or sudo, supported OS).
#   2. Installs Docker Engine if missing (with your confirmation).
#   3. Generates a 32-byte PLATFORM_SECRET (random, stored 0600).
#   4. Asks two questions: admin email and base domain.
#      Everything else (port, data directory, version) has a sensible
#      default. Pass --yes to skip every prompt.
#   5. Writes /etc/teal/.env and /etc/teal/docker-compose.yml.
#   6. Pulls the Teal image and starts the stack.
#   7. Prints the URL and the bootstrap step.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash -s -- --yes
#
# Flags:
#   --yes                    non-interactive; use defaults for everything
#   --base-domain=DOMAIN     defaults to teal.localhost
#   --admin-email=EMAIL      defaults to admin@<base-domain>
#   --data-dir=PATH          defaults to /var/lib/teal
#   --version=TAG            defaults to "latest"
#
# Re-running this script on a host that already has Teal is safe: it
# detects existing /etc/teal/.env, refuses to overwrite the platform
# secret, and skips Docker install if Docker is present.

set -euo pipefail

# --------- Defaults (the user can override; everything has one) ----------
BASE_DOMAIN="teal.localhost"
ADMIN_EMAIL=""
DATA_DIR="/var/lib/teal"
TEAL_VERSION="latest"
ETC_DIR="/etc/teal"
ASSUME_YES=0

# Parse flags. Uses GNU-style --key=value to keep the script POSIX-shell
# friendly without depending on getopt.
for arg in "$@"; do
  case "$arg" in
    --yes|-y) ASSUME_YES=1 ;;
    --base-domain=*) BASE_DOMAIN="${arg#*=}" ;;
    --admin-email=*) ADMIN_EMAIL="${arg#*=}" ;;
    --data-dir=*)    DATA_DIR="${arg#*=}" ;;
    --version=*)     TEAL_VERSION="${arg#*=}" ;;
    --help|-h)
      sed -n '3,40p' "$0"
      exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

# --------- Helpers ----------
log()  { printf '\033[1;36m[teal]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[teal]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[teal]\033[0m %s\n' "$*" >&2; exit 1; }

# When the user runs `curl … | bash`, the script's stdin IS the pipe
# carrying the script body — a plain `read` would consume the next line
# of the script itself instead of waiting for keyboard input. Routing
# reads through /dev/tty bypasses the pipe and talks to the terminal
# directly. If there's no tty (cron, cloud-init), we require --yes
# rather than silently defaulting.
HAVE_TTY=0
if [ -r /dev/tty ] && [ -w /dev/tty ]; then
  HAVE_TTY=1
fi
if [ "$ASSUME_YES" = "0" ] && [ "$HAVE_TTY" = "0" ]; then
  cat >&2 <<'EOF'
[teal] No interactive terminal detected (running headless?).
       Re-run with --yes to accept all defaults:

         curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash -s -- --yes

       Or pass --base-domain=… and --admin-email=… explicitly.
EOF
  exit 1
fi

ask() {
  # ask "prompt" default → echoes user's reply (or default in --yes mode)
  local prompt="$1" default="$2" reply=""
  if [ "$ASSUME_YES" = "1" ]; then
    echo "$default"
    return 0
  fi
  printf '\033[1;36m[teal]\033[0m %s [%s]: ' "$prompt" "$default" >/dev/tty
  read -r reply </dev/tty || reply=""
  if [ -z "$reply" ]; then
    echo "$default"
  else
    echo "$reply"
  fi
}

confirm() {
  # confirm "prompt" → 0 (yes) / 1 (no). --yes makes it always yes.
  local prompt="$1" reply=""
  if [ "$ASSUME_YES" = "1" ]; then return 0; fi
  printf '\033[1;36m[teal]\033[0m %s [Y/n] ' "$prompt" >/dev/tty
  read -r reply </dev/tty || reply=""
  case "${reply:-y}" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "This script must be run as root (try: sudo bash install.sh)"
  fi
}

require_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

random_hex() {
  # 32 bytes → 64 hex chars. openssl is preinstalled on every distro
  # we support; if it's missing we surface the gap clearly.
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  elif [ -r /dev/urandom ]; then
    head -c 32 /dev/urandom | od -An -vtx1 | tr -d ' \n'
  else
    die "cannot generate a secret: install openssl or ensure /dev/urandom is readable"
  fi
}

# --------- Preflight ----------
log "Teal installer — see https://github.com/sariakos/teal for docs"
require_root
require_cmd uname
require_cmd curl
require_cmd tee

# --------- Docker ----------
if ! command -v docker >/dev/null 2>&1; then
  warn "Docker Engine not found."
  if confirm "Install Docker via the official convenience script?"; then
    curl -fsSL https://get.docker.com | sh
  else
    die "Docker is required. Install it then re-run this script."
  fi
fi
docker info >/dev/null 2>&1 || die "docker is installed but not running. Start it (systemctl start docker) and re-run."
docker compose version >/dev/null 2>&1 || die "docker compose plugin is required (Docker Compose v2)."

# --------- Prompts (only ask what really needs a human) ----------
BASE_DOMAIN=$(ask "Base domain for the Teal UI" "$BASE_DOMAIN")
if [ -z "$ADMIN_EMAIL" ]; then
  ADMIN_EMAIL=$(ask "Admin email (used at first sign-in)" "admin@$BASE_DOMAIN")
fi

# Everything below is defaulted — no prompt unless overridden by flag.
mkdir -p "$ETC_DIR" "$DATA_DIR" "$DATA_DIR/traefik/dynamic" "$DATA_DIR/traefik/acme"
chmod 0750 "$ETC_DIR" "$DATA_DIR"

# --------- Secret ----------
ENV_FILE="$ETC_DIR/.env"
if [ -f "$ENV_FILE" ]; then
  log "Existing $ENV_FILE found — keeping the existing PLATFORM_SECRET."
else
  log "Generating a new PLATFORM_SECRET (32 bytes, hex)."
  SECRET=$(random_hex)
  cat > "$ENV_FILE" <<EOF
# Generated by Teal install.sh — do not commit, do not paste into Slack.
TEAL_PLATFORM_SECRET=$SECRET
TEAL_BASE_DOMAIN=$BASE_DOMAIN
TEAL_ADMIN_EMAIL=$ADMIN_EMAIL
EOF
  chmod 0600 "$ENV_FILE"
fi

# --------- Compose ----------
COMPOSE_FILE="$ETC_DIR/docker-compose.yml"
log "Writing $COMPOSE_FILE"
# Embedded copy of deploy/docker-compose.platform.yml so the installer
# is one self-contained download. Update both files together when the
# platform compose changes.
cat > "$COMPOSE_FILE" <<COMPOSE
services:
  teal:
    image: ghcr.io/sariakos/teal:${TEAL_VERSION}
    container_name: teal
    restart: unless-stopped
    env_file:
      - ./.env
    environment:
      TEAL_ENV: prod
      TEAL_HTTP_ADDR: ":3000"
      TEAL_DB_PATH: ${DATA_DIR}/teal.db
      TEAL_WORKDIR_ROOT: ${DATA_DIR}
      TEAL_TRAEFIK_DYNAMIC_DIR: ${DATA_DIR}/traefik/dynamic
      TEAL_TRAEFIK_STATIC_PATH: ${DATA_DIR}/traefik/traefik.yml
      TEAL_CONTAINER_LOGS_DIR: ${DATA_DIR}/container-logs
    volumes:
      - ${DATA_DIR}:${DATA_DIR}
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - platform_proxy
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.teal.rule=Host(\`${BASE_DOMAIN}\`)"
      - "traefik.http.routers.teal.entrypoints=web"
      - "traefik.http.services.teal.loadbalancer.server.port=3000"

  traefik:
    image: traefik:v3.5
    container_name: teal-traefik
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ${DATA_DIR}/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
      - ${DATA_DIR}/traefik/dynamic:/etc/traefik/dynamic:ro
      - ${DATA_DIR}/traefik/acme:/etc/traefik/acme
    networks:
      - platform_proxy
    depends_on:
      - teal

networks:
  platform_proxy:
    name: platform_proxy
    external: true
COMPOSE

# --------- Bootstrap network + traefik static (safe defaults until Teal regenerates) ----------
docker network inspect platform_proxy >/dev/null 2>&1 || \
  docker network create --label teal.managed=true platform_proxy

if [ ! -f "$DATA_DIR/traefik/traefik.yml" ]; then
  cat > "$DATA_DIR/traefik/traefik.yml" <<TRAEFIK
api:
  dashboard: true
  insecure: false
entryPoints:
  web:
    address: ":80"
providers:
  file:
    directory: /etc/traefik/dynamic
    watch: true
log:
  level: INFO
TRAEFIK
fi
[ -f "$DATA_DIR/traefik/acme/acme.json" ] || {
  touch "$DATA_DIR/traefik/acme/acme.json"
  chmod 0600 "$DATA_DIR/traefik/acme/acme.json"
}

# --------- Up ----------
log "Pulling images and starting the stack…"
( cd "$ETC_DIR" && docker compose pull && docker compose up -d )

# --------- Done ----------
cat <<DONE

────────────────────────────────────────────────────────────────────
 Teal is up.

   URL              http://${BASE_DOMAIN}
   Bootstrap step   open the URL and create the admin account
                    (email: ${ADMIN_EMAIL})
   Data dir         ${DATA_DIR}
   Config + secret  ${ETC_DIR}/.env  (chmod 600 — back this up)
   Compose          ${ETC_DIR}/docker-compose.yml

 Common next steps:
   • Point your domain's A record at this host so HTTPS can be issued.
   • In the UI: Settings → Platform → set ACME email + Save.
   • Then restart Traefik to pick up the new static config:
       docker compose -f ${ETC_DIR}/docker-compose.yml restart traefik

 Update later:           docker compose -f ${ETC_DIR}/docker-compose.yml pull && \\
                         docker compose -f ${ETC_DIR}/docker-compose.yml up -d
 Uninstall:              bash <(curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/uninstall.sh)
────────────────────────────────────────────────────────────────────
DONE
