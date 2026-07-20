# GPU VRAM Processes Card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "GPU Processes" card to the right column — between Top Processes and Filesystems — listing the processes holding VRAM sorted descending, and add a PID column to Top Processes.

**Architecture:** NVML reports GPU processes as `(pid, bytes)` with no name, through two calls (compute contexts and graphics contexts). `GPUReader` returns those as raw samples; a pure `MergeGPUProcs` function folds them into one list per PID (VRAM summed across GPUs, `C`/`G`/`C+G` type); the collector resolves names from `<HostProc>/<pid>/comm`. The frontend renders a third right-column table that the existing `autoFit()` trims to the window height.

**Tech Stack:** Go 1.23, `github.com/NVIDIA/go-nvml v0.13.3-1`, vanilla JS/CSS frontend (no build step, no JS test runner).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-20-gpu-vram-processes-design.md`.
- `go` is not on the default PATH and CGO is required. **Always go through `make`** (it exports `PATH` and `CGO_ENABLED=1`), never a bare `go build`/`go test`.
- Full test suite: `make test`.
- Two form factors share `web/`: the Docker/native web head and the GTK desktop app. Any frontend change must work in both.
- **Never bind or kill port 8080** — filebrowser owns it. Browser debugging uses `make dev` on `:8091`.
- `web/` assets are `go:embed`'d into the desktop binary — a frontend change is invisible in the desktop app until it is rebuilt.
- Right-column flex ratios are fixed by the approved mockup: `#card-proc: 3`, `#card-gpuproc: 1.5`, `#card-fs: 1`.
- Existing user-facing note strings in the app are Vietnamese (`+2 mount khác`) — match that.

---

### Task 1: Merge and sort GPU process samples

The pure logic, testable without a GPU: fold per-device NVML samples into one row per PID.

**Files:**
- Modify: `internal/model/snapshot.go` (add `GPUProcInfo`)
- Modify: `internal/collect/gpu.go` (add `GPUProcSample`, `MergeGPUProcs`)
- Create: `internal/collect/gpu_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `model.GPUProcInfo{PID int; Name string; Type string; VRAM uint64}`
  - `collect.GPUProcSample{PID int; VRAM uint64; Compute bool; Graphics bool}`
  - `collect.MergeGPUProcs(samples []GPUProcSample) []model.GPUProcInfo`

- [ ] **Step 1: Add the model type**

Append to `internal/model/snapshot.go`, after `GPUInfo`:

```go
// GPUProcInfo is one process holding VRAM, merged across every GPU it runs on.
type GPUProcInfo struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	Type string `json:"type"` // "C" (compute), "G" (graphics), or "C+G"
	VRAM uint64 `json:"vram"` // bytes
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/collect/gpu_test.go`:

```go
package collect

import "testing"

func TestMergeGPUProcs(t *testing.T) {
	in := []GPUProcSample{
		{PID: 100, VRAM: 200 << 20, Graphics: true},
		{PID: 200, VRAM: 3 << 30, Compute: true},
		{PID: 100, VRAM: 100 << 20, Compute: true}, // same PID, other context type
		{PID: 200, VRAM: 1 << 30, Compute: true},   // same PID, second GPU
	}
	got := MergeGPUProcs(in)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2, got %+v", len(got), got)
	}
	// Sorted by VRAM descending: 200 holds 4 GiB summed across two GPUs.
	if got[0].PID != 200 || got[0].VRAM != 4<<30 || got[0].Type != "C" {
		t.Errorf("got[0]=%+v", got[0])
	}
	// 100 appears in both lists, so it holds both context types.
	if got[1].PID != 100 || got[1].VRAM != 300<<20 || got[1].Type != "C+G" {
		t.Errorf("got[1]=%+v", got[1])
	}
}

func TestMergeGPUProcsValueNotAvailable(t *testing.T) {
	// NVML reports 0xFFFFFFFFFFFFFFFF when per-process VRAM is unavailable
	// (notably under MIG). Rendering it verbatim would show 16 EiB.
	got := MergeGPUProcs([]GPUProcSample{{PID: 7, VRAM: ^uint64(0), Compute: true}})
	if len(got) != 1 || got[0].VRAM != 0 {
		t.Errorf("got=%+v, want one row with VRAM 0", got)
	}
}

