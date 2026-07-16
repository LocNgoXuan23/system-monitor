# System Monitor

A lightweight live system resource monitor (CPU, memory, network, per-disk
%util, GPU via NVML, filesystems, top processes) with one shared UI available
as two form factors:

- **Web app** — runs in Docker, reachable from any device on the LAN.
- **Desktop app** — a native Linux (WebKitGTK) window on the host being monitored.

Both share one engine and one frontend, so the UI is identical.

## Web app (Docker)

```bash
make web            # docker compose up -d --build; serves on the .env PORT (8090)
```

Open `http://<host>:8090`.

## Configuration

Copy `.env.example` to `.env` and set `PORT` (defaults to **8090**; do not use
8080 — it belongs to the filebrowser container). Docker Compose reads `.env`
automatically. All other settings have safe defaults.

## Desktop app (native Linux)

Requires `gtk+-3.0` and `webkit2gtk-4.1` (and CGO). Build and run:

```bash
make run-desktop            # build + open the window
make install-desktop        # install to ~/.local (binary + app-menu launcher)
system-monitor-desktop --install-autostart   # start on login
system-monitor-desktop --remove-autostart    # stop starting on login
```

The desktop app reads the host's `/proc`, `/sys`, and `/` directly (no Docker),
serves the UI on a private loopback port, and shows it in a native window. Close
the window to quit. Window size is remembered under
`~/.config/system-monitor/window.json`.

## Development

```bash
make dev            # run the web head locally on :8091 with native paths,
                    # for fast browser-based iteration on web/
make test           # full test suite
```

Editing anything in `web/` updates both form factors.
