#!/usr/bin/env bash
# Deploy system-monitor: rebuild + reinstall the desktop app, restart its
# window, and stop the debug web head.
#
# The user runs the DESKTOP app as their real app. The web head (:8090, or
# :8091 via `make dev`) exists ONLY for Claude to debug the UI with browser-use.
# So "deploy" = make the change live in the desktop app + clean up the web head.
#
# Safe ordering: build to bin/ FIRST while the old window keeps running; only
# after the build succeeds do we stop the old window and swap the installed
# binary — so a broken build never leaves the user without a desktop app.
#
# Idempotent: run it whether or not the web/desktop are currently up.
# Host-specific (paths, DISPLAY=:1). NEVER touches :8080 (filebrowser owns it).
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DESKTOP_BIN="$HOME/.local/bin/system-monitor-desktop"
LOG="/tmp/sm-desktop.log"
UID_N="$(id -u)"
cd "$REPO" || { echo "FATAL: repo root not found from script path"; exit 1; }

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

# 1. Build the desktop binary to bin/ (running app untouched) -----------------
say "Building desktop binary (make desktop)…"
if ! make desktop; then
  echo "FATAL: build failed — running desktop app left untouched. Fix and re-run."
  exit 1
fi

# 2. Capture the live X-session env from the running window, then stop it -----
# comm is truncated to 15 chars, so match the full binary PATH with -f, never -x.
getenv() { tr '\0' '\n' < "/proc/$1/environ" 2>/dev/null | sed -n "s/^$2=//p" | head -n1; }
OLD_PIDS="$(pgrep -f "$DESKTOP_BIN" || true)"
SRC=""
for p in $OLD_PIDS; do [ -r "/proc/$p/environ" ] && { SRC="$p"; break; }; done
DISPLAY_V=""; XAUTH_V=""; DBUS_V=""; XRD_V=""
if [ -n "$SRC" ]; then
  DISPLAY_V="$(getenv "$SRC" DISPLAY)"
  XAUTH_V="$(getenv "$SRC" XAUTHORITY)"
  DBUS_V="$(getenv "$SRC" DBUS_SESSION_BUS_ADDRESS)"
  XRD_V="$(getenv "$SRC" XDG_RUNTIME_DIR)"
fi
# Fallbacks for this host if no window was running to read from.
DISPLAY_V="${DISPLAY_V:-:1}"
XRD_V="${XRD_V:-/run/user/$UID_N}"
XAUTH_V="${XAUTH_V:-$XRD_V/gdm/Xauthority}"
DBUS_V="${DBUS_V:-unix:path=$XRD_V/bus}"

if [ -n "$OLD_PIDS" ]; then
  say "Stopping running desktop window (pids: $OLD_PIDS)…"
  kill $OLD_PIDS 2>/dev/null || true
  for _ in $(seq 1 25); do pgrep -f "$DESKTOP_BIN" >/dev/null || break; sleep 0.2; done
  rem="$(pgrep -f "$DESKTOP_BIN" || true)"
  [ -n "$rem" ] && kill -9 $rem 2>/dev/null || true
fi

# 3. Install the freshly built binary (safe now nothing executes it) ----------
say "Installing desktop binary + icon + launcher (make install-desktop)…"
make install-desktop || echo "WARN: install failed — will relaunch whatever is at $DESKTOP_BIN"

# 4. Relaunch the desktop window, detached so it survives this shell ----------
say "Relaunching desktop window on $DISPLAY_V…"
setsid env DISPLAY="$DISPLAY_V" XAUTHORITY="$XAUTH_V" \
  DBUS_SESSION_BUS_ADDRESS="$DBUS_V" XDG_RUNTIME_DIR="$XRD_V" \
  "$DESKTOP_BIN" >"$LOG" 2>&1 </dev/null &
disown 2>/dev/null || true
sleep 1
NEW_PID="$(pgrep -f "$DESKTOP_BIN" | head -n1 || true)"
if [ -n "$NEW_PID" ]; then
  echo "    desktop window up (pid $NEW_PID)"
else
  echo "    WARN: desktop did not come up — last log lines:"; tail -n 15 "$LOG" 2>/dev/null || true
fi

# 5. Stop the debug web head — :8090 (web) and :8091 (dev). NEVER :8080. ------
say "Stopping debug web head (:8090, :8091)…"
for port in 8090 8091; do
  pids="$(ss -ltnp "sport = :$port" 2>/dev/null | grep -oP 'pid=\K[0-9]+' | sort -u)"
  if [ -n "$pids" ]; then echo "    :$port -> killing $pids"; kill $pids 2>/dev/null || true; fi
done
if command -v docker >/dev/null 2>&1 && [ -f "$REPO/docker-compose.yml" ]; then
  if [ -n "$(docker compose -f "$REPO/docker-compose.yml" ps -q 2>/dev/null)" ]; then
    echo "    docker compose down"; docker compose -f "$REPO/docker-compose.yml" down 2>/dev/null || true
  fi
fi

# 6. Summary -----------------------------------------------------------------
say "Deploy complete."
if pgrep -f "$DESKTOP_BIN" >/dev/null; then
  echo "  desktop : running (pid $(pgrep -f "$DESKTOP_BIN" | head -n1))"
else
  echo "  desktop : NOT running — check $LOG"
fi
for port in 8090 8091; do
  if ss -ltn "sport = :$port" 2>/dev/null | grep -q ":$port"; then
    echo "  web :$port: still up (!)"
  else
    echo "  web :$port: off"
  fi
done
