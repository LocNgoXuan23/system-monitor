# Desktop + Web System Monitor — Design

**Date:** 2026-07-16
**Status:** Approved (design), pending spec review before planning.

## Goal

Ship the existing system monitor as **two form factors that share one core and one UI**:

1. **Web app** — the current Docker container, serving the UI over HTTP + WebSocket. Unchanged in behavior.
2. **Desktop app** — a native Linux binary that opens a WebKitGTK window showing the *same* UI, reading host metrics natively (no Docker).

Editing the UI or a collector once must update both apps. High-performance and lightweight remain the governing principles.

## Non-Goals

- Windows / macOS support. **Linux-only** (this is the machine being monitored; GNOME/X11).
- System-tray icon. Explicitly dropped to stay lean (tray fights the webview for the GTK main loop on GNOME). May be added later.
- Native data bindings between JS and Go. The desktop deliberately reuses the HTTP+WebSocket path over loopback so the frontend stays byte-for-byte identical.
- Changing the web app's deployment, port (8090), `network_mode: host`, or its no-auth-on-LAN posture.

## Key Finding: the desktop needs zero collector changes

Verified on the host, as the normal (non-root) user, outside any container:

- `/proc/1/mounts`, `/proc/self/mounts`, `/proc/stat`, `/sys/class/hwmon/*` are all readable natively.
- `libnvidia-ml.so` is present natively, so GPU metrics via NVML work without the nvidia Docker runtime.
- `webkit2gtk-4.1` (dev + runtime), GTK3, gcc, and Go 1.23 are all installed.
- The host's PID-1 mounts include many Docker `overlay2` bind mounts, but `fs.go` already filters these — the web app reads the same PID-1 mount table and shows a clean 4 filesystems.

Therefore the desktop app runs the existing collectors verbatim, only pointing them at native paths (`/proc`, `/sys`, real `/`) instead of the container's `/host/*`. That is a configuration difference, not a code difference.

## Architecture — one core, two heads

```
cmd/
  web/          web-server binary (Docker, port 8090)   [renamed from cmd/monitor]
  desktop/      native binary (WebKitGTK window)         [NEW]
internal/
  collect/      UNCHANGED — proc/sys/NVML collectors + tests
  model/        UNCHANGED — Snapshot types
  config/       REFACTOR — web defaults (/host/*) vs desktop defaults (/proc,/sys,"")
  engine/       NEW — ticker + history ring + broadcast hub (transport-agnostic)
  server/       SLIMMED — HTTP + WebSocket handlers only, driven by engine
  desktop/      NEW — webview window, window-size persistence, autostart install
web/            UNCHANGED UI (HTML/CSS/JS) — identical in both apps
```

### Data flow (both heads)

```
collect.Collector.Tick(now) → model.Snapshot
        │
     engine (ticker cadence, history ring capped at HistorySec, broadcast fan-out)
        │
        ├─ web head:     server → HTTP FileServer(web.FS) + /ws WebSocket → browser (LAN :8090)
        └─ desktop head: server on 127.0.0.1:<random> ── webview window → http://127.0.0.1:<port>
```

Both heads mount the same embedded `web.FS` and the same `/ws` WebSocket endpoint through the `server` package. The desktop head differs only in (a) the bind address and (b) opening a native window instead of relying on a browser.

## Component responsibilities

### internal/engine (NEW)

Owns the transport-agnostic runtime currently embedded in `server.go` and `hub.go`:

- Holds the `*collect.Collector` and drives it on an interval ticker.
- Maintains the history ring buffer (last `HistorySec` marshaled snapshots) and the latest snapshot.
- Provides subscribe/unsubscribe for broadcast consumers and accessors for history + last snapshot.

**Rationale:** the ticker/history/fan-out logic is not inherently HTTP. Extracting it makes `server` a thin transport, makes the runtime unit-testable without a socket, and gives both heads a single shared source of truth.

### internal/server (SLIMMED)

HTTP + WebSocket transport only, driven by an `*engine.Engine`:

- `GET /` → `http.FileServer(http.FS(web.FS))`.
- `GET /ws` → WebSocket upgrade; on connect sends the `init` history frame, then streams `tick` frames from an engine subscription. `CheckOrigin` stays permissive (documented, LAN-internal).
- `GET /api/snapshot`, `GET /healthz` — unchanged.
- **New:** `Serve(ln net.Listener) error` so a caller can supply a pre-bound listener (the desktop binds `127.0.0.1:0` and reads the chosen port). `Run()` keeps its current signature by constructing a TCP listener on `cfg.Port` and delegating to `Serve`.

