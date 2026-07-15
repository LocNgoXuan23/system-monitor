# System Monitor Webapp — Design Spec

**Date:** 2026-07-15
**Status:** Approved (design), pending implementation plan
**Owner:** internal tooling

## 1. Overview

A lightweight web dashboard that mimics GNOME System Monitor's *Resources* tab and
updates in real time, packaged as a single Docker Compose service. It monitors the
**host** machine (not the container), adds **per-disk %util** (busy indicator) and a
**GPU panel** (nvidia-smi style), and fits everything on **one 1920×1080 page with no
scrolling**. Internal use only — **no authentication**.

Reference look-and-feel: GNOME System Monitor Resources tab (CPU per-core lines,
Memory & Swap, Network, Disk), extended with Disk %util, GPU, Filesystems, Top
processes, and temperatures.

## 2. Goals

- Visually resemble GNOME System Monitor's Resources view.
- Real-time updates (~1s tick), like the reference app.
- **Disk detail**: I/O rates **plus per-disk %util** so a busy disk is obvious.
- **GPU monitoring** like `nvidia-smi` (util, memory, temp, power, clocks, fan).
- **Everything on one 1080p page, no scroll.**
- Runs in a **Docker container via Docker Compose**.
- **Lightweight**: minimal RAM/CPU/image size while staying accurate and real-time.
- No login/auth (internal network).

## 3. Non-Goals

- Historical storage / long-term time-series database (only a rolling in-memory window).
- Alerting, multi-host aggregation, user management.
- Editing/killing processes from the UI (read-only monitoring).
- Windows/macOS host support (Linux host only).

## 4. Target Environment (verified on host)

- Ubuntu 24.04, kernel 6.17, **32 CPU cores**.
- Physical disks: `sda` (12TB HDD), `nvme0n1` (1TB), `nvme1n1` (500GB) — plus many
  `loop*` devices to be filtered out.
- **1 GPU**: NVIDIA RTX 3060 12GB; `nvidia-smi` present.
- Docker 29.6.1 + Compose v5.3.1; **`nvidia` runtime already registered**
  (nvidia-container-toolkit 1.20) — GPU-in-container works with no extra setup.
- Primary display: **1920×1080**.

The app must not hard-code these (core count, disk set, GPU count discovered at runtime),
but the layout is tuned so this configuration fits without scrolling.

## 5. Architecture

Single Go binary. One collector goroutine ticks every `INTERVAL_MS` (default 1000ms),
reads deltas, builds one compact snapshot, and a hub broadcasts it over a single
WebSocket to all connected clients. The same binary serves the static frontend (HTML/CSS/JS
+ uPlot) embedded via `embed.FS`.

```
Docker container (network_mode: host, runtime: nvidia)
  Collector goroutine --tick 1s--> read /host/proc, /host/sys, NVML  (delta computed ONCE)
      |  snapshot JSON (few KB)
      v
  Hub / broadcast --push--> all WebSocket clients
      |
  HTTP server --serve--> embedded frontend (uPlot canvas charts)

  mounts (ro): /proc -> /host/proc, /sys -> /host/sys
  nvidia runtime injects libnvidia-ml (NVML) for GPU
```

**Key efficiency principle:** compute all deltas **once per tick server-side** and
**broadcast the same snapshot** to every client. Clients never trigger recomputation.
Frontend receives ~2–5 KB/s and renders on canvas.

### Why these choices
- **Go**: tiny static binary, ~15 MB RAM, direct `/proc` reads with no dependencies,
  `embed.FS` serves the frontend from the same binary. Multi-stage build → tiny image.
- **uPlot**: ~40 KB canvas charting, extremely fast with many series (32 CPU lines).
- **Vanilla JS/HTML/CSS**: no framework → lightest possible frontend.
- **WebSocket push**: server-driven, one message/tick, no client polling.

## 6. Metric Collection (accurate = same kernel sources as standard tools)

All host files read from `/host/proc` and `/host/sys` (bind-mounted read-only). Path
prefixes are configurable via env (`HOST_PROC`, `HOST_SYS`) so collectors are testable
against fixtures.

