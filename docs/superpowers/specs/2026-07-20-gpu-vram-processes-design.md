# GPU VRAM processes card — design

Date: 2026-07-20

## Problem

The GPU card shows aggregate VRAM (`8.0 / 12 GiB`) but not *who* is holding it.
`nvidia-smi` has this in its Processes table; the app does not. When VRAM fills
up there is no way to tell from the app which process to kill.

## Scope

Add a third card to the right column, between **Top Processes** and
**Filesystems**, listing the processes holding VRAM, sorted by VRAM descending.
Add a PID column to **Top Processes** at the same time.

Out of scope: killing processes from the UI, per-process GPU utilisation
(NVML exposes it only through the accounting API, which is off by default),
per-GPU grouping when several GPUs are present.

## Layout

Chosen from side-by-side browser mockups (option B, then variant E for column
order). Mockups live in `.superpowers/brainstorm/*/content/`.

Right column flex ratios: `#card-proc: 3` · `#card-gpuproc: 1.5` · `#card-fs: 1`.
At the 1100x780 floor that is roughly 13 / 7 / 3 rows; `autoFit()` trims from the
bottom as it already does.

Both tables put PID first, as a fixed 52px left column in `--sub` colour with
tabular numerals, so the PIDs line up and can be read off for `kill`:

| Card | Columns |
|---|---|
| Top Processes | `PID` · `Process` · `CPU` · `Mem` |
| GPU Processes | `PID` · `Process` · `Type` · `VRAM` |

`Memory` is abbreviated to `Mem` in Top Processes to make room for PID.
Card subtitle for the new card: `by VRAM · <used> / <total>`, summed over all GPUs.

## Data collection

NVML exposes running processes through two calls, one per context type:
`DeviceGetComputeRunningProcesses` and `DeviceGetGraphicsRunningProcesses`. Both
return `ProcessInfo{Pid, UsedGpuMemory}` — a PID and a byte count, no name.

The name must come from `<HostProc>/<pid>/comm`, and `HostProc` belongs to
`config`, which the GPU reader does not see. So `GPUReader` returns raw samples
and the collector resolves names:

```go
type GPUProcSample struct {
    PID      int
    VRAM     uint64
    Compute  bool // from DeviceGetComputeRunningProcesses
    Graphics bool // from DeviceGetGraphicsRunningProcesses
}
```

This keeps the NVML layer free of filesystem concerns and makes the merge logic a
pure function that tests without a GPU.

Collector responsibilities:

- **Merge by PID.** A PID in both lists becomes `Type: "C+G"`. A PID on several
  GPUs has its VRAM summed — the table is one flat list across all GPUs.
- **Guard `VALUE_NOT_AVAILABLE`.** NVML returns `0xFFFFFFFFFFFFFFFF` for
  `UsedGpuMemory` in some configurations (notably MIG). Treat it as 0 rather
  than rendering 16 EiB.
- **Resolve names** from `<HostProc>/<pid>/comm`. A process that exits between
  the NVML call and the read is normal — fall back to an empty name rather than
  dropping the row, so the VRAM it holds is still accounted for.
- **Sort** by VRAM descending, ties broken by PID ascending, so row order does
  not jitter between ticks.
- **Cap** at `cfg.ProcTopN` for payload safety. `autoFit()` trims further to fit
  the window.

## Model

```go
type Snapshot struct {
    ...
    GPUProc []GPUProcInfo `json:"gpu_proc"`
}

type GPUProcInfo struct {
    PID  int    `json:"pid"`
    Name string `json:"name"`
    Type string `json:"type"` // "C" | "G" | "C+G"
    VRAM uint64 `json:"vram"` // bytes
}

type ProcInfo struct {
    PID  int `json:"pid"` // new; ScanProcs already carries it
    Name string `json:"name"`
    ...
}
```

`GPUProc` sits at the top level of the snapshot rather than nested inside
`GPUInfo`, because the card presents one merged list rather than a list per GPU.

## Frontend

- `index.html` — new `#card-gpuproc` between `#card-proc` and `#card-fs`, with
  `data-fit` so `autoFit()` trims it. Both table headers updated for PID.
- `style.css` — the three flex ratios above, plus `.tbl .pidl` (52px, left
  aligned, `--sub`, tabular numerals) and `.tbl .tag` for the C/G marker.
- `cards.js` — new `renderGPUProc()`; `renderProc()` gains the PID column.
- `app.js` — coalesce `s.gpu_proc` to `[]`; when there is no GPU, remove
  `#card-gpuproc` alongside `#card-gpu`, so no permanently empty table remains.

### autoFit generalisation

`autoFit()` currently hardcodes `wrap.dataset.fit === 'fs'` to write the
"+N mount khác" note. The new card needs the same truncation note, so the note
target moves to a `data-note="<element id>"` attribute on the wrapper. Same
mechanism, no table name baked into the function.

### Empty states

| Condition | Result |
|---|---|
| No GPU / NVML unavailable | Card removed from the DOM, as `#card-gpu` already is |
| GPU present, no processes | Card stays, table empty, note reads "không có tiến trình dùng GPU" |
| Rows trimmed to fit | Note reads "+N tiến trình khác" |

## Docker

`docker-compose.yml` gains `pid: host`. Without a shared PID namespace NVML in a
container cannot enumerate processes — the usual "no running processes found"
from `nvidia-smi` inside Docker. The desktop head (the real app; the web head
exists for browser debugging) is unaffected either way.

## Testing

- `gpu_test.go` — table-driven tests of the merge function: PID in both lists →
  `C+G`; same PID on two GPUs → VRAM summed; `VALUE_NOT_AVAILABLE` → 0; sort
  order including the PID tie-break.
- Name resolution against a temp directory, including a PID whose directory does
  not exist (process exited mid-tick).
- `collector_test.go` — `ProcInfo.PID` is populated.

No test covers live NVML: the collector tests use the existing `fakeGPU` in
`collector_test.go`, which gains the new `GPUReader` method along with the real
reader and the nop reader.
