# UI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the system monitor frontend to match `docs/new_design_system_monitor.png` with one uniform card anatomy across all five left cards, a light theme, and no scrolling — backed by four small collector additions so nothing on screen is fabricated.

**Architecture:** Four TDD'd Go collector changes land first (OS, Kernel, network interface names, CPU model), because the frontend renders them. Then the frontend is rebuilt incrementally — shell, then one card group per task — so every task leaves a runnable app that can be screenshotted. Symmetry is structural, not hand-tuned: a fixed 54px Y gutter and a fixed 210px stats column mean all five plot areas start and end at the same x, and a single-row CSS grid makes both columns' heights equal by definition. The final task is a headless-Chrome review loop against live data.

**Tech Stack:** Go 1.23 (CGO for NVML + WebKitGTK), vanilla JS (no framework, no build step, plain `<script>` tags), canvas 2D charts, GTK3 + WebKitGTK, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-07-17-ui-redesign-design.md`

## Global Constraints

- **Build env:** `go` is not on the default PATH. Every Go command needs `export PATH=$PATH:/home/xuanlocserver/.local/go/bin` and `export CGO_ENABLED=1`.
- **Never bind host port 8080** — filebrowser owns it. The web app is `:8090`.
- **One `web/` directory, two form factors.** Every frontend change must work in both the browser and the WebKitGTK desktop window. No form-factor-specific CSS or JS.
- **Light theme only.** The dark palette is deleted, not kept behind a toggle.
- **No fabricated data.** Every value rendered must trace to a field in `internal/model/snapshot.go` or a client-side computation over the 60-point history. If a metric is unavailable, hide the row — never show a placeholder or a plausible-looking constant.
- **No scrollbars** at or above 1100×780, on the page or inside any card.
- **Exact palette** (defined once in `web/style.css`):
  `--bg:#f6f7f9` `--card:#ffffff` `--line:#e8eaee` `--track:#eef0f4` `--ink:#1a1d23` `--sub:#8b909a` `--blue:#2f7ff5` `--green:#22c55e` `--red:#ef4444` `--purple:#a855f7` `--cyan:#38bdf8` `--amber:#f59e0b`
- **Fixed layout metrics:** Y gutter `54px`, stats column `210px`, right column `400px`, minimum window `1100×780`.
- **Thresholds:** CPU core cell red **above 80%**; disk device bar red **above 75%**; filesystem bar red **above 90%**.
- **Commit after every task.**

---

### Task 1: Collector — host OS and kernel

The container's `/etc/os-release` is the *image's*, not the host's. `/` is bind-mounted at `/host/root` (see `docker-compose.yml`), so the read must go through `cfg.HostRoot` or the web app will report the wrong distro. Both values are immutable for the process lifetime, so they are read once in `New()` — not per tick.

**Files:**
- Create: `internal/collect/host.go`
- Create: `internal/collect/host_test.go`
- Modify: `internal/model/snapshot.go:17-21` (HostInfo)
- Modify: `internal/collect/collector.go:15-32` (Collector struct + New), `:57-74` (host)

**Interfaces:**
- Consumes: `config.Config.HostRoot`, `config.Config.HostProc` (existing).
- Produces: `collect.ParseOSRelease(io.Reader) string`, `collect.ReadOSName(hostRoot string) string`, `collect.ReadKernel(hostProc string) string`; JSON fields `host.os` and `host.kernel` (both `string`, `""` when unavailable).

- [ ] **Step 1: Write the failing tests**

Create `internal/collect/host_test.go`:

```go
package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	in := "NAME=\"Ubuntu\"\n" +
		"VERSION_ID=\"24.04\"\n" +
		"PRETTY_NAME=\"Ubuntu 24.04.4 LTS\"\n" +
		"ID=ubuntu\n"
	if got := ParseOSRelease(strings.NewReader(in)); got != "Ubuntu 24.04.4 LTS" {
		t.Errorf("got %q, want %q", got, "Ubuntu 24.04.4 LTS")
	}
}

func TestParseOSReleaseUnquoted(t *testing.T) {
	if got := ParseOSRelease(strings.NewReader("PRETTY_NAME=Alpine Linux v3.20\n")); got != "Alpine Linux v3.20" {
		t.Errorf("got %q", got)
	}
}

func TestParseOSReleaseMissingKey(t *testing.T) {
	if got := ParseOSRelease(strings.NewReader("NAME=\"Weird\"\nID=weird\n")); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadOSNameMissingFile(t *testing.T) {
	if got := ReadOSName(t.TempDir()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadKernel(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sys", "kernel"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "sys", "kernel", "osrelease")
	if err := os.WriteFile(p, []byte("6.17.0-40-generic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadKernel(dir); got != "6.17.0-40-generic" {
		t.Errorf("got %q, want %q", got, "6.17.0-40-generic")
	}
}

func TestReadKernelMissingFile(t *testing.T) {
	if got := ReadKernel(t.TempDir()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
go test ./internal/collect/ -run 'OSRelease|OSName|Kernel' -v
```

Expected: FAIL — `undefined: ParseOSRelease`, `undefined: ReadOSName`, `undefined: ReadKernel`.

- [ ] **Step 3: Write the implementation**

Create `internal/collect/host.go`:

```go
package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ParseOSRelease returns the PRETTY_NAME value from an os-release file, or ""
// if the key is absent. Values are optionally quoted; the quotes are stripped.
func ParseOSRelease(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
	}
	return ""
}

// ReadOSName reads the host distro's PRETTY_NAME. hostRoot is the prefix under
// which the host filesystem is visible ("" natively, "/host/root" in the
// container) so the web app reports the host's distro, not the image's.
// Returns "" when unavailable; the UI then omits the field.
func ReadOSName(hostRoot string) string {
	// The suffix must be absolute: filepath.Join("", "etc", "os-release") drops
	// the empty prefix and yields a RELATIVE "etc/os-release", which breaks the
	// desktop head (HostRoot == ""). Same pattern as fs.go's Join(hostRoot, m.Mountpoint).
	f, err := os.Open(filepath.Join(hostRoot, "/etc/os-release"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return ParseOSRelease(f)
}

// ReadKernel reads the running kernel release. /proc is the host's /proc in
// both form factors, so this is the host kernel either way.
func ReadKernel(hostProc string) string {
	b, err := os.ReadFile(filepath.Join(hostProc, "sys", "kernel", "osrelease"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/collect/ -run 'OSRelease|OSName|Kernel' -v
```

Expected: PASS (6 tests).

- [ ] **Step 5: Add the model fields**

In `internal/model/snapshot.go`, replace the `HostInfo` struct:

```go
type HostInfo struct {
	Name   string     `json:"name"`
	OS     string     `json:"os"`     // distro PRETTY_NAME; "" if unknown
	Kernel string     `json:"kernel"` // kernel release; "" if unknown
	Uptime int64      `json:"uptime"`
	Load   [3]float64 `json:"load"`
}
```

- [ ] **Step 6: Cache the values in the collector**

In `internal/collect/collector.go`, add two fields to the `Collector` struct, immediately after `disks []string`:

```go
	// Immutable for the process lifetime, so they are read once in New()
	// rather than on every tick.
	osName string
	kernel string
```

Replace `New` with:

```go
func New(cfg config.Config, gpu GPUReader) *Collector {
	disks, _ := ListDisks(cfg.HostSys, cfg.DiskExclude)
	return &Collector{
		cfg:      cfg,
		gpu:      gpu,
		disks:    disks,
		osName:   ReadOSName(cfg.HostRoot),
		kernel:   ReadKernel(cfg.HostProc),
		prevDisk: map[string]DiskCounters{},
		prevProc: map[int]uint64{},
	}
}
```

In `host()`, add the two assignments immediately after `h.Name = name`:

```go
	h.OS = c.osName
	h.Kernel = c.kernel
```

- [ ] **Step 7: Verify the whole package still passes**

```bash
go build ./... && go test ./internal/... 
```

Expected: PASS, no build errors.

- [ ] **Step 8: Commit**

```bash
git add internal/collect/host.go internal/collect/host_test.go \
        internal/collect/collector.go internal/model/snapshot.go
git commit -m "feat(collect): report host OS and kernel

Read once at startup, through HostRoot so the container reports the
host's distro rather than the image's."
```

---

### Task 2: Collector — network interface names

`ParseNetDev` currently sums rx/tx across physical interfaces and **throws the names away**. The Network card's subtitle needs them. The names are sorted so the subtitle is stable across ticks.

**Files:**
- Modify: `internal/collect/net.go:12-15` (NetCounters), `:30-53` (ParseNetDev)
- Modify: `internal/collect/net_test.go:8-18`
- Modify: `internal/model/snapshot.go:39-44` (NetInfo)
- Modify: `internal/collect/collector.go:94-106` (net)

**Interfaces:**
- Consumes: nothing new.
- Produces: `collect.NetCounters.Ifaces []string` (sorted, physical only); JSON field `net.ifaces` (`[]string`, may be `null` — the frontend coalesces).

- [ ] **Step 1: Write the failing tests**

Replace the whole body of `internal/collect/net_test.go`:

