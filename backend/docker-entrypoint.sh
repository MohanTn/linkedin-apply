#!/usr/bin/env bash
set -euo pipefail

# Start the in-container display (Xvfb + VNC + noVNC) then run the server.
#
# The cockpit's Sign in button opens a real browser window. Rather than borrow
# the host's X server — which needs cookie wrangling and breaks on Wayland,
# macOS, and remote hosts — the container runs its own display and streams it to
# a browser tab, so signing in works the same way everywhere.
#
# Set USE_HOST_DISPLAY=true to skip all of this and draw on the host's X server
# instead (then DISPLAY/XAUTHORITY must be passed in; see scripts/allow-x11.sh).

log() {
  echo "[docker-entrypoint.sh] $*" >&2
}

main() {
  if [ "${HEADLESS:-false}" = "true" ] || [ "${USE_HOST_DISPLAY:-false}" = "true" ]; then
    log "not starting the built-in display (HEADLESS=${HEADLESS:-false}, USE_HOST_DISPLAY=${USE_HOST_DISPLAY:-false})"
    exec "$@"
  fi

  local display="${DISPLAY:-:99}"
  local screen="${display#:}"
  screen="${screen%%.*}"
  local vnc_port="${VNC_PORT:-5900}"
  local web_port="${NOVNC_PORT:-7900}"
  local geometry="${SCREEN_SIZE:-1440x900x24}"

  # A stale lock from a killed container stops Xvfb from claiming the display.
  rm -f "/tmp/.X${screen}-lock" "/tmp/.X11-unix/X${screen}" 2>/dev/null || true

  log "starting Xvfb on $display ($geometry)"
  Xvfb "$display" -screen 0 "$geometry" -nolisten tcp &

  # Wait for the socket rather than sleeping a fixed amount.
  local i
  for i in $(seq 1 50); do
    [ -S "/tmp/.X11-unix/X${screen}" ] && break
    sleep 0.1
  done
  if [ ! -S "/tmp/.X11-unix/X${screen}" ]; then
    log "ERROR: Xvfb did not come up on $display"
    exit 1
  fi

  log "starting x11vnc on port $vnc_port"
  x11vnc -display "$display" -nopw -listen 0.0.0.0 -rfbport "$vnc_port" \
    -forever -shared -quiet -bg >/dev/null

  log "starting noVNC on port $web_port"
  websockify --web=/usr/share/novnc "$web_port" "localhost:${vnc_port}" &

  export DISPLAY="$display"
  log "sign-in window will be viewable at http://localhost:${web_port}/vnc.html"
  exec "$@"
}

main "$@"

# scaffold:inject
