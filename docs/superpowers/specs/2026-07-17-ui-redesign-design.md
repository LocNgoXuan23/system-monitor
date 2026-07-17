# UI Redesign — Symmetric Cards, Light Theme

**Date:** 2026-07-17
**Status:** Approved (design), pending spec review before planning.
**Source design:** `docs/new_design_system_monitor.png` (1501×1048, light theme)
**Supersedes:** the UI sections of `2026-07-15-system-monitor-webapp-design.md`. The
architecture (Go core, WebSocket streaming, two form factors) is unchanged.

## Goal

Rebuild the frontend to match the new design, with two corrections the user asked for:

1. **Symmetry.** In the source design the five left cards (CPU, RAM, GPU, Disk, Network)
   are laid out ad hoc — different chart heights, different stats blocks, plot areas that
   start at different x-coordinates. Every card must share one anatomy so the five read as
   a set, not as five one-offs.
2. **Design review.** The source design has defects beyond symmetry (fabricated data,
   duplicated rows, dropped features). They are enumerated and resolved below.

Governing constraint carried over from the existing app: **one binary, one `web/` directory,
two form factors.** Every change here lands once and shows up in both the web app and the
desktop app.

## Non-Goals

- **No dark theme.** The app becomes light-only. The existing dark palette is deleted,
  not kept behind a toggle. (User chose this explicitly.)
- **No framework.** Vanilla JS and the hand-rolled canvas `Chart` stay.
- **No new metric sources beyond the four named in "Backend changes".** Specifically: no CPU
  fan collector (see "Rejected: CPU fan"), no network link-speed probe, no per-process icons.
- **No changes to collection cadence, transport, or the WebSocket protocol shape.** Still
  `init` (history) + `tick` (snapshot).

## Defects found in the source design, and their resolution