| Metric | Source | Computation |
|---|---|---|
| CPU per-core % | `/host/proc/stat` (`cpu0..cpuN` lines) | `busy = total - idle - iowait`; `%= Δbusy/Δtotal × 100` per core, per tick |
| CPU aggregate % | `/host/proc/stat` (`cpu` line) | same, aggregate |
| Load average | `/host/proc/loadavg` | as-is |
| Uptime | `/host/proc/uptime` | as-is |
| Memory | `/host/proc/meminfo` | `used = MemTotal - MemAvailable`; `cache = Cached + Buffers + SReclaimable` |
| Swap | `/host/proc/meminfo` | `SwapTotal - SwapFree` |
| Network RX/TX rate | `/host/proc/net/dev` | Σ over real ifaces (exclude `lo`, `docker*`, `veth*`, `br-*`); `Δbytes/Δt`; keep totals |
| Disk I/O rate | `/host/proc/diskstats` | read `Δsectors_read×512/Δt`, write `Δsectors_written×512/Δt` |
| **Disk %util** | `/host/proc/diskstats` field 10 (`io_ticks`, ms doing I/O) | **`%util = Δio_ticks / Δt_ms × 100`** (same as `iostat`), clamped 0–100, per disk |
| GPU | **NVML** via `github.com/NVIDIA/go-nvml` | util%, mem used/total, temp, power draw, SM/mem clocks, fan% — per GPU |
| Filesystem | `statfs` on each real mount (from `/host/proc/mounts`) | used/free/total, %; exclude pseudo fs (tmpfs, proc, sysfs, cgroup, overlay, etc.) |
| Top processes | scan `/host/proc/[pid]/stat` + `comm`/`status` | `Δ(utime+stime)/Δt` → CPU%; RSS from `status`; sort desc, take `PROC_TOP_N` |
| CPU temperature | `/host/sys/class/hwmon/*/temp*_input` (label `Package`/`Tctl`/`Tccd`) | °C |

Notes:
- Disk device filter: enumerate `/host/sys/block/*`, exclude `loop*`, `ram*`, `zram*`,
  `dm-*` (configurable); keep whole disks (`sda`, `nvme0n1`, ...). Disk model/rotational
  read from `/host/sys/block/<dev>/device/model` and `queue/rotational`.
- Host PIDs are visible because `/host/proc` is a bind mount of the host `/proc`
  (no `pid: host` needed).
- Network stats are correct because the container runs with `network_mode: host`
  (host network namespace) → no NAT overhead either.
- First tick has no previous sample → rates reported as 0 (needs two samples for a delta).

### Optional / future (not in v1 unless approved)
- GPU per-process table (NVML process APIs matched to PIDs) — nvidia-smi's process list.
- CPU per-core temperatures (per-CCD).

## 7. Data Model (snapshot broadcast each tick)

Compact JSON. Field names shortened where it materially reduces payload. Illustrative
shape (final field names finalized in implementation):

```jsonc
{
  "t": 1752600456,                 // unix seconds
  "host": {"name":"...", "uptime":  259200, "load":[4.2,3.8,3.1]},
  "cpu": {"agg": 42.0, "cores": [4.0, 0.0, 7.0, ...], "temp": 58.0},
  "mem": {"total": 33400, "used": 13700, "cache": 15300, "pct": 41.0,
          "swap_total": 34400, "swap_used": 13100, "swap_pct": 38.0},   // MB
  "net": {"rx": 30720, "tx": 250880, "rx_total": ..., "tx_total": ...},  // bytes/s, bytes
  "disk": {
    "io": {"read": 8388608, "write": 2621440, "read_total": ..., "write_total": ...},
    "devs": [ {"name":"sda","util":78.0,"read":...,"write":...,"model":"..."}, ... ]
  },
  "gpu": [ {"name":"RTX 3060","util":71,"mem_used":8100,"mem_total":12288,
            "temp":63,"power":120,"clk_sm":1700,"fan":45} ],
  "fs": [ {"mount":"/","used":6800000,"total":10900000,"pct":62.0}, ... ],
  "proc": [ {"pid":123,"name":"chrome","cpu":21.0,"rss":2100} ]        // MB
}
```

The server also keeps a rolling window of the last `HISTORY_SECONDS` (default 60) points
per time-series so a newly connected client can be sent the recent history once on connect
(so charts render populated immediately, matching the 60s x-axis of the reference app).

## 8. UI / Layout (1920×1080, no scroll)

Nine logical items merged into **7 tiles** in a 3-row CSS grid. CPU and GPU are prominent
on the top row; the middle row holds the three real-time charts; the bottom row holds the
two tables. Temperatures fold into the CPU tile (CPU temp) and GPU tile (GPU temp).

```
+-- host . uptime . load . clock -------------------------------------------+
| CPU  32-line chart (60s)            42%  | GPU  RTX 3060          util 71% |
| cores: 32 mini heatmap bars   CPU 58C    | util chart  mem 8.1/12GB  63C  |
|                                          | 120W . clk 1.7GHz . fan 45%    |
+-------------+---------------+------------+--------------------------------+
| Memory/Swap | Network chart | Disk chart  (read/write)                    |
| 13.7GB 41%  | down/up + tot | %util: sda 78%  nvme0 5%  nvme1 2% + totals |
+-------------+---------------+------------+--------------------------------+
| Filesystems (mounts: used/total, bar)    | Top processes (CPU% / RSS)     |
+------------------------------------------+--------------------------------+
```

Design decisions:
- Keep the familiar **32-line CPU chart**, but replace GNOME's tall 32-row text legend
  with a **compact strip of 32 mini bars (heatmap)** → instantly shows which core is busy
  in far less vertical space.