```go
package collect

import (
	"strings"
	"testing"
)

const netDevSample = "Inter-|   Receive                    |  Transmit\n" +
	" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets\n" +
	"    lo:  1000    10 0 0 0 0 0 0  1000 10 0 0 0 0 0 0\n" +
	"  eth0:  5000    50 0 0 0 0 0 0  2000 20 0 0 0 0 0 0\n" +
	" veth1:  9999    99 0 0 0 0 0 0  9999 99 0 0 0 0 0 0\n"

func TestParseNetDev(t *testing.T) {
	c := ParseNetDev(strings.NewReader(netDevSample))
	if c.RX != 5000 || c.TX != 2000 { // only eth0 counted
		t.Errorf("got RX=%d TX=%d, want 5000/2000", c.RX, c.TX)
	}
}

func TestParseNetDevIfaces(t *testing.T) {
	c := ParseNetDev(strings.NewReader(netDevSample))
	if len(c.Ifaces) != 1 || c.Ifaces[0] != "eth0" {
		t.Errorf("got %v, want [eth0] — lo and veth1 must be excluded", c.Ifaces)
	}
}

func TestParseNetDevIfacesSorted(t *testing.T) {
	in := " wlan0:  10 1 0 0 0 0 0 0  10 1 0 0 0 0 0 0\n" +
		"enp5s0:  20 2 0 0 0 0 0 0  20 2 0 0 0 0 0 0\n"
	c := ParseNetDev(strings.NewReader(in))
	if len(c.Ifaces) != 2 || c.Ifaces[0] != "enp5s0" || c.Ifaces[1] != "wlan0" {
		t.Errorf("got %v, want [enp5s0 wlan0] in sorted order", c.Ifaces)
	}
}

func TestParseNetDevNoPhysical(t *testing.T) {
	c := ParseNetDev(strings.NewReader("    lo:  1 1 0 0 0 0 0 0  1 1 0 0 0 0 0 0\n"))
	if len(c.Ifaces) != 0 {
		t.Errorf("got %v, want none", c.Ifaces)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
go test ./internal/collect/ -run TestParseNetDev -v
```

Expected: FAIL — `c.Ifaces undefined (type NetCounters has no field or method Ifaces)`.

- [ ] **Step 3: Write the implementation**

In `internal/collect/net.go`, add `"sort"` to the import block, then replace `NetCounters` and `ParseNetDev`:

```go
type NetCounters struct {
	RX     uint64
	TX     uint64
	Ifaces []string // names of the physical interfaces summed above, sorted
}

// ParseNetDev sums rx/tx bytes over physical interfaces and records their
// names. In /proc/net/dev the value after the interface colon has rx-bytes at
// index 0 and tx-bytes at index 8.
func ParseNetDev(r io.Reader) NetCounters {
	var c NetCounters
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || isVirtualIface(name) {
			continue
		}
		f := strings.Fields(line[colon+1:])
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		c.RX += rx
		c.TX += tx
		c.Ifaces = append(c.Ifaces, name)
	}
	// Sorted so the card subtitle does not reshuffle between ticks.
	sort.Strings(c.Ifaces)
	return c
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/collect/ -run TestParseNetDev -v
```

Expected: PASS (4 tests).

- [ ] **Step 5: Plumb it through the model and collector**

In `internal/model/snapshot.go`, replace `NetInfo`:

```go
type NetInfo struct {
	RX      uint64   `json:"rx"`
	TX      uint64   `json:"tx"`
	RXTotal uint64   `json:"rx_total"`
	TXTotal uint64   `json:"tx_total"`
	Ifaces  []string `json:"ifaces"` // physical interfaces being summed
}
```

In `internal/collect/collector.go`, in `net()`, replace the `n := ...` line:

```go
	n := model.NetInfo{RXTotal: cur.RX, TXTotal: cur.TX, Ifaces: cur.Ifaces}
```

- [ ] **Step 6: Verify the whole package still passes**

```bash
go build ./... && go test ./internal/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/collect/net.go internal/collect/net_test.go \
        internal/collect/collector.go internal/model/snapshot.go
git commit -m "feat(collect): report which network interfaces are summed

The Network card's subtitle names the real interfaces instead of the
invented 'enp5s0 - 1 Gb/s' the mockup showed. Same filter, same rates."
```

---

### Task 3: Collector — CPU model name

**Files:**
- Modify: `internal/collect/cpu.go` (add two functions at the end)
- Modify: `internal/collect/cpu_test.go` (append tests)
- Modify: `internal/model/snapshot.go:23-27` (CPUInfo)
- Modify: `internal/collect/collector.go` (Collector struct, New, Tick)

**Interfaces:**
- Consumes: `config.Config.HostProc`.
- Produces: `collect.ParseCPUModel(io.Reader) string`, `collect.ReadCPUModel(hostProc string) string`; JSON field `cpu.model` (`string`, `""` when absent).

- [ ] **Step 1: Write the failing tests**

Append to `internal/collect/cpu_test.go` (the file already imports `strings` and `testing`):

```go
func TestParseCPUModel(t *testing.T) {
	in := "processor\t: 0\n" +
		"vendor_id\t: GenuineIntel\n" +
		"model name\t: Intel(R) Core(TM) i9-14900K\n" +
		"cpu MHz\t\t: 3187.000\n" +
		"processor\t: 1\n" +
		"model name\t: Intel(R) Core(TM) i9-14900K\n"
	if got := ParseCPUModel(strings.NewReader(in)); got != "Intel(R) Core(TM) i9-14900K" {
		t.Errorf("got %q, want %q", got, "Intel(R) Core(TM) i9-14900K")
	}
}

// Some ARM kernels omit "model name" entirely. The UI then shows just the core
// count, so the parser must return "" rather than erroring.
func TestParseCPUModelAbsent(t *testing.T) {
	in := "processor\t: 0\nBogoMIPS\t: 108.00\nFeatures\t: fp asimd\n"
	if got := ParseCPUModel(strings.NewReader(in)); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadCPUModelMissingFile(t *testing.T) {
	if got := ReadCPUModel(t.TempDir()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
go test ./internal/collect/ -run TestParseCPUModel -v
```

Expected: FAIL — `undefined: ParseCPUModel`.

- [ ] **Step 3: Write the implementation**

Append to `internal/collect/cpu.go`:

```go
// ParseCPUModel returns the first "model name" value from /proc/cpuinfo, or ""
// if the key is absent (some ARM kernels omit it).
func ParseCPUModel(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "model name") {
			continue
		}
		if i := strings.IndexByte(line, ':'); i >= 0 {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
}

// ReadCPUModel reads the CPU model name. Constant for the process lifetime, so
// callers should read it once rather than per tick.
func ReadCPUModel(hostProc string) string {
	f, err := os.Open(filepath.Join(hostProc, "cpuinfo"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return ParseCPUModel(f)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/collect/ -run TestParseCPUModel -v
go test ./internal/collect/ -run TestReadCPUModel -v
```

Expected: PASS.

- [ ] **Step 5: Plumb it through the model and collector**

In `internal/model/snapshot.go`, replace `CPUInfo`:

```go
type CPUInfo struct {
	Agg   float64   `json:"agg"`
	Cores []float64 `json:"cores"`
	Temp  float64   `json:"temp"`  // 0 if unknown
	Model string    `json:"model"` // e.g. "Intel(R) Core(TM) i9-14900K"; "" if unknown
}
```

In `internal/collect/collector.go`, add one more field to the `Collector` struct, next to `osName` / `kernel`:

```go
	cpuModel string
```

Add one more line to the `New` literal, after `kernel:`:

```go
		cpuModel: ReadCPUModel(cfg.HostProc),
```

In `Tick`, set it beside the existing Temp assignment. `cpu()` early-returns a zero `CPUInfo` on read error, so assigning here (rather than inside `cpu()`) means the model name survives a `/proc/stat` failure:

```go
	snap.CPU.Temp = ReadCPUTemp(c.cfg.HostSys)
	snap.CPU.Model = c.cpuModel
```

- [ ] **Step 6: Verify the whole package still passes**

```bash
go build ./... && go test ./internal/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/collect/cpu.go internal/collect/cpu_test.go \
        internal/collect/collector.go internal/model/snapshot.go
git commit -m "feat(collect): report the CPU model name

Read once from /proc/cpuinfo; the CPU card's subtitle needs it."
```

---

### Task 4: Desktop — enforce the 1100×780 minimum window

The layout has a floor. On desktop the floor is enforced by GTK so the window physically cannot shrink below it — which is why the desktop never needs a scroll fallback.

**Note:** `TestWindowSizeRoundTrip` currently saves 1000×700 and asserts it round-trips. That size is now below the floor, so the existing test must change — this is intentional, not collateral damage.

**Files:**
- Modify: `internal/desktop/geometry.go:17-20` (constants), `:30-46` (LoadWindowSize)
- Modify: `internal/desktop/geometry_test.go:7-16`
- Modify: `internal/desktop/window.go` (cgo `run_window`, `WindowConfig`, `RunWindow`)
- Modify: `cmd/desktop/main.go` (RunWindow call)

**Interfaces:**
- Consumes: nothing new.
- Produces: `desktop.MinWidth = 1100`, `desktop.MinHeight = 780` (exported consts); `desktop.WindowConfig.MinWidth`, `desktop.WindowConfig.MinHeight` (`int`; `0` means no hint). `LoadWindowSize()` now clamps up to the minimum.

- [ ] **Step 1: Write the failing tests**

In `internal/desktop/geometry_test.go`, replace `TestWindowSizeRoundTrip` and append a clamp test:

```go
func TestWindowSizeRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 1200, Height: 800}); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowSize()
	if got.Width != 1200 || got.Height != 800 {
		t.Errorf("got %+v, want {1200 800}", got)
	}
}

// A size saved before the minimum existed (or by a window manager that ignored
// the hint) must not reopen a window too small to lay out.
func TestLoadClampsBelowMinimum(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 800, Height: 600}); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowSize()
	if got.Width != MinWidth || got.Height != MinHeight {
		t.Errorf("got %+v, want clamped to {%d %d}", got, MinWidth, MinHeight)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
go test ./internal/desktop/ -run 'WindowSizeRoundTrip|ClampsBelowMinimum' -v
```

Expected: FAIL — `undefined: MinWidth`.

- [ ] **Step 3: Write the implementation**

In `internal/desktop/geometry.go`, replace the const block:

```go
const (
	defaultWidth  = 1440
	defaultHeight = 900

	// MinWidth/MinHeight are the smallest window the dashboard lays out
	// correctly in. Below this the charts collapse to unreadable slivers and
	// the 32-core grid disappears, so GTK is told to refuse smaller sizes.
	MinWidth  = 1100
	MinHeight = 780
)
```

In `LoadWindowSize`, replace the final `return ws` with:

```go
	if ws.Width < MinWidth {
		ws.Width = MinWidth
	}
	if ws.Height < MinHeight {
		ws.Height = MinHeight
	}
	return ws
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/desktop/ -run 'WindowSizeRoundTrip|ClampsBelowMinimum|LoadDefault|SaveRejects' -v
```

Expected: PASS (4 tests).

- [ ] **Step 5: Apply the GTK geometry hint**

In `internal/desktop/window.go`, replace the `run_window` C function signature and add the hint right after `gtk_window_set_default_size`:

```c
static void run_window(const char *title, const char *url, int width, int height,
                       int min_width, int min_height, int autoclose_ms) {
    gtk_init(0, NULL);
    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), title);
    gtk_window_set_default_size(GTK_WINDOW(window), width, height);
    if (min_width > 0 && min_height > 0) {
        GdkGeometry hints;
        hints.min_width = min_width;
        hints.min_height = min_height;
        gtk_window_set_geometry_hints(GTK_WINDOW(window), NULL, &hints, GDK_HINT_MIN_SIZE);
    }
    // Resolve the taskbar/window icon from the installed hicolor theme.
    gtk_window_set_icon_name(GTK_WINDOW(window), "system-monitor");
```

(The rest of `run_window` is unchanged.)

Add two fields to `WindowConfig`, after `Height`:

```go
	MinWidth    int            // 0 = no minimum-size hint
	MinHeight   int            // 0 = no minimum-size hint
```

And update the `C.run_window` call in `RunWindow`:

```go
	C.run_window(ctitle, curl, C.int(cfg.Width), C.int(cfg.Height),
		C.int(cfg.MinWidth), C.int(cfg.MinHeight), C.int(cfg.AutoCloseMS))
```

- [ ] **Step 6: Pass the minimum from main**

In `cmd/desktop/main.go`, add two fields to the `desktop.WindowConfig` literal, after `Height:`:

```go
		MinWidth:    desktop.MinWidth,
		MinHeight:   desktop.MinHeight,
```

- [ ] **Step 7: Verify it builds and the package passes**

```bash
go build ./... && go test ./internal/desktop/ -v
```

Expected: PASS. The existing `window_smoke_test.go` passes `MinWidth`/`MinHeight` of 0, which the `if` guard skips.

- [ ] **Step 8: Commit**

```bash
git add internal/desktop/geometry.go internal/desktop/geometry_test.go \
        internal/desktop/window.go cmd/desktop/main.go
git commit -m "feat(desktop): refuse to resize below 1100x780

The redesigned dashboard fits one viewport with no scrolling, which only
works down to a floor. GTK enforces the floor so the desktop can never
render the squashed state."
```

---

### Task 5: Frontend — light shell, topbar, and the empty card grid

This task replaces the dark three-by-three tile grid with the new two-column shell and gets the topbar live. The cards are present but empty; they fill in over Tasks 6–9. The app must run and look right after this task.

`web/format.js` is new: the formatting helpers move out of `app.js` so `chart.js`, `cards.js`, and `app.js` can all use them. Scripts are plain `<script>` tags with no build step, so load order is the dependency graph: `format.js` → `chart.js` → `cards.js` → `app.js`.

**Files:**
- Create: `web/format.js`
- Rewrite: `web/style.css`
- Rewrite: `web/index.html`
- Rewrite: `web/app.js`

**Interfaces:**
- Produces (globals, from `format.js`): `$(id)`, `cssVar(name)`, `esc(s)`, `fmtBytes(b)`, `fmtRate(b)`, `fmtUptime(s)`, `pct(v)`, `peak(arr)`.
- Produces (globals, from `app.js`): `applySnap(s)`, `seedHistory(history)`, `renderTopbar(s)`, `initCharts(s)`, `setConn(on)`, and the chart handles `cpuChart`, `memChart`, `gpuChart`, `netChart`, `diskChart`, plus `coreCells` (array of the `<b>` fill elements) and `hasGPU` (bool).
- Produces (DOM contract for later tasks): element ids `subCpu subMem subGpu subNet subDisk`, `cpuChart memChart gpuChart netChart diskChart`, `cpuGut memGut gpuGut netGut dskGut`, `card-gpu`, `procBody fsBody fsNote`.

- [ ] **Step 1: Create the formatting helpers**

Create `web/format.js`:

```js
// Shared formatting and DOM helpers. Loaded first; chart.js, cards.js and
// app.js all depend on these globals.
const $ = id => document.getElementById(id);
const cssVar = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
const esc = s => String(s).replace(/[&<>"']/g, c =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function fmtBytes(b) {
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB']; let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return b.toFixed(b >= 100 || i === 0 ? 0 : 1) + ' ' + u[i];
}
const fmtRate = b => fmtBytes(b) + '/s';

function fmtUptime(s) {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return (d ? d + 'd ' : '') + h + 'h ' + m + 'm';
}
const pct = v => v.toFixed(0) + '%';

// Highest value in a chart's rolling window; [] -> 0.
const peak = arr => (arr && arr.length) ? Math.max(...arr) : 0;
```

- [ ] **Step 2: Rewrite the stylesheet**

Replace all of `web/style.css`:

```css
:root {
  --bg: #f6f7f9; --card: #fff; --line: #e8eaee; --track: #eef0f4;
  --ink: #1a1d23; --sub: #8b909a;
  --blue: #2f7ff5; --green: #22c55e; --red: #ef4444;
  --purple: #a855f7; --cyan: #38bdf8; --amber: #f59e0b;
  --grid: rgba(26, 29, 35, 0.07); /* chart gridlines; read by chart.js */
  --gutter: 54px;                 /* fixed Y-label column — the left edge of every plot */
  --statsw: 210px;                /* fixed stats column — the right edge of every plot */
}
* { box-sizing: border-box; margin: 0; }
html, body { height: 100%; }
body {
  background: var(--bg); color: var(--ink);
  font: 13px/1.35 system-ui, -apple-system, sans-serif;
  overflow: auto; /* only ever engages below the minimum size; see #app */
}

/* Fits exactly one viewport at or above the floor. Below the floor the app
   keeps its minimum size and the body scrolls, which is the browser-only
   escape hatch — the desktop shell blocks resizing below it instead. */
#app {
  height: 100vh; min-width: 1100px; min-height: 780px;
  display: flex; flex-direction: column;
}

/* ---- topbar ---- */
#topbar { display: flex; align-items: center; padding: 9px 12px; flex: none; }
.brand { display: flex; align-items: center; gap: 8px; font-size: 15px; font-weight: 700; }
.logo {
  width: 24px; height: 24px; border-radius: 7px; background: var(--blue); color: #fff;
  display: grid; place-items: center; font-size: 13px; flex: none;
}
.meta { display: flex; align-items: center; gap: 11px; margin-left: 16px; font-size: 11px; color: var(--sub); }
.meta b { color: var(--ink); font-weight: 600; font-variant-numeric: tabular-nums; }
.meta .d { width: 1px; height: 11px; background: var(--line); flex: none; }
/* OS and kernel are the first things to go when the window narrows, and are
   hidden outright when the collector could not read them. */
@media (max-width: 1320px) { .meta .sm { display: none; } }
.meta.no-os .sm { display: none; }

#conn {
  margin-left: auto; display: flex; align-items: center; gap: 6px;
  font-size: 10.5px; font-weight: 600; padding: 3px 9px; border-radius: 20px;
}
#conn i { width: 5px; height: 5px; border-radius: 50%; display: block; }
#conn.on { background: #eafaf0; color: #15803d; }
#conn.on i { background: var(--green); }
#conn.off { background: #fdeaee; color: #b91c1c; }
#conn.off i { background: var(--red); }

/* ---- grid: ONE row, so both columns are the same height by definition ---- */
#grid {
  flex: 1; min-height: 0; display: grid;
  grid-template-columns: 1fr 400px; grid-template-rows: 1fr;
  gap: 8px; padding: 0 10px 10px;
}
.col { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
.card {
  background: var(--card); border: 1px solid var(--line); border-radius: 10px;
  padding: 10px 13px; display: flex; flex-direction: column; min-height: 0; overflow: hidden;
}
#colLeft > .card { flex: 1; }
#card-proc { flex: 3; }
#card-fs { flex: 1; }

/* ---- card header ---- */
.ch { display: flex; align-items: center; gap: 9px; margin-bottom: 7px; flex: none; }
.cico { width: 26px; height: 26px; border-radius: 7px; display: grid; place-items: center; font-size: 13px; flex: none; }
/* Card icon tints — a pale wash of that card's own series colour. These washes
   and the #conn badge's tints are the only literal colours below :root; every
   other colour in this file is a var. */
.cico.i-blue { background: #e8f0fe; color: var(--blue); }
.cico.i-red { background: #fdeaee; color: var(--red); }
.cico.i-green { background: #eafaf0; color: var(--green); }
.cico.i-purple { background: #f7ecfe; color: var(--purple); }
.cico.i-plain { background: var(--track); color: var(--ink); }
.ct { font-size: 13.5px; font-weight: 700; letter-spacing: -.01em; }
/* Every card has a subtitle, even a derived one — a missing subtitle would
   make that card's header shorter and its chart taller than its neighbours'. */
.cs {
  font-size: 10.5px; color: var(--sub); margin-top: 1px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 340px;
}

/* ---- card body: [fixed gutter][flex chart] + [fixed stats] ---- */
.cbody { display: flex; gap: 12px; flex: 1; min-height: 0; }
.plot { flex: 1; min-width: 0; display: flex; flex-direction: column; min-height: 0; }
.chartrow { display: flex; flex: 1; min-height: 0; }
.gut {
  width: var(--gutter); flex: none; display: flex; flex-direction: column;
  justify-content: space-between; font-size: 9px; color: var(--sub);
  text-align: right; padding-right: 6px; font-variant-numeric: tabular-nums;
}
.cv { flex: 1; min-width: 0; height: 100%; display: block; }
.xax {
  display: flex; justify-content: space-between; font-size: 9px; color: var(--sub);
  margin-left: var(--gutter); margin-top: 3px; flex: none;
}
.stats { width: var(--statsw); flex: none; display: flex; flex-direction: column; min-height: 0; }

/* ---- stat rows ---- */
.r { display: flex; align-items: baseline; gap: 8px; padding: 1.5px 0; font-size: 11.5px; flex: none; }
.r .k { color: var(--sub); display: flex; align-items: center; gap: 6px; }
.r .v { margin-left: auto; font-variant-numeric: tabular-nums; font-weight: 600; }
.r.dim .k, .r.dim .v { color: var(--sub); font-weight: 400; font-size: 10px; }
/* A dot maps a row to its series. .hole keeps rows without a series aligned. */
.dot { width: 6px; height: 6px; border-radius: 50%; flex: none; }
.dot.hole { background: transparent; }
/* Series swatches. One rule per colour, worn by whatever marks that series —
   a legend dot, a stacked-bar segment, or a key square — so the palette stays
   defined only in :root. */
.sw-blue { background: var(--blue); }
.sw-red { background: var(--red); }
.sw-green { background: var(--green); }
.sw-amber { background: var(--amber); }
.sw-cyan { background: var(--cyan); }
.sw-purple { background: var(--purple); }
.sw-track { background: var(--track); }
.sep { border-top: 1px solid var(--line); margin: 5px 0; flex: none; }
.cap {
  font-size: 8.5px; letter-spacing: .05em; color: var(--sub); font-weight: 600;
  text-transform: uppercase; margin-bottom: 4px; flex: none;
}

/* ---- per-core grid ---- */
.cores { display: grid; grid-template-columns: repeat(16, 1fr); gap: 2px; flex: 1; min-height: 14px; }
.cores i { background: var(--track); border-radius: 2px; position: relative; overflow: hidden; display: block; }
.cores i b { position: absolute; bottom: 0; left: 0; right: 0; display: block; }

/* ---- per-device utilisation ---- */
#devList { display: flex; flex-direction: column; gap: 2px; overflow: hidden; }
.dev {
  display: grid; grid-template-columns: 66px 1fr 30px; gap: 6px;
  align-items: center; font-size: 10px; padding: 1px 0;
}
.dev .nm { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dev .bar { height: 5px; background: var(--track); border-radius: 3px; overflow: hidden; }
.dev .bar i { display: block; height: 100%; border-radius: 3px; background: var(--purple); }
.dev .bar i.hot { background: var(--red); }
.dev .pc { text-align: right; font-variant-numeric: tabular-nums; color: var(--sub); }

/* ---- stacked bar ---- */
.stack { height: 8px; border-radius: 4px; overflow: hidden; display: flex; background: var(--track); flex: none; }
.stack i { display: block; height: 100%; }
.skey { display: flex; gap: 10px; font-size: 9px; color: var(--sub); margin-top: 4px; flex: none; }
.skey b { display: inline-block; width: 6px; height: 6px; border-radius: 2px; margin-right: 3px; }

/* ---- right-column tables ---- */
.tw { flex: 1; min-height: 0; overflow: hidden; } /* overflow hidden, never auto: autoFit() trims instead */
.tbl { width: 100%; border-collapse: collapse; font-size: 11.5px; }
.tbl th {
  font-size: 8.5px; letter-spacing: .05em; text-transform: uppercase; color: var(--sub);
  font-weight: 600; text-align: left; padding: 0 0 4px; border-bottom: 1px solid var(--line);
}
.tbl td { padding: 3px 0; border-bottom: 1px solid var(--track); }
.tbl .n { text-align: right; font-variant-numeric: tabular-nums; }
.tbl td.nm { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 0; width: 100%; }
/* Always present, even when empty, so appending the note cannot re-overflow
   the table that autoFit() just trimmed to fit. */
.note { font-size: 9.5px; color: var(--sub); height: 14px; flex: none; padding-top: 2px; }
.fsbar {
  width: 60px; height: 5px; background: var(--track); border-radius: 3px;
  overflow: hidden; display: inline-block; vertical-align: middle;
}
.fsbar i { display: block; height: 100%; background: var(--blue); }
.fsbar i.hot { background: var(--red); }
```

- [ ] **Step 3: Rewrite the markup**

Replace all of `web/index.html`:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>System Monitor</title>
<link rel="stylesheet" href="style.css">
</head>
<body>
<div id="app">
  <header id="topbar">
    <div class="brand"><span class="logo">◈</span> System Monitor</div>
    <div class="meta" id="meta">
      <b id="host">—</b>
      <i class="d"></i>
      <span class="sm" id="os"></span>
      <i class="d sm"></i>
      <span class="sm" id="kernel"></span>
      <i class="d sm"></i>
      <span>up <b id="uptime">—</b></span>
      <i class="d"></i>
      <span>load <b id="load">—</b></span>
      <i class="d"></i>
      <b id="clock">—</b>
    </div>
    <div id="conn" class="off"><i></i><span>Offline</span></div>
  </header>

  <main id="grid">
    <div class="col" id="colLeft">
      <section class="card" id="card-cpu">
        <div class="ch">
          <div class="cico i-blue">▦</div>
          <div><div class="ct">CPU</div><div class="cs" id="subCpu">—</div></div>
        </div>
        <div class="cbody">
          <div class="plot">
            <div class="chartrow">
              <div class="gut" id="cpuGut"><span>100%</span><span>50%</span><span>0%</span></div>
              <canvas class="cv" id="cpuChart"></canvas>
            </div>
            <div class="xax"><span>60s</span><span>30s</span><span>now</span></div>
          </div>
          <div class="stats" id="cpuStats"></div>
        </div>
      </section>

      <section class="card" id="card-mem">
        <div class="ch">
          <div class="cico i-red">▤</div>
          <div><div class="ct">Memory &amp; Swap</div><div class="cs" id="subMem">—</div></div>
        </div>
        <div class="cbody">
          <div class="plot">
            <div class="chartrow">
              <div class="gut" id="memGut"><span>100%</span><span>50%</span><span>0%</span></div>
              <canvas class="cv" id="memChart"></canvas>
            </div>
            <div class="xax"><span>60s</span><span>30s</span><span>now</span></div>
          </div>
          <div class="stats" id="memStats"></div>
        </div>
      </section>

      <section class="card" id="card-gpu">
        <div class="ch">
          <div class="cico i-green">◫</div>
          <div><div class="ct">GPU</div><div class="cs" id="subGpu">—</div></div>
        </div>
        <div class="cbody">
          <div class="plot">
            <div class="chartrow">
              <div class="gut" id="gpuGut"><span>100%</span><span>50%</span><span>0%</span></div>
              <canvas class="cv" id="gpuChart"></canvas>
            </div>
            <div class="xax"><span>60s</span><span>30s</span><span>now</span></div>
          </div>
          <div class="stats" id="gpuStats"></div>
        </div>
      </section>

      <section class="card" id="card-net">
        <div class="ch">
          <div class="cico i-blue">⇅</div>
          <div><div class="ct">Network</div><div class="cs" id="subNet">—</div></div>
        </div>
        <div class="cbody">
          <div class="plot">
            <div class="chartrow">
              <div class="gut" id="netGut"><span>—</span><span>—</span><span>0</span></div>
              <canvas class="cv" id="netChart"></canvas>
            </div>
            <div class="xax"><span>60s</span><span>30s</span><span>now</span></div>
          </div>
          <div class="stats" id="netStats"></div>
        </div>
      </section>

      <section class="card" id="card-disk">
        <div class="ch">
          <div class="cico i-purple">▥</div>
          <div><div class="ct">Disk</div><div class="cs" id="subDisk">—</div></div>
        </div>
        <div class="cbody">
          <div class="plot">
            <div class="chartrow">
              <div class="gut" id="dskGut"><span>—</span><span>—</span><span>0</span></div>
              <canvas class="cv" id="diskChart"></canvas>
            </div>
            <div class="xax"><span>60s</span><span>30s</span><span>now</span></div>
          </div>
          <div class="stats" id="dskStats"></div>
        </div>
      </section>
    </div>

    <div class="col" id="colRight">
      <section class="card" id="card-proc">
        <div class="ch">
          <div class="cico i-plain">≡</div>
          <div><div class="ct">Top Processes</div><div class="cs" id="subProc">by CPU</div></div>
        </div>
        <div class="tw" data-fit="proc">
          <table class="tbl">
            <thead><tr><th>Process</th><th class="n">CPU</th><th class="n">Memory</th><th class="n">PID</th></tr></thead>
            <tbody id="procBody"></tbody>
          </table>
        </div>
      </section>

      <section class="card" id="card-fs">
        <div class="ch">
          <div class="cico i-plain">◰</div>
          <div><div class="ct">Filesystems</div><div class="cs" id="subFs">by usage</div></div>
        </div>
        <div class="tw" data-fit="fs">
          <table class="tbl">
            <thead><tr><th>Mount</th><th class="n">Used</th><th class="n"></th><th class="n">%</th></tr></thead>
            <tbody id="fsBody"></tbody>
          </table>
        </div>
        <div class="note" id="fsNote"></div>
      </section>
    </div>
  </main>