### internal/config (REFACTOR)

Today `Load()` hardcodes `/host/*` defaults. Refactor so the host-path defaults are supplied by the caller:

- Web head → defaults `HostProc=/host/proc`, `HostSys=/host/sys`, `HostRoot=/host/root` (current behavior).
- Desktop head → defaults `HostProc=/proc`, `HostSys=/sys`, `HostRoot=""` (real host, `filepath.Join("", "/x") == "/x"`).

Environment variables still override every field. The `env`/`envInt` helpers and the `n > 0` guard are unchanged.

### cmd/web (renamed from cmd/monitor)

Wiring for the web head: load web config, build collector + engine, run `server` on `cfg.Port`. Behaviorally identical to today. The Dockerfile's build target path updates to `cmd/web`.

### cmd/desktop (NEW)

Wiring for the desktop head:

- Flags: default run opens the window; `--install-autostart` and `--remove-autostart` manage the autostart entry and exit.
- Load desktop config, build collector + engine.
- Bind `127.0.0.1:0`, start `server.Serve(ln)` in a goroutine, read the actual port from `ln.Addr()`.
- Restore saved window size, open the webview at `http://127.0.0.1:<port>`, run.
- On window close, persist the current window size, then exit (close = quit).

### internal/desktop (NEW)

Desktop-only support code:

- **Window:** create/configure the WebKitGTK window via the `webview` Go lib (`webkit2gtk-4.1`), set initial size, navigate to the loopback URL, run the GTK loop.
- **Size persistence:** load/save `{width, height}` at `~/.config/system-monitor/window.json` (default 1440×900). Reading the final size on close uses a small CGO helper calling `gtk_window_get_size` on the webview's window handle.
- **Autostart:** write/remove `~/.config/autostart/system-monitor.desktop` pointing at the installed binary. Explicit (flag-driven), never silent.

## Build / dev / switch — Makefile

- `make web` — `docker compose up -d --build` (unchanged; port 8090).
- `make desktop` — native build (`CGO_ENABLED=1`, webkit2gtk-4.1) → `bin/system-monitor-desktop`.
- `make run-desktop` — build + launch the window.
- `make dev` — run the server locally on a fixed port for fast browser-based UI iteration on `web/` (no window rebuild loop).
- `make install-desktop` — copy the binary to `~/.local/bin` and a `.desktop` launcher into `~/.local/share/applications`.
- `make test` — `go test ./...` (with the Go toolchain PATH and `CGO_ENABLED=1`).

**Coexistence / switching:** nothing to toggle. The web container runs continuously on `:8090` for any LAN device; the desktop app is launched when a native window is wanted. Both read the same host and render the same UI.

## Testing strategy

- `collect/*` — unchanged tests continue to pass.
- `engine` — unit tests for ticker cadence, ring-buffer cap at `HistorySec`, and broadcast fan-out to multiple subscribers (absorb the existing `hub_test`).
- `server` — adapted to `Serve(listener)`; existing HTTP/WS behavior preserved.
- `config` — tests for web defaults, desktop defaults, and env override precedence.
- `desktop` — pure unit tests for window-size JSON round-trip and the autostart `.desktop` writer (writing into a temp dir). The webview glue is kept thin and validated by a real build + launch smoke test.

## Risks and gates

**Only real risk:** the `webview` Go lib building cleanly against `webkit2gtk-4.1`.

**Gate:** the first implementation task is a minimal spike — a blank native window that loads a loopback URL and closes cleanly. If the lib misbehaves on 4.1, the fallback is a ~60-line direct CGO wrapper around `webkit2gtk-4.1` + GTK3 (both installed). This changes the window primitive only; no other part of the design moves.

## Deliverables

- `internal/engine` extracted and tested; `server` slimmed to transport.
- `internal/config` refactored for per-head defaults.
- `cmd/web` (renamed) building and running as today in Docker.
- `cmd/desktop` + `internal/desktop` producing a native window sharing the UI, with remember-window-size and flag-driven autostart.
- `Makefile` with web/desktop/dev/install/test targets.
- The `web/` UI unchanged and working identically in both heads.