| # | Defect | Resolution |
|---|---|---|
| 1 | Five cards are asymmetric: chart sizes, stats blocks, and plot origins all differ | One card anatomy for all five (see "Card anatomy") |
| 2 | Network card shows `Max: 1.26 MiB/s` **twice** (once for RX, once for TX, same value) | Rewritten stats block: Receiving / Sending / Total received / Total sent |
| 3 | System Summary card duplicates data already in the topbar | Card deleted; OS + Kernel move to the topbar |
| 4 | CPU card shows `Fan 0%` — no such metric exists in the app | Row removed. No CPU fan. GPU fan kept (NVML gives a real %) |
| 5 | Design draws its own window titlebar/traffic-lights in HTML | Removed. Wrong for both form factors: the web app is a browser tab, the desktop app already has a real GTK titlebar |
| 6 | Design is light; app is dark | App becomes light (decision above) |
| 7 | Memory chart is near-flat and unreadable | Chart plots mem% and swap% filled, on a fixed 0–100 axis, matching the numbers shown in the stats column |
| 8 | X-axis ends in `10 secs 0` — two labels colliding | X-axis reads `60s … 30s … now` |
| 9 | No overflow behaviour defined; design is 1048px tall and the default window is ~863px usable | Fit-to-viewport layout + enforced minimum window (see "Sizing") |
| 10 | Per-process icons | Removed (user's own suggestion) |
| 11 | 32 unlabelled CPU lines are unreadable as a spaghetti chart | Kept (user chose to match the design) **and** backed by a 32-cell per-core grid in the stats column, which is the readable view |
| 12 | Two features silently dropped vs the current app: the 32-core grid and per-disk %util | Both restored. Per-disk util is a hard requirement (user: *"tôi muốn xem utils của từng disk"*) |
| 13 | Design fabricates values the app cannot collect (`enp5s0 · 1 Gb/s`, a CPU model name, OS/kernel strings) | Each resolved individually — see "Backend changes". Nothing in the UI displays a value the collector cannot produce |

### Rejected: CPU fan

`/sys/class/hwmon/hwmon*/fan*_input` reports **RPM, not percent**, and there is no reliable
way to identify which fan is the CPU fan across machines. A `Fan 0%` row would either be
fabricated or wrong. The row is dropped rather than faked.

## Layout

```
┌──────────────────────────────────────────────────────────────────────┐
│ ⬢ System Monitor   host · OS · Kernel · up 1d4h · load 1.05 · 14:32  │
│                                                          ● Live      │  topbar, flex:none
├─────────────────────────────────────────┬────────────────────────────┤
│ CPU                             flex:1  │ Top Processes      flex:3  │
│ Memory & Swap                   flex:1  │                            │
│ GPU                             flex:1  │                            │
│ Network                         flex:1  ├────────────────────────────┤
│ Disk                            flex:1  │ Filesystems        flex:1  │
└─────────────────────────────────────────┴────────────────────────────┘
   grid-template-columns: 1fr 400px          ONE grid row
```

The grid has **exactly one row**. That is what structurally guarantees the user's
requirement that *"2 bên trái phải khi co lại phải luôn bằng nhau"* — both columns are
grid items in the same row, so their heights are equal by definition at every viewport
size. It cannot drift.

Inside each column, cards are flex children with the weights above. There is **no scrollbar
anywhere** — not on the page, not inside a card — at or above the minimum window size.

## Card anatomy (all five left cards)

```
┌────────────────────────────────────────────────────────────┐
│ [26px icon]  Title                                          │  header, flex:none
│              subtitle                                       │
├──────────┬──────────────────────────────┬───────────────────┤
│ Y gutter │ chart                        │ stats column      │
│  54px    │ flex:1                       │ 210px             │  body, flex:1
│  fixed   │                              │ fixed             │
├──────────┼──────────────────────────────┤                   │
│ (54px)   │ 60s        30s          now  │                   │  x-axis, flex:none
└──────────┴──────────────────────────────┴───────────────────┘
```

Three rules produce the symmetry:

- **The Y gutter is a fixed 54px on every card.** Not sized to its labels. This is what
  makes all five plot areas start at the same x-coordinate.
- **The stats column is a fixed 210px on every card.** This makes all five plot areas *end*
  at the same x-coordinate.
- **Every card has a subtitle**, even where it is merely derived (e.g. Disk's `4 devices`).
  A missing subtitle would make that card's header shorter and its chart taller than its
  neighbours'.

**No hero number.** The source design put a large `7%` / `53%` figure in each header. The
user removed them: the same value is already in the stats column, and the chart already
shows it.

The stats column is always: **legend rows** (each with a colour dot matching its series in
the chart) → **separator** → **detail block**. The legend rows are what tie the chart to the
numbers; without them the 210px column is just floating text.

Charts stretch with the card. **They are `<canvas>`, drawn by the existing `web/chart.js`** —
not SVG. (The approved mockups used SVG with `preserveAspectRatio="none"`; that was a
prototyping shortcut. Reusing the real `Chart` class keeps one chart implementation and
avoids re-solving DPR handling.) `Chart._resize()` already reads the element's box and
matches the backing store to it, so a flex-resized card is handled by re-rendering on
`resize`, and stroke width stays constant for free.

## Per-card specification

Every value below is traceable to a field in `internal/model/snapshot.go`, either directly
or as a client-side computation over the 60-point history. Nothing is invented.

### CPU
- **Subtitle:** `32 cores · <model name>` — core count from `cpu.cores.length`, model from
  the new `cpu.model` field.
- **Chart:** 32 lines, one per core, coloured by `hsl(i * 360/n, 65%, 55%)`. Y axis 0–100%.
- **Stats:** Average (`cpu.agg`) · Max (max of `cpu.cores`) · Temperature (`cpu.temp`, row
  hidden when `temp === 0`, which is the collector's "unknown" value) · separator ·
  `PER-CORE` caption · 32-cell grid, 16 columns, each cell a vertical fill.
- **Core grid threshold:** cells go red **above 80%** — matching the existing `app.js`, not
  the 50% used in the mockups.

### Memory & Swap
- **Subtitle:** `31.1 GiB RAM · 32.0 GiB swap` from `mem.total` / `mem.swap_total`.
- **Chart:** `mem.pct` and `mem.swap_pct`, both filled, Y axis fixed 0–100%.
- **Stats:** Memory `used / total` · dim sub-row `53% · cache 15.0 GiB` · Swap `used / total` ·
  dim sub-row `34% used` · separator · `RAM BREAKDOWN` caption · stacked bar
  used / cache / free with a small key.
- `free` is derived: `mem.total - mem.used - mem.cache`.

### GPU
- **Subtitle:** `gpu.name`.
- **Chart:** `gpu.util`, filled. Y axis 0–100%.
- **Stats:** Utilisation · Temperature · Power · Clock · **Fan (only when `gpu.fan >= 0`)** ·
  separator · `VRAM · x / y GiB` caption · bar.
- **Empty state:** `snap.gpu` is a slice and may be empty (no NVML, no card). When it is
  empty the **entire GPU card is removed from the DOM**, and the remaining four cards
  redistribute the space via `flex:1`. It is not rendered as a card full of dashes. When
  multiple GPUs are present, only `gpu[0]` is shown — matching current behaviour.

### Network
- **Subtitle:** the real interface names, e.g. `enp5s0 + wlan0`, from the new `net.ifaces`
  field. **Not** a link speed — the app does not collect one.
- **Chart:** `net.rx` and `net.tx`, filled. Y axis auto-scaled to the window max.
- **Stats:** Receiving ↓ (`net.rx`) · dim `peak 1 min` · Sending ↑ (`net.tx`) · dim
  `peak 1 min` · separator · Total received (`net.rx_total`) · Total sent (`net.tx_total`).
- Peaks are computed client-side over the 60-point history. Defect #2 is gone: RX and TX
  each get their own peak, and the totals are genuinely different numbers.

### Disk
- **Subtitle:** `4 devices` from `disk.devs.length`.
- **Chart:** `disk.read` and `disk.write`, filled. Y axis auto-scaled.
- **Stats:** Reading ↓ · Writing ↑ · separator · `UTILISATION PER DEVICE` caption · one bar
  per device (`disk.devs[].util`), **sorted by util descending**, red above 75%.
- This satisfies the user's hard requirement to see per-device utilisation. Sorting
  descending means the device that matters is always the top row, which also makes the
  block degrade gracefully when there are many devices.

### Top Processes (right column, flex:3)
- Columns: **Process / CPU / Memory / PID**. No icons.
- Sorted by CPU descending (already done server-side).
- Row count **auto-fits the available height** — see "Auto-fit".

### Filesystems (right column, flex:1)
- Columns: mount, used / total, bar, %.
- **Sorted by % used descending** — the full mount is the one you need to see.
- Row count auto-fits; when rows are trimmed, a `+N mount khác` note is appended so the
  truncation is never silent.

## Topbar

`⬢ System Monitor` · hostname · **OS** · **Kernel** · Uptime · Load · Time · `● Live` (right).

No fake window chrome. When the window narrows, **OS and Kernel are the first items to
hide** (they are the least urgent), via CSS only.

The `Live` badge reflects WebSocket state and keeps the existing reconnect behaviour
(exponential backoff 500ms → 5000ms max).

## Auto-fit

The two right-column tables must never scroll and never overflow. After each render:

```js
app.querySelectorAll('[data-fit]').forEach(wrap => {
  const tb = wrap.querySelector('tbody');
  const total = tb.rows.length;
  while (tb.rows.length > 1 && wrap.scrollHeight > wrap.clientHeight) {
    tb.deleteRow(tb.rows.length - 1);
  }
  const hidden = total - tb.rows.length;
  if (hidden > 0 && wrap.dataset.fit === 'fs') { /* append "+N mount khác" note */ }
});
```

Trim from the bottom of an already-sorted list, so what survives is always the most
important rows. This must also run on window `resize`.

**Consequence for the backend:** the client can only trim rows it was sent. `PROC_TOP_N`
must rise from **8 → 25** so a tall window has enough rows to fill. 25 is chosen to cover a
full-height Top Processes card at 1440×900 with headroom, without meaningfully growing the
tick payload.

## Sizing and overflow

The layout fits exactly one viewport and never scrolls — but only down to a floor. Below
that floor, charts would be squeezed to ~29px tall and the core grid would vanish. So there
is a floor, enforced differently per form factor:

- **Minimum size: 1100 × 780.**
- **Desktop:** hard-enforced with `gtk_window_set_geometry_hints` in
  `internal/desktop/window.go`. The user physically cannot resize below the floor, so the
  desktop never scrolls and never squishes. Default window size (1440×900) is unchanged.
- **Web:** a browser tab cannot be size-constrained. Below the floor the page falls back to
  **normal vertical scrolling** as an escape hatch (`min-width`/`min-height` on the app
  shell, `overflow:auto` on the body). At or above the floor — which is every normal case —
  behaviour is identical to the desktop: no scroll.

This keeps the "no scroll" requirement intact for real usage while refusing to render a
broken layout in the corner case.

## Theme

Light only. The palette below comes from the approved mockups:

```css
--bg:     #f6f7f9;   /* page */
--card:   #ffffff;
--line:   #e8eaee;   /* borders, separators */
--track:  #eef0f4;   /* bar/grid backgrounds */
--ink:    #1a1d23;   /* primary text */
--sub:    #8b909a;   /* secondary text */
--blue:   #2f7ff5;   /* CPU, RX */
--green:  #22c55e;   /* swap, live */
--red:    #ef4444;   /* memory, over-threshold */
--purple: #a855f7;   /* disk util, write */
--cyan:   #38bdf8;   /* read */
--amber:  #f59e0b;   /* TX */
```

**`chart.js` currently hardcodes `ctx.strokeStyle = 'rgba(255,255,255,0.06)'` for
gridlines** — invisible on white. It must read the gridline colour from a CSS variable
instead. This is the one change `chart.js` cannot avoid; the rest of its API (`push`,
`seed`, `render`, `_resize`) is unchanged, plus a new option for per-series colours to
support the 32-line CPU chart.

## Backend changes

Four additions. All are cheap reads; none change the tick cadence.

| Change | Where | Notes |
|---|---|---|
| `HostInfo.OS string` | `internal/collect/collector.go` `host()` | Parse `PRETTY_NAME` from `/etc/os-release`. Read **once at startup**, not per tick — it cannot change while the process runs. |
| `HostInfo.Kernel string` | same | `/proc/sys/kernel/osrelease` (respects `cfg.HostProc`, so the container reads the host's). Also cached at startup. |
| `NetInfo.Ifaces []string` | `internal/collect/net.go` | `ParseNetDev` currently sums rx/tx and **throws the interface name away** (`name := ...; if isVirtualIface(name) { continue }`). Collect the surviving names into `NetCounters.Ifaces` and pass them through `collector.net()`. Same filter, same exclusion list — no behaviour change to the rates. |
| `CPUInfo.Model string` | `internal/collect/cpu.go` | First `model name` line of `/proc/cpuinfo`. Cached at startup. Empty string when absent (e.g. some ARM kernels) — the UI then shows just `32 cores`. |

Config: `ProcTopN` default `8 → 25` in `internal/config/config.go`.

**Every other value in the UI is either already in the snapshot or computed client-side**
(peaks, max core, free memory, device sort order). No new per-tick syscalls.

## Files touched

| File | Change |
|---|---|
| `web/index.html` | Rewritten — new topbar, 2-column grid, 7 cards |
| `web/style.css` | Rewritten — light palette, grid, card anatomy |
| `web/app.js` | Rewritten render logic; keeps `esc()`, nil-slice coalescing, WS reconnect |
| `web/chart.js` | Gridline colour from CSS var; per-series colours; stretch-safe rendering |
| `internal/model/snapshot.go` | +`Host.OS`, +`Host.Kernel`, +`Net.Ifaces`, +`CPU.Model` |
| `internal/collect/collector.go` | Populate OS/Kernel (startup-cached) |
| `internal/collect/net.go` | Return interface names |
| `internal/collect/cpu.go` | Read CPU model name |
| `internal/config/config.go` | `ProcTopN` 8 → 25 |
| `internal/desktop/window.go` | `gtk_window_set_geometry_hints` min 1100×780 |

## Testing

- **Collector unit tests** follow the existing pattern in `internal/collect/*_test.go`:
  parse functions take a reader or a fixture directory, so `ParseNetDev` gains a case
  asserting the returned names exclude `lo`/`docker0`/`veth*`; `/proc/cpuinfo` and
  `os-release` parsers get fixture-based tests including the **absent/malformed** cases
  that must yield an empty string rather than an error.
- **Geometry:** `internal/desktop/geometry_test.go` already exists; extend it for the
  minimum-size constants. The GTK call itself stays covered by the existing smoke test.
- **Empty states** must be exercised by hand, since they depend on the host: no GPU
  (card removed), `cpu.temp === 0` (row hidden), `gpu.fan === -1` (row hidden).

### Visual verification loop (required before the work is called done)

Building to spec is not the same as looking right. After the implementation runs, the web
app is started against **real live data** and the UI is reviewed **in a browser**, then
corrected — repeating until it holds up.

Mechanism (verified working on this machine):

```bash
google-chrome --headless --disable-gpu --hide-scrollbars \
  --window-size=<W>,<H> --screenshot=<out>.png http://localhost:8090/
```

The screenshot is then **read back and critiqued visually**, not just diffed. Each round:

1. Serve the app on `:8090` with live metrics (never bind 8080 — filebrowser owns it).
2. Capture at three viewports: **1440×863** (desktop default, usable height after GTK
   chrome), **1100×780** (the enforced floor), **1920×1080**.
3. Review each capture against the checks below, plus general UI/UX judgement — spacing,
   alignment, visual weight, whether the five cards actually read as a set.
4. Fix what's wrong; re-capture. Repeat until a round produces no findings.

Hard checks, any of which fails the round:

- No scrollbar at any of the three viewports.
- Left and right column bottoms align exactly.
- All five plot areas start and end at identical x-coordinates.
- Top Processes and Filesystems row counts adapt to height, with no clipped final row.
- Gridlines are visible against white (regression guard for the hardcoded
  `rgba(255,255,255,0.06)`).
- No value on screen is a placeholder, a dash where a number was promised, or a
  duplicate of a neighbouring row.

Headless Chrome renders the same WebKit-adjacent light theme the desktop shell shows, but
it is not WebKitGTK. So the final round is also **eyeballed once in the real desktop
window**, which is the only way to confirm the GTK minimum-size hint and the chrome-height
assumption behind 1440×863.

## Risks

- **The 32-line CPU chart is a per-frame cost.** 32 paths × 60 points, redrawn once per
  second on a canvas that is only ~600×120 CSS px. Expected to be negligible, but it is
  32× the current CPU chart's work and should be watched during the visual verification
  loop. If it does bite, the mitigation is to thin the lines (draw every other core) rather
  than to abandon the design the user chose.
- **Auto-fit runs after layout and mutates the DOM**, which re-triggers layout. It must run
  once per render and be guarded against feedback loops (the `while` loop only ever deletes,
  so it terminates).
- **`PROC_TOP_N=25` grows every tick payload** by ~17 process entries. At 1/sec over
  loopback and LAN this is negligible, but it is a real change to the wire format's size.