</div>
<script src="format.js"></script>
<script src="chart.js"></script>
<script src="cards.js"></script>
<script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 4: Create a stub cards.js so the page loads**

The renderers land in Tasks 6–9. Create `web/cards.js` with just the header comment for now:

```js
// Per-card renderers. Each takes the snapshot and updates one card's DOM.
// Chart handles (cpuChart, memChart, ...) and coreCells are globals created by
// initCharts() in app.js.
```

- [ ] **Step 5: Rewrite app.js — topbar and wiring only**

Replace all of `web/app.js`:

```js
let cpuChart, memChart, gpuChart, netChart, diskChart;
let coreCells = [], hasGPU = false;

// Sets the three labels of an auto-scaled chart's Y gutter. Fixed-scale charts
// (CPU, Memory, GPU) have static labels in the HTML instead.
function setRateGutter(id, ymax) {
  const g = $(id);
  g.children[0].textContent = fmtRate(ymax);
  g.children[1].textContent = fmtRate(ymax / 2);
  g.children[2].textContent = '0';
}

// Charts are created from the first snapshot, because the core count and
// whether a GPU exists are only knowable from real data.
function initCharts(s) {
  const cores = (s.cpu.cores || []).length;
  hasGPU = !!(s.gpu && s.gpu.length);
  // No GPU: drop the card entirely rather than render one full of dashes. The
  // remaining four cards take its space via flex:1.
  if (!hasGPU) { const c = $('card-gpu'); if (c) c.remove(); }

  cpuChart = new Chart($('cpuChart'), {
    series: Array.from({ length: cores }, (_, i) =>
      ({ color: `hsl(${(i * 360 / cores) | 0} 65% 55%)` })),
    yMax: 100,
  });
  memChart = new Chart($('memChart'), {
    series: [{ color: cssVar('--red') }, { color: cssVar('--green') }], yMax: 100, fill: true,
  });
  netChart = new Chart($('netChart'), {
    series: [{ color: cssVar('--blue') }, { color: cssVar('--amber') }], fill: true,
    onScale: m => setRateGutter('netGut', m),
  });
  diskChart = new Chart($('diskChart'), {
    series: [{ color: cssVar('--cyan') }, { color: cssVar('--purple') }], fill: true,
    onScale: m => setRateGutter('dskGut', m),
  });
  if (hasGPU) {
    gpuChart = new Chart($('gpuChart'), { series: [{ color: cssVar('--blue') }], yMax: 100, fill: true });
  }

  const grid = $('coreGrid');
  if (grid) {
    grid.innerHTML = ''; coreCells = [];
    for (let i = 0; i < cores; i++) {
      const cell = document.createElement('i');
      const bar = document.createElement('b');
      cell.appendChild(bar); grid.appendChild(cell); coreCells.push(bar);
    }
  }
}

function renderTopbar(s) {
  $('host').textContent = s.host.name;
  $('os').textContent = s.host.os || '';
  $('kernel').textContent = s.host.kernel || '';
  // Hide OS/kernel outright when the collector could not read them, rather
  // than leaving empty slots and stray dividers.
  $('meta').classList.toggle('no-os', !s.host.os && !s.host.kernel);
  $('uptime').textContent = fmtUptime(s.host.uptime);
  $('load').textContent = s.host.load.map(x => x.toFixed(2)).join(' ');
  $('clock').textContent = new Date(s.t * 1000).toLocaleTimeString();
}

function applySnap(s) {
  // Coalesce nil Go slices (JSON null) to [] so one missing collector can't
  // abort the whole render.
  s.cpu.cores = s.cpu.cores || [];
  s.disk.devs = s.disk.devs || [];
  s.net.ifaces = s.net.ifaces || [];
  s.gpu = s.gpu || [];
  s.fs = s.fs || [];
  s.proc = s.proc || [];
  if (!cpuChart) initCharts(s);
  renderTopbar(s);
}

function seedHistory(history) {
  if (!history.length) return;
  const first = history[0];
  first.cpu.cores = first.cpu.cores || [];
  first.gpu = first.gpu || [];
  if (!cpuChart) initCharts(first);
  applySnap(history[history.length - 1]);
}

function setConn(on) {
  const e = $('conn');
  e.className = on ? 'on' : 'off';
  e.lastElementChild.textContent = on ? 'Live' : 'Offline';
}

let ws, backoff = 500;
function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.onopen = () => { backoff = 500; setConn(true); };
  ws.onmessage = ev => {
    const m = JSON.parse(ev.data);
    if (m.type === 'init') seedHistory(m.history || []);
    else if (m.type === 'tick') applySnap(m.snap);
  };
  ws.onclose = () => {
    setConn(false);
    setTimeout(connect, backoff); backoff = Math.min(backoff * 2, 5000);
  };
  ws.onerror = () => ws.close();
}
connect();
```

- [ ] **Step 6: Run the app and capture it**

`cmd/web` defaults to the container's `/host/*` paths, so a native run needs the host paths passed explicitly. `HOST_ROOT` must be `/` and not the empty string — `config.env()` falls back to the default when a variable is empty.

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
SD=/tmp/claude-1000/-media-xuanlocserver-DellEMC12T-workingspace-system-monitor-service/a07d4590-3c47-460d-a995-3bf9b12d59dd/scratchpad
go build -o $SD/sm-web ./cmd/web
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 2
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1440,863 --screenshot=$SD/t5.png http://localhost:8090/
```

Read `$SD/t5.png`. Expected: light background; topbar showing a real hostname, `Ubuntu 24.04.4 LTS`, `6.17.0-40-generic`, uptime, load, clock, and a green `Live` badge; seven bordered cards in two columns; both columns ending at the same y; no scrollbar. Cards are empty apart from headers — that is correct at this stage.

If every value reads `—`, the capture beat the first WebSocket frame: raise `--virtual-time-budget`.

Kill the server when done: `kill %1`.

- [ ] **Step 7: Commit**

```bash
git add web/format.js web/style.css web/index.html web/app.js web/cards.js
git commit -m "feat(web): light two-column shell and live topbar

Replaces the dark 3x3 tile grid. The grid has exactly one row, so the two
columns are the same height by construction rather than by tuning. OS and
kernel move into the topbar; the System summary card is gone."
```

---

### Task 6: Frontend — chart.js on light, and the CPU card

Two changes to `chart.js`: gridlines read their colour from the theme (they are currently hardcoded `rgba(255,255,255,0.06)` — invisible on white), and auto-scaled charts report their resolved Y max so the gutter can label itself.

**Files:**
- Modify: `web/chart.js:3-17` (constructor), `:38-51` (render)
- Modify: `web/cards.js` (add `renderCPU`)
- Modify: `web/index.html` (fill `#cpuStats`)
- Modify: `web/app.js` (call `renderCPU`)

**Interfaces:**
- Consumes: `cssVar` (format.js), `pct`, `peak`.
- Produces: `Chart` option `onScale: (ymax) => void`, called at the end of every `render()`; `renderCPU(s)`.

- [ ] **Step 1: Update chart.js**

In `web/chart.js`, replace the constructor's first lines (through `this.fill = fill;`) with:

```js
class Chart {
  constructor(canvas, { series, maxPoints = 60, yMax = null, fill = false, onScale = null }) {
    this.c = canvas;
    this.ctx = canvas.getContext('2d');
    this.series = series;            // [{color}]
    this.maxPoints = maxPoints;
    this.yMax = yMax;                // null = auto
    this.fill = fill;
    this.onScale = onScale;          // called with the resolved Y max after each render
```

In `render()`, replace the hardcoded gridline colour:

```js
    // Gridline colour comes from the theme, so the chart is legible on any
    // background instead of assuming a dark one.
    ctx.strokeStyle = cssVar('--grid') || 'rgba(0,0,0,0.07)'; ctx.lineWidth = 1;
```

And add the callback as the last statement of `render()`, after the series loop closes:

```js
    // Safe to call during render: the gutter is a fixed width, so relabelling
    // it cannot resize the canvas and re-enter here.
    if (this.onScale) this.onScale(ymax);
```

- [ ] **Step 2: Fill in the CPU stats column**