func TestMergeGPUProcsTieBreakByPID(t *testing.T) {
	// Equal VRAM must order by PID ascending, so rows do not swap between ticks.
	got := MergeGPUProcs([]GPUProcSample{
		{PID: 9, VRAM: 1 << 20, Compute: true},
		{PID: 3, VRAM: 1 << 20, Compute: true},
	})
	if len(got) != 2 || got[0].PID != 3 || got[1].PID != 9 {
		t.Errorf("got=%+v, want PIDs [3 9]", got)
	}
}

func TestMergeGPUProcsEmpty(t *testing.T) {
	if got := MergeGPUProcs(nil); len(got) != 0 {
		t.Errorf("got=%+v, want empty", got)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — `undefined: GPUProcSample`, `undefined: MergeGPUProcs`.

- [ ] **Step 4: Write the implementation**

In `internal/collect/gpu.go`, add `"sort"` to the import block so it reads:

```go
import (
	"sort"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"system-monitor/internal/model"
)
```

Then append at the end of the file:

```go
// GPUProcSample is one process holding VRAM on one GPU, as NVML reports it: a
// PID, a byte count, and which context type it was listed under. NVML has no
// process names, so the collector resolves those from procfs.
type GPUProcSample struct {
	PID      int
	VRAM     uint64
	Compute  bool
	Graphics bool
}

// vramUnavailable is NVML's NVML_VALUE_NOT_AVAILABLE sentinel for
// UsedGpuMemory, returned in configurations where per-process VRAM cannot be
// attributed (notably MIG).
const vramUnavailable = ^uint64(0)

// MergeGPUProcs folds per-device samples into one row per PID: VRAM sums across
// devices, and a PID listed under both context types becomes "C+G". Sorted by
// VRAM descending, ties by PID ascending so row order is stable between ticks.
func MergeGPUProcs(samples []GPUProcSample) []model.GPUProcInfo {
	type acc struct {
		vram              uint64
		compute, graphics bool
	}
	byPID := map[int]*acc{}
	var order []int
	for _, s := range samples {
		a, ok := byPID[s.PID]
		if !ok {
			a = &acc{}
			byPID[s.PID] = a
			order = append(order, s.PID)
		}
		if s.VRAM != vramUnavailable {
			a.vram += s.VRAM
		}
		a.compute = a.compute || s.Compute
		a.graphics = a.graphics || s.Graphics
	}
	out := make([]model.GPUProcInfo, 0, len(order))
	for _, pid := range order {
		a := byPID[pid]
		t := ""
		switch {
		case a.compute && a.graphics:
			t = "C+G"
		case a.compute:
			t = "C"
		case a.graphics:
			t = "G"
		}
		out = append(out, model.GPUProcInfo{PID: pid, Type: t, VRAM: a.vram})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VRAM != out[j].VRAM {
			return out[i].VRAM > out[j].VRAM
		}
		return out[i].PID < out[j].PID
	})
	return out
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `make test`
Expected: PASS — `ok  	system-monitor/internal/collect`, and every other package still ok.

- [ ] **Step 6: Commit**

```bash
git add internal/model/snapshot.go internal/collect/gpu.go internal/collect/gpu_test.go
git commit -m "feat(gpu): merge NVML per-device process samples by PID"
```

---

### Task 2: Read GPU processes from NVML and name them

Wires the real NVML calls in, resolves names from procfs, and publishes the list on the snapshot.

**Files:**
- Modify: `internal/collect/gpu.go` (interface + both readers)
- Modify: `internal/collect/proc.go` (add `ReadProcName`)
- Modify: `internal/collect/collector.go` (add `gpuProcs`, call it in `Tick`)
- Modify: `internal/model/snapshot.go` (add the `GPUProc` field)
- Modify: `internal/collect/proc_test.go`, `internal/collect/collector_test.go`
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: `GPUProcSample`, `MergeGPUProcs`, `model.GPUProcInfo` from Task 1.
- Produces:
  - `GPUReader` interface gains `ReadProcs() []GPUProcSample` — implemented by `nvmlGPU`, `nopGPU`, and the test `fakeGPU`.
  - `collect.ReadProcName(hostProc string, pid int) string`
  - `model.Snapshot.GPUProc []GPUProcInfo`, JSON key `gpu_proc` (Task 4 consumes it).

- [ ] **Step 1: Write the failing test for name resolution**

In `internal/collect/proc_test.go`, replace the import line `import "testing"` with:

```go
import (
	"os"
	"path/filepath"
	"testing"
)
```

Then append:

```go
func TestReadProcName(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "77"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "77", "comm"), []byte("python3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := ReadProcName(root, 77); got != "python3" {
		t.Errorf("name = %q, want %q", got, "python3")
	}
	// A process that exited between the NVML sample and this read is a normal
	// race, not an error: the caller still shows the row it sampled.
	if got := ReadProcName(root, 78); got != "" {
		t.Errorf("name = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — `undefined: ReadProcName`.

- [ ] **Step 3: Implement name resolution**

Append to `internal/collect/proc.go` (its import block already has everything needed):

```go
// ReadProcName reads a process's comm name. Returns "" when the process has
// exited between the caller sampling its PID and this read.
func ReadProcName(hostProc string, pid int) string {
	b, err := os.ReadFile(filepath.Join(hostProc, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test`
Expected: `TestReadProcName` passes. Other packages still ok.

- [ ] **Step 5: Add the snapshot field**

In `internal/model/snapshot.go`, add the field to `Snapshot` after `Proc`:

```go
type Snapshot struct {
	Host    HostInfo      `json:"host"`
	CPU     CPUInfo       `json:"cpu"`
	Mem     MemInfo       `json:"mem"`
	Net     NetInfo       `json:"net"`
	Disk    DiskInfo      `json:"disk"`
	GPU     []GPUInfo     `json:"gpu"`
	FS      []FSInfo      `json:"fs"`
	Proc    []ProcInfo    `json:"proc"`
	GPUProc []GPUProcInfo `json:"gpu_proc"`
}
```

- [ ] **Step 6: Write the failing collector test**

In `internal/collect/collector_test.go`, add the new method to `fakeGPU` right after its `Read` method:

```go
func (fakeGPU) ReadProcs() []GPUProcSample {
	return []GPUProcSample{{PID: 42, VRAM: 512 << 20, Compute: true}}
}
```

In `TestCollectorTick`, add this line to the `writeFile` block (after the `uptime` line):

```go
	writeFile(t, filepath.Join(proc, "42/comm"), "trainer\n")
```

and add this assertion after the existing `snap.GPU` check:

```go
	if len(snap.GPUProc) != 1 || snap.GPUProc[0].Name != "trainer" ||
		snap.GPUProc[0].Type != "C" || snap.GPUProc[0].VRAM != 512<<20 {
		t.Errorf("gpu proc = %+v", snap.GPUProc)
	}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — `fakeGPU` now has a method the interface lacks, so the assertion on `snap.GPUProc` fails with `gpu proc = []` (the collector never fills it).

- [ ] **Step 8: Add ReadProcs to the GPUReader interface and both readers**

In `internal/collect/gpu.go`, change the interface and the nop reader:

```go
type GPUReader interface {
	Read() []model.GPUInfo
	ReadProcs() []GPUProcSample
	Close()
}

type nopGPU struct{}

func (nopGPU) Read() []model.GPUInfo      { return nil }
func (nopGPU) ReadProcs() []GPUProcSample { return nil }
func (nopGPU) Close()                     {}
```

Then add the NVML implementation after the existing `func (g *nvmlGPU) Read()`:

```go
// ReadProcs lists every process holding VRAM on every device. NVML splits these
// across two calls by context type, and a process using both appears in both —
// MergeGPUProcs folds that back together.
func (g *nvmlGPU) ReadProcs() []GPUProcSample {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil
	}
	var out []GPUProcSample
	for i := 0; i < count; i++ {
		d, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}
		if ps, ret := d.GetComputeRunningProcesses(); ret == nvml.SUCCESS {
			for _, p := range ps {
				out = append(out, GPUProcSample{PID: int(p.Pid), VRAM: p.UsedGpuMemory, Compute: true})
			}
		}
		if ps, ret := d.GetGraphicsRunningProcesses(); ret == nvml.SUCCESS {
			for _, p := range ps {
				out = append(out, GPUProcSample{PID: int(p.Pid), VRAM: p.UsedGpuMemory, Graphics: true})
			}
		}
	}
	return out
}
```

- [ ] **Step 9: Wire it into the collector**

In `internal/collect/collector.go`, add this line to `Tick` right after `snap.Proc = c.procs(dt)`:

```go
	snap.GPUProc = c.gpuProcs()
```

and add the method after `func (c *Collector) procs(dt float64) []model.ProcInfo`:

```go
// gpuProcs merges NVML's per-device samples, trims to the same cap as the
// process table, then resolves names — in that order, so a busy GPU does not
// cost one procfs read per dropped row.
func (c *Collector) gpuProcs() []model.GPUProcInfo {
	out := MergeGPUProcs(c.gpu.ReadProcs())
	if len(out) > c.cfg.ProcTopN {
		out = out[:c.cfg.ProcTopN]
	}
	for i := range out {
		out[i].Name = ReadProcName(c.cfg.HostProc, out[i].PID)
	}
	return out
}
```

- [ ] **Step 10: Run the tests to verify they pass**

Run: `make test`
Expected: PASS across all packages.

- [ ] **Step 11: Give the containerized web head a shared PID namespace**

In `docker-compose.yml`, add `pid: host` immediately after the `network_mode: host` line:

```yaml
    network_mode: host
    pid: host
```

Without a shared PID namespace NVML in a container enumerates no processes — the familiar "no running processes found" from `nvidia-smi` inside Docker. The desktop head is unaffected either way.

- [ ] **Step 12: Verify against the real GPU**

Run `make dev` in one shell (it serves the native web head on `:8091`), then in another:

```bash
curl -s localhost:8091/api/snapshot | python3 -m json.tool | grep -A 5 gpu_proc
nvidia-smi --query-compute-apps=pid,used_memory --format=csv
```

Expected: `gpu_proc` rows whose PIDs and byte counts line up with `nvidia-smi` (its Processes table reports MiB, the snapshot reports bytes), each with a non-empty `name` and a `type` of `C`, `G`, or `C+G`. Stop `make dev` afterwards.

Note `--query-compute-apps` lists only compute contexts, so the graphics-only rows (Xorg, gnome-shell, chrome) appear in `gpu_proc` but not in that CSV — compare those against the full `nvidia-smi` table.

- [ ] **Step 13: Commit**

```bash
git add internal/collect/gpu.go internal/collect/proc.go internal/collect/collector.go \
        internal/model/snapshot.go internal/collect/proc_test.go \
        internal/collect/collector_test.go docker-compose.yml
git commit -m "feat(gpu): collect per-process VRAM usage from NVML"
```

---

### Task 3: Expose PID on process rows

`ScanProcs` already carries the PID; `ProcInfo` just never published it.

**Files:**
- Modify: `internal/model/snapshot.go`
- Modify: `internal/collect/collector.go:164`
- Modify: `internal/collect/collector_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `model.ProcInfo.PID int`, JSON key `pid` (Task 4 consumes it).

- [ ] **Step 1: Write the failing test**

In `internal/collect/collector_test.go`, inside `TestCollectorTickDeltas`, replace the `found` loop with:

```go
	var found bool
	for _, p := range snap.Proc {
		if p.Name == "testproc" {
			found = true
			if p.CPU != 100 {
				t.Errorf("proc testproc CPU = %v, want 100", p.CPU)
			}
			if p.PID != 1234 {
				t.Errorf("proc testproc PID = %d, want 1234", p.PID)
			}
		}
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — `p.PID undefined (type model.ProcInfo has no field or method PID)`.

- [ ] **Step 3: Add the field and populate it**

In `internal/model/snapshot.go`:

```go
type ProcInfo struct {
	PID  int     `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"` // percent of one core (can exceed 100)
	RSS  uint64  `json:"rss"` // bytes
}
```

In `internal/collect/collector.go`, in `procs`, change the append to:

```go
		out = append(out, model.ProcInfo{PID: s.PID, Name: s.Name, CPU: cpu, RSS: s.RSS})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make test`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add internal/model/snapshot.go internal/collect/collector.go internal/collect/collector_test.go
git commit -m "feat(proc): publish PID on process rows"
```

---

### Task 4: Render the card and the PID columns

The frontend. There is no JS test runner in this repo — verification is the browser at `:8091`, then the desktop app.

**Files:**
- Modify: `web/index.html` (new card + both table headers)
- Modify: `web/style.css` (flex ratios + three new rules)
- Modify: `web/cards.js` (`renderGPUProc`, `renderProc`, generalised `autoFit`)
- Modify: `web/app.js` (coalesce, remove card when no GPU, call the renderer)

**Interfaces:**
- Consumes: `gpu_proc` rows `{pid, name, type, vram}` from Task 2; `proc` rows gain `pid` from Task 3.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Add the card and update both table headers**

In `web/index.html`, replace the Top Processes `<thead>` line:

```html
            <thead><tr><th class="pidl">PID</th><th>Process</th><th class="n">CPU</th><th class="n">Mem</th></tr></thead>
```

Give the Filesystems wrapper a note target — replace its `<div class="tw" data-fit="fs">` with:

```html
        <div class="tw" data-fit="fs" data-note="fsNote">
```

and its note element with:

```html
        <div class="note" id="fsNote" data-unit="mount"></div>
```

Then insert this whole section between `</section>` of `#card-proc` and `<section class="card" id="card-fs">`:

```html
      <section class="card" id="card-gpuproc">
        <div class="ch">
          <div class="cico i-green">◫</div>
          <div><div class="ct">GPU Processes</div><div class="cs" id="subGpuProc">by VRAM</div></div>
        </div>
        <div class="tw" data-fit="gpuproc" data-note="gpuProcNote">
          <table class="tbl">
            <thead><tr><th class="pidl">PID</th><th>Process</th><th class="n">Type</th><th class="n">VRAM</th></tr></thead>
            <tbody id="gpuProcBody"></tbody>
          </table>
        </div>
        <div class="note" id="gpuProcNote" data-unit="tiến trình"></div>
      </section>
```

- [ ] **Step 2: Add the styles**

In `web/style.css`, replace the two flex lines under `#colLeft > .card { flex: 1; }`:

```css
#card-proc { flex: 3; }
#card-gpuproc { flex: 1.5; }
#card-fs { flex: 1; }
```

Then, in the "right-column tables" block, add after the `.tbl td.nm` rule:

```css
/* PID leads both right-column tables as a fixed left column, so the numbers
   line up down the page and can be read off for kill(1). --sub keeps them from
   competing with the process name beside them. */
.tbl .pidl {
  width: 52px; padding-right: 10px; text-align: left; color: var(--sub);
  font-variant-numeric: tabular-nums; white-space: nowrap;
}
/* NVML's context-type marker: C, G, or C+G. Sized like a .cap so it reads as a
   label rather than a value. */
.tbl .tag { font-size: 8.5px; font-weight: 700; color: var(--sub); }
/* The "no GPU process" state is a row, not a note: autoFit() rewrites notes on
   every tick and would wipe a message written there. */
.tbl td.empty { color: var(--sub); font-size: 10.5px; border-bottom: 0; }
```

- [ ] **Step 3: Generalise autoFit and add the renderer**

In `web/cards.js`, replace `renderProc` with:

```js
function renderProc(s) {
  // Already sorted by CPU descending server-side. No per-process icons: they
  // carried no information the name doesn't.
  $('procBody').innerHTML = s.proc.map(p =>
    `<tr><td class="pidl">${p.pid}</td>` +
    `<td class="nm" title="${esc(p.name)}">${esc(p.name)}</td>` +
    `<td class="n">${p.cpu.toFixed(0)}%</td>` +
    `<td class="n">${fmtBytes(p.rss)}</td></tr>`).join('');
}
```

Add after it:

```js
// Only called when hasGPU; the card is removed from the DOM otherwise. Rows are
// already merged across GPUs and sorted by VRAM descending server-side.
function renderGPUProc(s) {
  const used = s.gpu.reduce((a, g) => a + g.mem_used, 0);
  const total = s.gpu.reduce((a, g) => a + g.mem_total, 0);
  $('subGpuProc').textContent = 'by VRAM · ' + fmtBytes(used) + ' / ' + fmtBytes(total);
  // A process that exits mid-tick still holds the VRAM NVML reported, so the
  // row stays and only its name falls back to a dash.
  $('gpuProcBody').innerHTML = s.gpu_proc.length
    ? s.gpu_proc.map(p =>
        `<tr><td class="pidl">${p.pid}</td>` +
        `<td class="nm" title="${esc(p.name)}">${esc(p.name || '—')}</td>` +
        `<td class="n tag">${esc(p.type)}</td>` +
        `<td class="n">${fmtBytes(p.vram)}</td></tr>`).join('')
    : '<tr><td class="empty" colspan="4">không có tiến trình dùng GPU</td></tr>';
}
```

Replace `autoFit` with:

```js
// Trim rows from the bottom until each table fits its wrapper, so the right
// column adapts to the window height without ever scrolling. The loop only
// ever deletes, so it terminates. Every list is pre-sorted, so the rows that
// survive are the ones worth keeping.
function autoFit() {
  document.querySelectorAll('[data-fit]').forEach(wrap => {
    const tb = wrap.querySelector('tbody');
    if (!tb) return;
    const total = tb.rows.length;
    while (tb.rows.length > 1 && wrap.scrollHeight > wrap.clientHeight) {
      tb.deleteRow(tb.rows.length - 1);
    }
    // Never truncate silently. A .note reserves its height even when empty, so
    // writing into it cannot re-overflow the table just trimmed to fit.
    const note = wrap.dataset.note && $(wrap.dataset.note);
    if (note) {
      const hidden = total - tb.rows.length;
      note.textContent = hidden > 0 ? `+${hidden} ${note.dataset.unit} khác` : '';
    }
  });
}
```

- [ ] **Step 4: Wire it into the app**

In `web/app.js`, in `initCharts`, replace the no-GPU line:

```js
  // No GPU: drop both GPU cards entirely rather than render tables full of
  // dashes. The remaining cards take their space via flex.
  if (!hasGPU) {
    ['card-gpu', 'card-gpuproc'].forEach(id => { const c = $(id); if (c) c.remove(); });
  }
```

In `applySnap`, add the coalesce line after `s.proc = s.proc || [];`:

```js
  s.gpu_proc = s.gpu_proc || [];
```

and replace the GPU render line with:

```js
  if (hasGPU && s.gpu.length) { renderGPU(s); renderGPUProc(s); }
```

- [ ] **Step 5: Verify in the browser**

Run `make dev`, then open `http://localhost:8091` with the browser-use skill and screenshot the right column.
Expected: three cards stacked — Top Processes (PID first), GPU Processes (PID · Process · Type · VRAM, rows descending by VRAM), Filesystems. PIDs and VRAM match `nvidia-smi`. Resize the window down toward 1100x780 and confirm no table scrolls and the notes read `+N tiến trình khác` / `+N mount khác`.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/style.css web/cards.js web/app.js
git commit -m "feat(ui): GPU processes card and PID columns"
```

- [ ] **Step 7: Deploy to the desktop app**

`web/` assets are embedded in the binary, so the desktop app shows nothing new until it is rebuilt. Use the repo's deploy skill:

```bash
bash .claude/skills/deploy/deploy.sh
```

Expected summary: `desktop` running, `:8090` and `:8091` off. Confirm the new card is in the desktop window.
