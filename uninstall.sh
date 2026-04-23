#!/usr/bin/env bash
#
# Teal uninstaller — removes the platform from the host.
#
# What this does:
#   1. Stops and removes the Teal + Traefik containers.
#   2. Removes the platform_proxy network if no app containers still
#      attach to it.
#   3. With your confirmation: deletes /var/lib/teal (deployment
#      workdirs, container log buffer, SQLite DB, Traefik state).
#   4. Removes /etc/teal (config + .env containing PLATFORM_SECRET).
#
# What this does NOT do:
#   - Touch your user-app containers or volumes. They are not part of
#     the Teal install; remove them with `docker compose down -v` against
#     each app's compose file before running this script if you want a
#     full wipe.
#
# Usage:
#   sudo bash uninstall.sh            # interactive; asks before deleting data
#   sudo bash uninstall.sh --yes      # non-interactive; data dir KEPT (safe)
#   sudo bash uninstall.sh --purge    # also delete /var/lib/teal (DESTRUCTIVE)

set -euo pipefail

ETC_DIR="/etc/teal"
DATA_DIR="/var/lib/teal"
ASSUME_YES=0
PURGE=0

for arg in "$@"; do
  case "$arg" in
    --yes|-y) ASSUME_YES=1 ;;
    --purge) PURGE=1 ;;
    --data-dir=*) DATA_DIR="${arg#*=}" ;;
    --help|-h) sed -n '3,30p' "$0"; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1;36m[teal]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[teal]\033[0m %s\n' "$*" >&2; }

confirm() {
  local prompt="$1"
  if [ "$ASSUME_YES" = "1" ]; then return 0; fi
  printf '\033[1;33m[teal]\033[0m %s [y/N] ' "$prompt" >&2
  local r=""
  read -r r || r=""
  case "${r:-n}" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

if [ "$(id -u)" -ne 0 ]; then
  echo "Run as root (sudo bash uninstall.sh)" >&2
  exit 1
fi

if [ -f "$ETC_DIR/docker-compose.yml" ]; then
  log "Stopping the Teal stack…"
  ( cd "$ETC_DIR" && docker compose down ) || warn "docker compose down failed (continuing)"
fi

# Remove the network only if no other containers attach.
if docker network inspect platform_proxy >/dev/null 2>&1; then
  in_use=$(docker network inspect platform_proxy --format '{{len .Containers}}')
  if [ "$in_use" = "0" ]; then
    docker network rm platform_proxy >/dev/null 2>&1 && log "Removed platform_proxy network."
  else
    warn "platform_proxy still has $in_use container(s) attached — leaving it in place."
  fi
fi

if [ "$PURGE" = "1" ] || confirm "Delete platform DATA at $DATA_DIR (deployment logs, db, traefik state)?"; then
  if [ -d "$DATA_DIR" ]; then
    rm -rf "$DATA_DIR"
    log "Removed $DATA_DIR"
  fi
else
  log "Kept $DATA_DIR — your deployment history + db survive."
fi

if [ -d "$ETC_DIR" ]; then
  rm -rf "$ETC_DIR"
  log "Removed $ETC_DIR (config + .env)."
fi

log "Done. Teal is removed from this host."