In `web/index.html`, replace `<div class="stats" id="cpuStats"></div>` with:

```html
          <div class="stats" id="cpuStats">
            <div class="r"><span class="k"><i class="dot hole"></i>Average</span><span class="v" id="cpuAvg">—</span></div>
            <div class="r"><span class="k"><i class="dot hole"></i>Max core</span><span class="v" id="cpuMax">—</span></div>
            <div class="r" id="cpuTempRow"><span class="k"><i class="dot hole"></i>Temperature</span><span class="v" id="cpuTemp">—</span></div>
            <div class="sep"></div>
            <div class="cap">Per-core</div>
            <div class="cores" id="coreGrid"></div>
          </div>
```

The CPU rows use `.dot.hole` deliberately: with 32 rainbow series there is no single colour a legend dot could honestly stand for, but the transparent dot keeps these rows aligned with the other cards'.

- [ ] **Step 3: Write renderCPU**

Append to `web/cards.js`:

```js
function renderCPU(s) {
  const n = s.cpu.cores.length;
  $('subCpu').textContent = n + ' cores' + (s.cpu.model ? ' · ' + s.cpu.model : '');
  cpuChart.push(s.cpu.cores);
  $('cpuAvg').textContent = pct(s.cpu.agg);
  $('cpuMax').textContent = pct(peak(s.cpu.cores));
  // temp === 0 is the collector's "unknown"; hide the row rather than show 0°C.
  if (s.cpu.temp > 0) {
    $('cpuTempRow').style.display = '';
    $('cpuTemp').textContent = s.cpu.temp.toFixed(0) + '°C';
  } else {
    $('cpuTempRow').style.display = 'none';
  }
  const hot = cssVar('--red'), norm = cssVar('--blue');
  s.cpu.cores.forEach((v, i) => {
    const bar = coreCells[i];
    if (!bar) return;
    bar.style.height = v + '%';
    bar.style.background = v > 80 ? hot : norm;
  });
}
```

- [ ] **Step 4: Call it, and seed the chart**

In `web/app.js`, add to `applySnap`, immediately after `if (!cpuChart) initCharts(s);`:

```js
  renderCPU(s);
```

And in `seedHistory`, add before the final `applySnap(...)` line:

```js
  const past = history.slice(0, -1);
  cpuChart.seed(past.map(x => x.cpu.cores || []));
```

- [ ] **Step 5: Run and capture**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
SD=/tmp/claude-1000/-media-xuanlocserver-DellEMC12T-workingspace-system-monitor-service/a07d4590-3c47-460d-a995-3bf9b12d59dd/scratchpad
go build -o $SD/sm-web ./cmd/web
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 3
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1440,863 --screenshot=$SD/t6.png http://localhost:8090/
kill %1
```

Read `$SD/t6.png`. Expected: the CPU subtitle reads `32 cores · Intel(R) Core(TM) i9-14900K`; the chart shows rainbow lines with **visible** gridlines; Average / Max core / Temperature are populated; a 16-wide, 2-row core grid is filled. Gridlines still invisible means `--grid` is not resolving — check that `format.js` loads before `chart.js`.

- [ ] **Step 6: Commit**

```bash
git add web/chart.js web/cards.js web/index.html web/app.js
git commit -m "feat(web): CPU card, and make charts theme-aware

Gridlines were hardcoded to a translucent white that is invisible on the
light background; they now come from --grid. Auto-scaled charts report
their Y max via onScale so the gutter can label itself."
```

---

### Task 7: Frontend — Memory and GPU cards

**Files:**
- Modify: `web/index.html` (fill `#memStats`, `#gpuStats`)
- Modify: `web/cards.js` (add `renderMem`, `renderGPU`)
- Modify: `web/app.js` (call them, seed their charts)

**Interfaces:**
- Consumes: `fmtBytes`, `pct`, `cssVar`, the `memChart` / `gpuChart` globals, `hasGPU`.
- Produces: `renderMem(s)`, `renderGPU(s)`.

- [ ] **Step 1: Fill in the Memory stats column**

In `web/index.html`, replace `<div class="stats" id="memStats"></div>` with:

```html
          <div class="stats" id="memStats">
            <div class="r"><span class="k"><i class="dot sw-red"></i>Memory</span><span class="v" id="memV">—</span></div>
            <div class="r dim"><span class="k"><i class="dot hole"></i></span><span class="v" id="memDim">—</span></div>
            <div class="r"><span class="k"><i class="dot sw-green"></i>Swap</span><span class="v" id="swapV">—</span></div>
            <div class="r dim"><span class="k"><i class="dot hole"></i></span><span class="v" id="swapDim">—</span></div>
            <div class="sep"></div>
            <div class="cap">RAM breakdown</div>
            <div class="stack">
              <i id="stkUsed" class="sw-red"></i>
              <i id="stkCache" class="sw-amber"></i>
            </div>
            <div class="skey">
              <span><b class="sw-red"></b><span id="keyUsed">—</span></span>
              <span><b class="sw-amber"></b><span id="keyCache">—</span></span>
              <span><b class="sw-track"></b><span id="keyFree">—</span></span>
            </div>
          </div>
```

- [ ] **Step 2: Fill in the GPU stats column**

Replace `<div class="stats" id="gpuStats"></div>` with:

```html
          <div class="stats" id="gpuStats">
            <div class="r"><span class="k"><i class="dot sw-blue"></i>Utilisation</span><span class="v" id="gpuUtil">—</span></div>
            <div class="r"><span class="k"><i class="dot hole"></i>Temperature</span><span class="v" id="gpuTemp">—</span></div>
            <div class="r"><span class="k"><i class="dot hole"></i>Power</span><span class="v" id="gpuPower">—</span></div>
            <div class="r"><span class="k"><i class="dot hole"></i>Clock</span><span class="v" id="gpuClk">—</span></div>
            <div class="r" id="gpuFanRow"><span class="k"><i class="dot hole"></i>Fan</span><span class="v" id="gpuFan">—</span></div>
            <div class="sep"></div>
            <div class="cap" id="gpuVramCap">VRAM</div>
            <div class="stack"><i id="gpuVramBar" class="sw-blue"></i></div>
          </div>
```

There is deliberately **no Fan row on the CPU card**. The design showed `Fan 0%` there, but hwmon reports fan speed in RPM and does not reliably identify which fan is the CPU's. The GPU's fan is a real percentage from NVML, so it is shown — and hidden when NVML reports `-1`.

- [ ] **Step 3: Write renderMem and renderGPU**

Append to `web/cards.js`:

```js
function renderMem(s) {
  $('subMem').textContent = fmtBytes(s.mem.total) + ' RAM · ' + fmtBytes(s.mem.swap_total) + ' swap';
  memChart.push([s.mem.pct, s.mem.swap_pct]);
  $('memV').textContent = fmtBytes(s.mem.used) + ' / ' + fmtBytes(s.mem.total);
  $('memDim').textContent = pct(s.mem.pct) + ' · cache ' + fmtBytes(s.mem.cache);
  $('swapV').textContent = fmtBytes(s.mem.swap_used) + ' / ' + fmtBytes(s.mem.swap_total);
  $('swapDim').textContent = pct(s.mem.swap_pct) + ' used';

  const total = s.mem.total || 1;
  const free = Math.max(0, s.mem.total - s.mem.used - s.mem.cache);
  $('stkUsed').style.width = (s.mem.used / total) * 100 + '%';
  $('stkCache').style.width = (s.mem.cache / total) * 100 + '%';
  $('keyUsed').textContent = 'used ' + fmtBytes(s.mem.used);
  $('keyCache').textContent = 'cache ' + fmtBytes(s.mem.cache);
  $('keyFree').textContent = 'free ' + fmtBytes(free);
}

// Only called when hasGPU; the card is removed from the DOM otherwise.
function renderGPU(s) {
  const g = s.gpu[0];
  $('subGpu').textContent = g.name;
  gpuChart.push([g.util]);
  $('gpuUtil').textContent = g.util + '%';
  $('gpuTemp').textContent = g.temp + '°C';
  $('gpuPower').textContent = g.power + ' W';
  $('gpuClk').textContent = g.clk_sm + ' MHz';
  // NVML reports -1 when the card exposes no fan (e.g. passively cooled).
  if (g.fan >= 0) {
    $('gpuFanRow').style.display = '';
    $('gpuFan').textContent = g.fan + '%';
  } else {
    $('gpuFanRow').style.display = 'none';
  }
  $('gpuVramCap').textContent = 'VRAM · ' + fmtBytes(g.mem_used) + ' / ' + fmtBytes(g.mem_total);
  $('gpuVramBar').style.width = (g.mem_total ? (g.mem_used / g.mem_total) * 100 : 0) + '%';
}
```

- [ ] **Step 4: Call them, and seed their charts**

In `web/app.js`, in `applySnap`, replace the `renderCPU(s);` line with:

```js
  renderCPU(s);
  renderMem(s);
  if (hasGPU && s.gpu.length) renderGPU(s);
```

In `seedHistory`, after the `cpuChart.seed(...)` line, add:

```js
  memChart.seed(past.map(x => [x.mem.pct, x.mem.swap_pct]));
  if (gpuChart) gpuChart.seed(past.map(x => [x.gpu && x.gpu[0] ? x.gpu[0].util : 0]));
```

