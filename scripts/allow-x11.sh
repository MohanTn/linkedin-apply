#!/usr/bin/env bash
set -euo pipefail

# Grant the backend container access to your X display so the sign-in window can open.
#
# Run this on the HOST, once per login session, before `docker compose up`.
#
# It writes .xauth-docker: a copy of your X cookie rewritten to the "wildcard"
# address family, so it is accepted from inside the container (whose hostname
# differs from your host's — the usual reason a plain XAUTHORITY mount fails).
# xhost is also relaxed as a fallback, because some setups authorise by user
# rather than by cookie.

log() {
  echo "[allow-x11.sh] $*" >&2
}

main() {
  cd "$(dirname "$0")/.."

  if [ -z "${DISPLAY:-}" ]; then
    log "DISPLAY is not set — are you on a graphical session?"
    log "If you are on Wayland without Xwayland, or on a remote/headless host,"
    log "X passthrough cannot work; see the README for the alternatives."
    exit 1
  fi
  log "DISPLAY=$DISPLAY"

  if ! command -v xauth >/dev/null 2>&1; then
    log "xauth not found — install it (Debian/Ubuntu: sudo apt install xauth)."
    exit 1
  fi

  # A directory here means docker created it from a missing bind-mount source.
  if [ -d .xauth-docker ]; then
    log "removing .xauth-docker/ (docker created it as a directory)"
    rmdir .xauth-docker 2>/dev/null || rm -rf .xauth-docker
  fi

  : >.xauth-docker
  # Family 'ffff' (FamilyWild) matches any hostname, which is what the container needs.
  if ! xauth nlist "$DISPLAY" | sed -e 's/^..../ffff/' | xauth -f .xauth-docker nmerge - 2>/dev/null; then
    log "could not read a cookie for $DISPLAY from your Xauthority"
  fi

  if [ -s .xauth-docker ]; then
    chmod 644 .xauth-docker
    log "wrote .xauth-docker ($(xauth -f .xauth-docker nlist | wc -l) cookie(s))"
  else
    log "no cookie found — your X server may not use cookie auth; relying on xhost below"
  fi

  # Belt and braces: allow the container's user (root) over the local socket.
  # +SI:localuser: is the precise modern form; +local: is the older catch-all.
  if command -v xhost >/dev/null 2>&1; then
    xhost +SI:localuser:root >/dev/null 2>&1 && log "xhost: allowed local user root" ||
      { xhost +local: >/dev/null 2>&1 && log "xhost: allowed all local connections"; } ||
      log "xhost call failed (not fatal if the cookie above was written)"
  else
    log "xhost not found — skipping (not fatal if the cookie above was written)"
  fi

  # Which displays exist, and is DISPLAY one of them? A window drawn on the
  # wrong display starts fine and is simply never seen.
  local socks
  socks=$(ls /tmp/.X11-unix 2>/dev/null | sed -e 's/^X/:/' | tr '\n' ' ')
  log "X displays on this host: ${socks:-none}"
  case " $socks " in
    *" ${DISPLAY%%.*} "*) ;;
    *) log "WARNING: $DISPLAY is not among them — a window opened there will be invisible." ;;
  esac

  # docker compose reads DISPLAY from the environment. Under sudo it is usually
  # stripped, so the container silently falls back to :0.
  if [ "${SUDO_USER:-}" != "" ]; then
    log "WARNING: running under sudo. If you also start compose with sudo, DISPLAY is"
    log "         dropped and the container falls back to :0. Prefer running compose"
    log "         without sudo, or pass:  sudo -E docker compose up -d"
  fi

  log "done. Now run:  docker compose up -d --force-recreate backend"
  log "then check:     docker compose logs backend | grep 'sign-in browser'"
}

main "$@"

# scaffold:inject
