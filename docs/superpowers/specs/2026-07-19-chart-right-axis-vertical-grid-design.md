# Chart: Y-scale labels on the right + vertical gridlines

**Date:** 2026-07-19
**Status:** Approved

## Goal

Make the time-series charts read like Ubuntu's default GNOME System Monitor:

1. Move the Y-scale labels (`100% / 50% / 0%`, and the auto-scaled KiB/s labels)
   from the **left** of each plot to the **right**.
2. Add **vertical gridlines** — the charts currently have horizontal gridlines only.

Chosen treatment (option **B**, "most Ubuntu-faithful"): vertical gridlines every
10 s and a time label at every 10 s mark.

## Design

Localized to the web frontend. Newest sample already sits at the right edge, so no
data-direction change is needed. Applies to all five charts (CPU, Memory, GPU,
Network, Disk) and therefore to both the web (`:8090`) and desktop shells, which
share `web/`.

### `web/chart.js` — vertical gridlines
In `render()`, after the existing horizontal gridline loop, draw a 6-column grid:
5 interior vertical lines at `x = w * g / 6` for `g = 1..5`, using the same
`--grid` stroke colour and `lineWidth = 1`. Horizontal gridlines are unchanged
(3 interior lines at quarters).

### `web/index.html` — gutter side + time labels (all 5 chart cards)
- Move `<div class="gut">` to **after** `<canvas>` inside `.chartrow` so it renders
  to the right of the plot.
- Expand `.xax` from 3 labels to 7: `60s · 50 · 40 · 30 · 20 · 10 · now`. With flex
  `space-between` and 7 items the interior labels land under the 5 vertical
  gridlines; `now` (newest) stays at the right edge.
- Network/Disk gutters keep their `— / — / 0` placeholders (filled at runtime),
  now on the right.

### `web/style.css` — flip sides
- `.gut`: `text-align:right; padding-right:6px` → `text-align:left; padding-left:6px`.
- `.xax`: `margin-left:var(--gutter)` → `margin-right:var(--gutter)`.

### `web/app.js` — no change
`setRateGutter()` writes top/middle/bottom children in that order; the gutter's
vertical order is unchanged when it moves to the right, so the auto-scaled
Network/Disk rate labels appear correctly on the right with no code change.

## Out of scope
- Number of `%` labels stays 3 (`100/50/0`).
- Horizontal gridlines stay at quarters (25/50/75). Not aligning them 1:1 with the
  `%` labels; that would be a separate change.

## Verification
Run the web server (`:8090`), load the page, confirm on real data: `%` and KiB/s
labels sit on the right; 5 vertical + 3 horizontal gridlines; 7 time labels with
`now` at the right; no horizontal overflow when narrowing the window.