- **Disk tile shows per-disk %util** bars (the busy indicator) alongside the I/O chart.
- Charts: uPlot canvas, 60s rolling window, ~1s update. Color palette echoes GNOME.
- Responsive/fluid grid using `vh/vw`/`fr` units and `clamp()` so it also degrades
  gracefully on larger screens, but tuned to fit 1080p exactly.
- Dark theme by default (monitoring context), light-theme-friendly CSS variables.
- WebSocket auto-reconnect with backoff; a small "disconnected" indicator when the
  socket drops.

## 9. Docker / Deployment

- **Multi-stage Dockerfile**: `golang` build stage → copy static binary into
  `scratch` or `gcr.io/distroless/static` → image target **< 30 MB**.
- **docker-compose.yml**:
  - `runtime: nvidia` + `NVIDIA_VISIBLE_DEVICES=all`, `NVIDIA_DRIVER_CAPABILITIES=utility`.
  - `network_mode: host`.
  - Read-only mounts: `/proc:/host/proc:ro`, `/sys:/host/sys:ro`.
  - No `privileged`. `restart: unless-stopped`.
  - Env: `PORT=8080`, `INTERVAL_MS=1000`, `HISTORY_SECONDS=60`, `PROC_TOP_N=8`,
    `HOST_PROC=/host/proc`, `HOST_SYS=/host/sys`, `DISK_EXCLUDE=loop,ram,zram,dm-`.
- If GPU/NVML is unavailable at runtime, the app starts normally and hides the GPU tile.

## 10. Resource Budget (targets)

| Resource | Target |
|---|---|
| Container RAM | < 30 MB |
| CPU | < 1–2% of one core (one collector/second) |
| Image size | < 30 MB |
| Network payload | ~2–5 KB/s per client |

Heaviest work is the per-process scan; if it proves costly it can run at a lower cadence
(e.g., every 2s) via config while charts stay at 1s.

## 11. Project Structure

```
system_monitor_service/
  docker-compose.yml
  Dockerfile
  go.mod / go.sum
  cmd/monitor/main.go
  internal/
    config/config.go            # env parsing, defaults
    model/snapshot.go           # shared structs
    collect/
      cpu.go  mem.go  net.go  disk.go  gpu.go  fs.go  proc.go  temp.go
      collect.go                # orchestrates one tick -> Snapshot
    server/
      http.go  hub.go  ws.go    # static serving + broadcast + websocket
  web/
    index.html  style.css  app.js  uplot.min.js   # embedded via embed.FS
  docs/superpowers/specs/2026-07-15-system-monitor-webapp-design.md
```

Each collector is an independent unit: input = fixture-able file paths / NVML handle,
output = a typed struct. No collector reaches into another's internals.

## 12. Error Handling & Resilience

- Per-collector errors are captured and do not crash the tick; the affected section is
  omitted/marked stale rather than taking down the whole snapshot.
- GPU: NVML init failure → `gpu: []`, GPU tile hidden. Handles 0..N GPUs generically.
- Missing hwmon/temp → temp omitted, no error surfaced to user.
- Disk/mount hotplug → device and mount lists re-enumerated periodically.
- First tick: rates = 0 (delta needs two samples).
- Frontend: WebSocket auto-reconnect with backoff; stale-data indicator.
- Malformed `/proc` lines are skipped defensively (kernels vary field counts).

## 13. Testing Strategy

- **Unit tests per collector** using captured fixture files (real `/proc/stat`,
  `/proc/diskstats`, `/proc/meminfo`, `/proc/net/dev`, `/host/sys/...` samples).
- **Delta math tests**: feed two consecutive fixture snapshots, assert computed
  rates/%util/CPU% match hand-computed expected values (e.g., known `io_ticks` delta →
  known %util). Deterministic, no live host needed.
- Filter tests: loop/pseudo devices and pseudo filesystems excluded correctly.
- GPU collector behind an interface so NVML can be faked in tests; real NVML exercised
  via a manual/integration check in the container.
- A smoke test: run the container, connect a WebSocket client, assert a well-formed
  snapshot arrives within ~2 ticks and the frontend serves 200.

## 14. Configuration Summary

| Env | Default | Meaning |
|---|---|---|
| `PORT` | 8080 | HTTP/WS listen port |
| `INTERVAL_MS` | 1000 | collector tick interval |
| `HISTORY_SECONDS` | 60 | rolling chart window sent on connect |
| `PROC_TOP_N` | 8 | number of top processes |
| `PROC_INTERVAL_MS` | = INTERVAL_MS | optional slower cadence for process scan |
| `HOST_PROC` | /host/proc | procfs path |
| `HOST_SYS` | /host/sys | sysfs path |
| `DISK_EXCLUDE` | loop,ram,zram,dm- | disk name prefixes to skip |

## 15. Open Questions / Future Work

- GPU per-process table (nvidia-smi style) — deferred unless requested; adds NVML process
  querying and vertical space.
- Per-core CPU temperatures — deferred; package temp only in v1.
- Optional configurable faster tick (e.g., 500 ms) if smoother charts are desired.
