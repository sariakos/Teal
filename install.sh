#!/usr/bin/env bash
#
# Teal installer — bootstraps the platform on a fresh Linux host.
#
# What this script does (in order):
#   1. Confirms it can do its work (root or sudo, supported OS).
#   2. Installs Docker Engine if missing (with your confirmation).
#   3. Generates a 32-byte PLATFORM_SECRET (random, stored 0600).
#   4. Generates a one-time bootstrap token (printed to your
#      terminal; required to create the very first admin so that
#      step is safe even before HTTPS is live).
#   5. Asks three questions: admin email, base domain, ACME email.
#      Pass --yes to skip every prompt.
#   6. DNS precheck: dig the base domain and warn if it doesn't
#      resolve to this host (HTTPS won't issue otherwise).
#   7. Writes /etc/teal/.env, /etc/teal/docker-compose.yml,
#      Traefik static + dynamic config (with HTTPS pre-wired when
#      ACME email is supplied).
#   8. Pulls the Teal image and starts the stack.
#   9. Prints the bootstrap URL (HTTPS when ACME is configured)
#      with the one-time token embedded.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/install.sh | bash -s -- --yes
#
# Flags:
#   --yes                    non-interactive; use defaults for everything
#   --base-domain=DOMAIN     defaults to teal.localhost
#   --admin-email=EMAIL      defaults to admin@<base-domain>
#   --acme-email=EMAIL       enables HTTPS via Let's Encrypt; "skip" disables
#                            (defaults to admin email when interactive)
#   --acme-staging           use Let's Encrypt staging CA (testing)
#   --data-dir=PATH          defaults to /var/lib/teal
#   --version=TAG            defaults to "latest"
#
# Re-running this script on a host that already has Teal is safe: it
# detects existing /etc/teal/.env, refuses to overwrite the platform
# secret, and skips Docker install if Docker is present. The bootstrap
# token is regenerated on each run but only matters until the first
# admin exists.

set -euo pipefail

# --------- Defaults (the user can override; everything has one) ----------
BASE_DOMAIN="teal.localhost"
ADMIN_EMAIL=""
ACME_EMAIL=""
ACME_STAGING=0
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
    --acme-email=*)  ACME_EMAIL="${arg#*=}" ;;
    --acme-staging)  ACME_STAGING=1 ;;
    --data-dir=*)    DATA_DIR="${arg#*=}" ;;
    --version=*)     TEAL_VERSION="${arg#*=}" ;;
    --help|-h)
      sed -n '3,46p' "$0"
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

# host_public_ip resolves the host's public IPv4 best-effort via a
# couple of well-known echo services. Empty on failure — DNS precheck
# treats that as "skip" rather than fail.
host_public_ip() {
  local ip=""
  for svc in https://api.ipify.org https://ifconfig.me/ip https://ipinfo.io/ip; do
    ip=$(curl -fsS --max-time 3 "$svc" 2>/dev/null | tr -d '[:space:]' || true)
    if [ -n "$ip" ]; then
      echo "$ip"
      return 0
    fi
  done
  echo ""
}

# resolve_domain returns the A record for $1 via the first available
# tool. dig > getent > host. Empty on failure.
resolve_domain() {
  local d="$1" out=""
  if command -v dig >/dev/null 2>&1; then
    out=$(dig +short "$d" A 2>/dev/null | grep -E '^[0-9.]+$' | head -1 || true)
  elif command -v getent >/dev/null 2>&1; then
    out=$(getent ahostsv4 "$d" 2>/dev/null | awk '{print $1; exit}' || true)
  elif command -v host >/dev/null 2>&1; then
    out=$(host -t A "$d" 2>/dev/null | awk '/has address/{print $4; exit}' || true)
  fi
  echo "$out"
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
# When re-running the installer on an existing host, prefer the values
# already in /etc/teal/.env over the script defaults — the operator
# almost certainly wants the same domain + admin they picked the first
# time, not the literal "teal.localhost" placeholder.
ENV_FILE="$ETC_DIR/.env"
if [ -f "$ENV_FILE" ]; then
  prior_domain=$(grep -E '^TEAL_BASE_DOMAIN=' "$ENV_FILE" | head -1 | cut -d= -f2-)
  prior_email=$(grep -E '^TEAL_ADMIN_EMAIL=' "$ENV_FILE" | head -1 | cut -d= -f2-)
  prior_acme=$(grep -E '^TEAL_ACME_EMAIL=' "$ENV_FILE" | head -1 | cut -d= -f2-)
  [ -n "$prior_domain" ] && BASE_DOMAIN="$prior_domain"
  [ -n "$prior_email" ] && ADMIN_EMAIL="$prior_email"
  [ -n "$prior_acme" ] && ACME_EMAIL="$prior_acme"
fi

BASE_DOMAIN=$(ask "Base domain for the Teal UI" "$BASE_DOMAIN")
if [ -z "$ADMIN_EMAIL" ]; then
  ADMIN_EMAIL=$(ask "Admin email (used at first sign-in)" "admin@$BASE_DOMAIN")
fi
if [ -z "$ACME_EMAIL" ]; then
  ACME_EMAIL=$(ask "Email for Let's Encrypt cert (or 'skip' for HTTP only)" "$ADMIN_EMAIL")
fi
case "$ACME_EMAIL" in
  skip|SKIP|none|NONE|"") ACME_EMAIL="" ;;