- [ ] **Step 5: Run and capture**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
SD=/tmp/claude-1000/-media-xuanlocserver-DellEMC12T-workingspace-system-monitor-service/a07d4590-3c47-460d-a995-3bf9b12d59dd/scratchpad
go build -o $SD/sm-web ./cmd/web
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 3
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1440,863 --screenshot=$SD/t7.png http://localhost:8090/
kill %1
```

Read `$SD/t7.png`. Expected: Memory subtitle shows real RAM and swap totals; its chart has two filled bands whose heights match the stated percentages; the RAM breakdown bar's segments sum to under 100%. GPU card shows a real card name and a populated VRAM bar. Confirm the CPU, Memory and GPU plot areas begin and end at exactly the same x.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/cards.js web/app.js
git commit -m "feat(web): Memory and GPU cards

Memory plots mem% and swap% filled on a fixed 0-100 axis, so the chart
agrees with the numbers beside it instead of flatlining. GPU drops the
fan row when NVML reports -1."
```

---

### Task 8: Frontend — Network and Disk cards

The Network card is where the source design duplicated `Max: 1.26 MiB/s` on two rows. Its replacement gives RX and TX each their own peak — computed client-side from the chart's own 60-point window, which is why no new backend field is needed. The Disk card restores per-device `%util`, which the design dropped and the user explicitly requires.

**Files:**
- Modify: `web/index.html` (fill `#netStats`, `#dskStats`)
- Modify: `web/cards.js` (add `renderNet`, `renderDisk`)
- Modify: `web/app.js` (call them, seed their charts)

**Interfaces:**
- Consumes: `fmtRate`, `fmtBytes`, `esc`, `peak`, `netChart.data`, `diskChart.data`.
- Produces: `renderNet(s)`, `renderDisk(s)`.

- [ ] **Step 1: Fill in the Network stats column**

In `web/index.html`, replace `<div class="stats" id="netStats"></div>` with:

```html
          <div class="stats" id="netStats">
            <div class="r"><span class="k"><i class="dot sw-blue"></i>Receiving ↓</span><span class="v" id="netRx">—</span></div>
            <div class="r dim"><span class="k"><i class="dot hole"></i>peak 1 min</span><span class="v" id="netRxPeak">—</span></div>
            <div class="r"><span class="k"><i class="dot sw-amber"></i>Sending ↑</span><span class="v" id="netTx">—</span></div>
            <div class="r dim"><span class="k"><i class="dot hole"></i>peak 1 min</span><span class="v" id="netTxPeak">—</span></div>
            <div class="sep"></div>
            <div class="r"><span class="k"><i class="dot hole"></i>Total received</span><span class="v" id="netRxTot">—</span></div>
            <div class="r"><span class="k"><i class="dot hole"></i>Total sent</span><span class="v" id="netTxTot">—</span></div>
          </div>
```

- [ ] **Step 2: Fill in the Disk stats column**

Replace `<div class="stats" id="dskStats"></div>` with:

```html
          <div class="stats" id="dskStats">
            <div class="r"><span class="k"><i class="dot sw-cyan"></i>Reading ↓</span><span class="v" id="dskR">—</span></div>
            <div class="r"><span class="k"><i class="dot sw-purple"></i>Writing ↑</span><span class="v" id="dskW">—</span></div>
            <div class="sep"></div>
            <div class="cap">Utilisation per device</div>
            <div id="devList"></div>
          </div>
```

- [ ] **Step 3: Write renderNet and renderDisk**

Append to `web/cards.js`:

```js
function renderNet(s) {
  // Real interface names — the design's "enp5s0 - 1 Gb/s" invented a link
  // speed the app does not collect.
  $('subNet').textContent = s.net.ifaces.length ? s.net.ifaces.join(' + ') : 'no interface';
  netChart.push([s.net.rx, s.net.tx]);
  $('netRx').textContent = fmtRate(s.net.rx);
  $('netTx').textContent = fmtRate(s.net.tx);
  // Peaks come from the chart's own rolling window, so RX and TX get their own
  // number instead of the design's duplicated "Max" row.
  $('netRxPeak').textContent = fmtRate(peak(netChart.data[0]));
  $('netTxPeak').textContent = fmtRate(peak(netChart.data[1]));
  $('netRxTot').textContent = fmtBytes(s.net.rx_total);
  $('netTxTot').textContent = fmtBytes(s.net.tx_total);
}

function renderDisk(s) {
  const devs = s.disk.devs.slice().sort((a, b) => b.util - a.util);
  $('subDisk').textContent = devs.length + (devs.length === 1 ? ' device' : ' devices');
  diskChart.push([s.disk.read, s.disk.write]);
  $('dskR').textContent = fmtRate(s.disk.read);
  $('dskW').textContent = fmtRate(s.disk.write);
  // Sorted descending: the device that matters is always the top row, which is
  // also what makes this degrade gracefully with many devices.
  $('devList').innerHTML = devs.map(d =>
    `<div class="dev"><span class="nm" title="${esc(d.model || d.name)}">${esc(d.name)}</span>` +
    `<span class="bar"><i class="${d.util > 75 ? 'hot' : ''}" style="width:${d.util.toFixed(0)}%"></i></span>` +
    `<span class="pc">${d.util.toFixed(0)}%</span></div>`).join('');
}
```

- [ ] **Step 4: Call them, and seed their charts**

In `web/app.js`, in `applySnap`, extend the render block:

```js
  renderCPU(s);
  renderMem(s);
  if (hasGPU && s.gpu.length) renderGPU(s);
  renderNet(s);
  renderDisk(s);
```

In `seedHistory`, after the `memChart.seed(...)` line, add:

```js
  netChart.seed(past.map(x => [x.net.rx, x.net.tx]));
  diskChart.seed(past.map(x => [x.disk.read, x.disk.write]));
```

- [ ] **Step 5: Run and capture, with some load**

Generate disk and network activity so the cards are not all zeroes:

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
SD=/tmp/claude-1000/-media-xuanlocserver-DellEMC12T-workingspace-system-monitor-service/a07d4590-3c47-460d-a995-3bf9b12d59dd/scratchpad
go build -o $SD/sm-web ./cmd/web
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 1
dd if=/dev/zero of=$SD/load.bin bs=1M count=800 oflag=direct 2>/dev/null &
sleep 4
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1440,863 --screenshot=$SD/t8.png http://localhost:8090/
kill %1; rm -f $SD/load.bin
```

Read `$SD/t8.png`. Expected: the Network subtitle names real interfaces (no link speed); RX and TX peaks are **different numbers**; the Y gutters on Network and Disk are labelled with real rates, not `—`; the Disk card lists every device with its own utilisation bar, highest first. Verify all five plot areas still share the same left and right edges.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/cards.js web/app.js
git commit -m "feat(web): Network and Disk cards

Network gives RX and TX their own peak, from the chart's rolling window,
replacing the design's duplicated Max row. Disk restores per-device
%util, sorted so the busiest device is always the top row."
```

---

### Task 9: Frontend — right column, auto-fit, and enough rows to fit

The tables never scroll: they render every row they were sent, then trim from the bottom until they fit. Because both lists are pre-sorted, what survives is always the most important rows.

The client can only trim rows it was sent, so `PROC_TOP_N` rises from 8 to 25. **`docker-compose.yml` hardcodes `PROC_TOP_N=8`**, and the environment overrides the Go default — changing `config.go` alone would do nothing for the web app.

**Files:**
- Modify: `web/cards.js` (add `renderProc`, `renderFS`, `autoFit`)
- Modify: `web/app.js` (call them, bind resize)
- Modify: `internal/config/config.go:65`
- Modify: `docker-compose.yml:12`
- Modify: `README.md` (if it documents the default)

**Interfaces:**
- Consumes: `esc`, `fmtBytes`, the `[data-fit]` wrappers and `#fsNote` from Task 5's markup.
- Produces: `renderProc(s)`, `renderFS(s)`, `autoFit()`.

- [ ] **Step 1: Write the renderers and the fit pass**

Append to `web/cards.js`:

```js
function renderProc(s) {
  // Already sorted by CPU descending server-side. No per-process icons: they
  // carried no information the name doesn't.
  $('procBody').innerHTML = s.proc.map(p =>
    `<tr><td class="nm" title="${esc(p.name)}">${esc(p.name)}</td>` +
    `<td class="n">${p.cpu.toFixed(0)}%</td>` +
    `<td class="n">${fmtBytes(p.rss)}</td>` +
    `<td class="n">${p.pid}</td></tr>`).join('');
}

function renderFS(s) {
  // Sorted by % used descending — the full mount is the one worth seeing.
  const fs = s.fs.slice().sort((a, b) => b.pct - a.pct);
  $('fsBody').innerHTML = fs.map(f =>
    `<tr><td class="nm" title="${esc(f.mount)}">${esc(f.mount)}</td>` +
    `<td class="n">${fmtBytes(f.used)} / ${fmtBytes(f.total)}</td>` +
    `<td class="n"><span class="fsbar"><i class="${f.pct > 90 ? 'hot' : ''}" style="width:${f.pct.toFixed(0)}%"></i></span></td>` +
    `<td class="n">${f.pct.toFixed(0)}%</td></tr>`).join('');
}

// Trim rows from the bottom until each table fits its wrapper, so the right
// column adapts to the window height without ever scrolling. The loop only
// ever deletes, so it terminates. Both lists are pre-sorted, so the rows that
// survive are the ones worth keeping.
function autoFit() {
  document.querySelectorAll('[data-fit]').forEach(wrap => {
    const tb = wrap.querySelector('tbody');
    if (!tb) return;
    const total = tb.rows.length;
    while (tb.rows.length > 1 && wrap.scrollHeight > wrap.clientHeight) {
      tb.deleteRow(tb.rows.length - 1);
    }
    if (wrap.dataset.fit === 'fs') {
      const hidden = total - tb.rows.length;
      // Never truncate silently. #fsNote reserves its height even when empty,
      // so writing into it cannot re-overflow the table just trimmed to fit.
      $('fsNote').textContent = hidden > 0 ? `+${hidden} mount khác` : '';
    }
  });
}
```