esac

# --------- DNS + port preflight ----------
# Best-effort: warn about misconfigurations that will keep ACME from
# issuing. Non-fatal because users sometimes intentionally run installs
# before DNS propagates and fix it later.
if [ -n "$ACME_EMAIL" ]; then
  HOST_IP=$(host_public_ip)
  RESOLVED_IP=$(resolve_domain "$BASE_DOMAIN")
  if [ -n "$HOST_IP" ] && [ -n "$RESOLVED_IP" ] && [ "$HOST_IP" != "$RESOLVED_IP" ]; then
    warn "DNS for $BASE_DOMAIN resolves to $RESOLVED_IP, but this host's public IP is $HOST_IP."
    warn "Let's Encrypt won't issue a cert until DNS points here. Continuing anyway —"
    warn "fix the A record and Traefik will retry the challenge automatically."
  elif [ -z "$RESOLVED_IP" ]; then
    warn "$BASE_DOMAIN doesn't resolve to any IPv4 yet."
    warn "Add an A record pointing to this host before HTTPS can be issued."
  fi
fi

mkdir -p "$ETC_DIR" "$DATA_DIR" "$DATA_DIR/traefik/dynamic" "$DATA_DIR/traefik/acme"
chmod 0750 "$ETC_DIR" "$DATA_DIR"

# --------- Secret + bootstrap token ----------
if [ -f "$ENV_FILE" ]; then
  log "Existing $ENV_FILE found — keeping the existing PLATFORM_SECRET."
  # Strip prior bootstrap-related lines; we'll re-emit them below. The
  # token is re-generated on every install run so a leaked one becomes
  # useless after the next install. (Once an admin exists, the token
  # is irrelevant — registerBootstrap 409s regardless.)
  grep -v -E '^(TEAL_BOOTSTRAP_TOKEN|TEAL_ACME_EMAIL|TEAL_ACME_STAGING)=' "$ENV_FILE" > "$ENV_FILE.tmp"
  mv "$ENV_FILE.tmp" "$ENV_FILE"
  chmod 0600 "$ENV_FILE"
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

BOOTSTRAP_TOKEN=$(random_hex)
{
  echo "TEAL_BOOTSTRAP_TOKEN=$BOOTSTRAP_TOKEN"
  if [ -n "$ACME_EMAIL" ]; then
    echo "TEAL_ACME_EMAIL=$ACME_EMAIL"
  fi
  if [ "$ACME_STAGING" = "1" ]; then
    echo "TEAL_ACME_STAGING=true"
  fi
} >> "$ENV_FILE"
chmod 0600 "$ENV_FILE"

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

# --------- Bootstrap network + traefik static ----------
docker network inspect platform_proxy >/dev/null 2>&1 || \
  docker network create --label teal.managed=true platform_proxy

log "Writing Traefik static config ($DATA_DIR/traefik/traefik.yml)"
if [ -n "$ACME_EMAIL" ]; then
  ACME_CA_LINE=""
  if [ "$ACME_STAGING" = "1" ]; then
    ACME_CA_LINE='      caServer: "https://acme-staging-v02.api.letsencrypt.org/directory"'
  fi
  cat > "$DATA_DIR/traefik/traefik.yml" <<TRAEFIK
api:
  dashboard: true
  insecure: false
entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
providers:
  file:
    directory: /etc/traefik/dynamic
    watch: true
certificatesResolvers:
  letsencrypt:
    acme:
      email: $ACME_EMAIL
      storage: /etc/traefik/acme/acme.json
${ACME_CA_LINE}
      httpChallenge:
        entryPoint: web
log:
  level: INFO
TRAEFIK
else
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

# --------- Platform-UI route ----------
# When ACME is configured we ship the secure router AND the redirect
# middleware from boot — installer-time HTTPS. Without ACME we still
# write the secure router shell (Traefik silently ignores routers
# whose certresolver doesn't exist), so the only thing missing is the
# resolver — which the admin can later wire from Settings → Platform.
PLATFORM_ROUTE_FILE="$DATA_DIR/traefik/dynamic/_platform.yml"
log "Writing $PLATFORM_ROUTE_FILE"
if [ -n "$ACME_EMAIL" ]; then
  cat > "$PLATFORM_ROUTE_FILE" <<ROUTE
# Generated by Teal install.sh — routes ${BASE_DOMAIN} to the
# platform UI with HTTP→HTTPS redirect. Per-app routes are managed
# by the engine in this same directory; their filenames are app
# slugs and don't collide with this one.
http:
  middlewares:
    teal-platform-redirect:
      redirectScheme:
        scheme: https
        permanent: true
  routers:
    teal-platform:
      rule: "Host(\`${BASE_DOMAIN}\`)"
      entryPoints: [web]
      service: teal-platform
      middlewares: [teal-platform-redirect]
    teal-platform-secure:
      rule: "Host(\`${BASE_DOMAIN}\`)"
      entryPoints: [websecure]
      service: teal-platform
      tls:
        certResolver: letsencrypt
  services:
    teal-platform:
      loadBalancer:
        servers:
          - url: "http://teal:3000"
ROUTE
else
  cat > "$PLATFORM_ROUTE_FILE" <<ROUTE
# Generated by Teal install.sh — routes ${BASE_DOMAIN} to the
# platform UI on HTTP. Configure ACME in the UI later (Settings →
# Platform) and re-run install.sh, or manually add the certresolver
# block to traefik.yml.
http:
  routers:
    teal-platform:
      rule: "Host(\`${BASE_DOMAIN}\`)"
      entryPoints: [web]
      service: teal-platform
  services:
    teal-platform:
      loadBalancer:
        servers:
          - url: "http://teal:3000"
ROUTE
fi

# --------- Up ----------
log "Pulling images and starting the stack…"
( cd "$ETC_DIR" && docker compose pull && docker compose up -d )

# --------- Done ----------
if [ -n "$ACME_EMAIL" ]; then
  PROTO="https"
  CERT_HINT="The first cert may take ~30-60s to issue. Watch progress:
                    docker logs -f teal-traefik | grep -i acme"
else
  PROTO="http"
  CERT_HINT="HTTPS isn't configured. To enable later: set acme.email in
                    Settings → Platform, then 'docker compose -f ${ETC_DIR}/docker-compose.yml restart traefik'."
fi

BOOTSTRAP_URL="${PROTO}://${BASE_DOMAIN}/setup?token=${BOOTSTRAP_TOKEN}"

cat <<DONE

────────────────────────────────────────────────────────────────────
 Teal is up.

   URL              ${PROTO}://${BASE_DOMAIN}
   Bootstrap URL    ${BOOTSTRAP_URL}
                    (one-time — opens the admin-create form pre-
                    authenticated by the token)
   Admin email      ${ADMIN_EMAIL}
   Data dir         ${DATA_DIR}
   Config + secret  ${ETC_DIR}/.env  (chmod 600 — back this up)
   Compose          ${ETC_DIR}/docker-compose.yml

 Notes:
   • ${CERT_HINT}
   • Make sure ports 80 + 443 are open in your firewall.
   • The bootstrap token expires the moment any admin exists — don't
     share it. If you lose it before bootstrapping, re-run install.sh.

 Update later:           docker compose -f ${ETC_DIR}/docker-compose.yml pull && \\
                         docker compose -f ${ETC_DIR}/docker-compose.yml up -d
 Uninstall:              bash <(curl -fsSL https://raw.githubusercontent.com/sariakos/teal/main/uninstall.sh)
────────────────────────────────────────────────────────────────────
DONE