- [ ] **Step 2: Call them**

In `web/app.js`, in `applySnap`, extend the render block and add the fit pass:

```js
  renderCPU(s);
  renderMem(s);
  if (hasGPU && s.gpu.length) renderGPU(s);
  renderNet(s);
  renderDisk(s);
  renderProc(s);
  renderFS(s);
  autoFit();
```

At the end of `web/app.js`, before `connect();`, add:

```js
// Row counts are height-dependent, so refit when the window changes.
window.addEventListener('resize', autoFit);
```

- [ ] **Step 3: Raise the process count**

In `internal/config/config.go`, change the `ProcTopN` line:

```go
		ProcTopN:       envInt("PROC_TOP_N", 25),
```

In `docker-compose.yml`, change the environment entry:

```yaml
      - PROC_TOP_N=25
```

Then check whether the README documents the old default and update it if so:

```bash
grep -rn "PROC_TOP_N" README.md docs/ --include='*.md'
```

Update any line that states the default is 8. Leave the spec and this plan alone — they describe the change itself.

- [ ] **Step 4: Verify the backend still passes**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
go build ./... && go test ./internal/...
```

Expected: PASS.

- [ ] **Step 5: Run and capture at two heights**

The point of auto-fit is that the row count changes with height, so capture both:

```bash
SD=/tmp/claude-1000/-media-xuanlocserver-DellEMC12T-workingspace-system-monitor-service/a07d4590-3c47-460d-a995-3bf9b12d59dd/scratchpad
go build -o $SD/sm-web ./cmd/web
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 3
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1920,1080 --screenshot=$SD/t9-tall.png http://localhost:8090/
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1100,780 --screenshot=$SD/t9-floor.png http://localhost:8090/
kill %1
```

Read both. Expected: the tall capture shows **more** process rows than the floor capture; neither shows a scrollbar or a row clipped mid-height; no table has an empty gap below its last row large enough for another row. If Filesystems trimmed any mounts, `+N mount khác` appears; if it trimmed none, the note is empty.

- [ ] **Step 6: Commit**

```bash
git add web/cards.js web/app.js internal/config/config.go docker-compose.yml README.md
git commit -m "feat(web): auto-fitting Top Processes and Filesystems

Tables render every row then trim from the bottom to fit, so the right
column adapts to window height with no scrollbar. Filesystems sorts by
usage and says how many mounts it dropped. PROC_TOP_N 8 -> 25 so there
are enough rows to trim -- in compose too, since the env overrides the
Go default."
```

---

### Task 10: Visual verification loop

Building to spec is not the same as looking right. This task is the review the user asked for: run the real app on real data, look at it, and fix what is actually wrong. It is not a formality — expect to find things and to iterate.

**Files:**
- Modify: whichever `web/` files the findings implicate.
- Temporarily modify, never commit: `internal/collect/gpu.go`, `internal/collect/collector.go` (Step 5 only, reverted in the same step).

**Interfaces:** none — this task changes no contracts.

- [ ] **Step 1: Serve the app on live data**

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin && export CGO_ENABLED=1
SD=/tmp/claude-1000/-media-xuanlocserver-DellEMC12T-workingspace-system-monitor-service/a07d4590-3c47-460d-a995-3bf9b12d59dd/scratchpad
mkdir -p $SD/shots
go build -o $SD/sm-web ./cmd/web
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 3
```

- [ ] **Step 2: Capture all three viewports**

Do **not** pass `--hide-scrollbars` — a scrollbar appearing is one of the things being checked for.

```bash
for vp in 1440,863 1100,780 1920,1080; do
  google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
    --window-size=$vp --screenshot=$SD/shots/${vp/,/x}.png http://localhost:8090/
done
ls -l $SD/shots/
```

- [ ] **Step 3: Read every capture and judge it**

Read all three PNGs. Each of these fails the round:

1. A scrollbar at any viewport.
2. The left and right columns ending at different heights.
3. The five plot areas not starting and ending at the same x.
4. A table row clipped mid-height, or a gap below the last row big enough for another row.
5. Gridlines invisible against white.
6. Any value that is a placeholder, a dash where a number was promised, or a duplicate of a neighbouring row.

Then judge it as a designer, not just a checklist: spacing, alignment, visual weight, whether the five cards actually read as a set, whether anything is too cramped at the floor or too empty at 1920×1080.

- [ ] **Step 4: Fix and re-capture**

Fix every finding, rebuild, and repeat Steps 2–3. **Repeat until a round produces no findings.** Report each round's findings rather than only the final state — a fix that needed three rounds is worth knowing about.

- [ ] **Step 5: Exercise the three empty states**

This machine has a GPU, a readable CPU temperature and a fan, so the three conditional paths never fire on their own. Force them with temporary edits, capture, then revert. These edits are **never committed**.

First, the no-GPU path. In `internal/collect/gpu.go`, make `NewGPUReader()` return the nop reader unconditionally by adding one line as the first statement of its body:

```go
func NewGPUReader() GPUReader {
	return nopGPU{} // TEMPORARY — revert before committing
```

Then the two hidden-row paths. In `internal/collect/collector.go`, in `Tick`, temporarily replace the Temp assignment and force the fan to NVML's "no fan" value:

```go
	snap.CPU.Temp = 0 // TEMPORARY — revert before committing
	snap.CPU.Model = c.cpuModel
	if len(snap.GPU) > 0 {
		snap.GPU[0].Fan = -1 // TEMPORARY — revert before committing
	}
```

Capture each state separately — the GPU edit removes the card that the fan edit is testing, so applying both at once would hide the fan result:

```bash
# no GPU: apply only the gpu.go edit
go build -o $SD/sm-web ./cmd/web && HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 3
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1440,863 --screenshot=$SD/shots/empty-nogpu.png http://localhost:8090/
kill %1
git checkout -- internal/collect/gpu.go
# hidden rows: apply only the collector.go edits
go build -o $SD/sm-web ./cmd/web && HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8090 $SD/sm-web &
sleep 3
google-chrome --headless --disable-gpu --no-sandbox --virtual-time-budget=8000 \
  --window-size=1440,863 --screenshot=$SD/shots/empty-rows.png http://localhost:8090/
kill %1
git checkout -- internal/collect/collector.go
```

Read both. Expected in `empty-nogpu.png`: **four** left cards, evenly sharing the full column height, no GPU card and no gap where it was; the right column still ends level with the left. Expected in `empty-rows.png`: the CPU card has no Temperature row and the GPU card has no Fan row, with neither leaving a blank gap.

Confirm the revert before going on — a stray `return nopGPU{}` would silently disable GPU support for good:

```bash
git status --short internal/collect/
```

Expected: **empty output**.

- [ ] **Step 6: Check what the 32-line CPU chart costs**

The spec flags this as a risk: the CPU chart redraws 32 paths × 60 points every second, 32× the old chart's work. Measure it rather than assume. With the desktop app running (Step 7 below), sample the renderer process:

```bash
for i in 1 2 3 4 5; do ps -eo pcpu,comm | grep -i webkit | head -3; echo '--'; sleep 2; done
```

A few percent is the expected result and needs no action. If the renderer is sustaining tens of percent, apply the spec's mitigation — draw every other core — rather than dropping the design.

- [ ] **Step 7: Confirm in the real desktop window**

Headless Chrome is not WebKitGTK, and it cannot test the GTK size hint. Both need the real shell:

```bash
go build -o $SD/sm-desktop ./cmd/desktop
SM_AUTOCLOSE_MS=20000 $SD/sm-desktop
```

While it is open: confirm the layout matches the browser captures, then **try to drag the window smaller than 1100×780** and confirm GTK refuses. If a window manager ignores the hint, say so rather than assuming it held.

- [ ] **Step 8: Stop the server and clean up**

```bash
kill %1 2>/dev/null
rm -f $SD/sm-web $SD/sm-desktop
```

- [ ] **Step 9: Commit any fixes**

```bash
git add web/
git commit -m "fix(web): visual review corrections

Findings from the headless-Chrome review at 1440x863, 1100x780 and
1920x1080, plus a pass in the real WebKitGTK window."
```

(Skip if the first round was clean — but say so explicitly rather than committing an empty change.)

---

## Notes on testing strategy

The Go changes are TDD'd because they are parsing logic with real edge cases (a missing `PRETTY_NAME`, an ARM kernel with no `model name`, an unreadable file) — exactly what unit tests are for, and the pattern `internal/collect/*_test.go` already follows.

The frontend deliberately gets **no JS unit test harness**. The repo has none today, adding one is new infrastructure the spec did not ask for, and the failures that actually matter here are visual — a chart that lays out wrong, a column that drifts, a gridline that vanishes into a white background. No assertion catches those. The headless-Chrome loop in Task 10 does, which is why it is a required task and not a nicety. Each frontend task also ends with its own capture, so a regression is caught at the task that caused it rather than at the end.
