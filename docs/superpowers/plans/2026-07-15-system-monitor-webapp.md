# System Monitor Webapp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lightweight web dashboard that mimics GNOME System Monitor's Resources tab, updating in real time, with per-disk %util and a GPU panel, packaged as a single Docker Compose service that fits a 1080p screen with no scrolling.

**Architecture:** A single Go binary reads host metrics from bind-mounted `/host/proc` and `/host/sys` plus NVML, on a 1s ticker. It computes all deltas once per tick, keeps a rolling history ring, and broadcasts each snapshot over a WebSocket to all clients. The same binary serves an embedded vanilla-JS frontend that renders canvas charts.

**Tech Stack:** Go 1.23 (stdlib + `github.com/gorilla/websocket` + `github.com/NVIDIA/go-nvml`), vanilla HTML/CSS/JS with a custom canvas chart, Docker multi-stage build (distroless/base runtime), Docker Compose with `runtime: nvidia` + `network_mode: host`.

## Global Constraints

- **Language/version:** Go 1.23. Module path `system-monitor`.
- **External deps (only these):** `github.com/gorilla/websocket`, `github.com/NVIDIA/go-nvml`.
- **Host access paths (env-configurable, never hard-coded):** `HOST_PROC=/host/proc`, `HOST_SYS=/host/sys`, `HOST_ROOT=/host/root`.
- **Runtime discovery:** core count, disk set, mount set, and GPU count MUST be discovered at runtime, never hard-coded.
- **Disk %util formula (verbatim):** `%util = Δio_ticks_ms / Δt_ms × 100`, clamped to [0,100], per whole disk. `io_ticks` = field 13 (1-based) of `/proc/diskstats` (index 12 in `strings.Fields`).
- **Disk filter:** enumerate `/host/sys/block`, exclude name prefixes in `DISK_EXCLUDE` (default `loop,ram,zram,dm-`); this yields whole disks only (partitions are not top-level entries).
- **CPU%:** per line, `busy = total - (idle + iowait)`; `pct = Δbusy/Δtotal × 100`, clamped [0,100].
- **USER_HZ = 100, PAGE_SIZE = 4096** (Linux x86_64 constants; used for process CPU% and RSS).
- **Units:** all byte quantities in JSON are **bytes**; rates are **bytes/second**; temps **°C**; power **watts**; clocks **MHz**. Frontend formats for display.
- **Update tick:** default `INTERVAL_MS=1000`; history window default `HISTORY_SECONDS=60`; `PROC_TOP_N=8`.
- **No auth.** Internal use only.
- **Resource targets:** container RAM < 30 MB, CPU < 1–2% of one core, image < ~35 MB.
- **First tick has no previous sample → all rates and %util report 0.**
- **Commit after every task.** TDD: write the failing test first.

---

## File Structure

```
system_monitor_service/
  go.mod  go.sum
  cmd/monitor/main.go                 # wire config -> server, run
  internal/config/config.go           # env parsing (+ _test.go)
  internal/model/snapshot.go          # shared JSON structs (no logic)
  internal/collect/
    cpu.go   cpu_test.go              # /proc/stat parse + percent
    mem.go   mem_test.go              # /proc/meminfo parse
    net.go   net_test.go              # /proc/net/dev parse + iface filter
    disk.go  disk_test.go             # /proc/diskstats parse + %util + disk list
    fs.go    fs_test.go               # /proc/mounts parse + statfs wrapper
    proc.go  proc_test.go             # /proc/[pid]/stat parse + scan
    temp.go  temp_test.go             # /sys/class/hwmon CPU temp
    gpu.go                            # GPUReader interface + NVML impl + nop
    collector.go collector_test.go    # orchestrates one tick -> model.Snapshot
  internal/server/
    hub.go   hub_test.go              # broadcast to clients
    server.go                         # ticker loop, ring buffer, http routes
    ws.go                             # websocket upgrade + per-client pump
  web/
    index.html  style.css  app.js  chart.js   # embedded via embed.FS
  Dockerfile
  docker-compose.yml
  .dockerignore
```

Each collector is an isolated unit: input = an `io.Reader`/dir path (fixture-able), output = a typed value. No collector imports another. `collector.go` owns previous-sample state and delta math.

---

### Task 1: Scaffold — module, config, model

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/model/snapshot.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` struct + `config.Load() Config`.
- Produces: all `model.*` structs (see below) consumed by every later task.

- [ ] **Step 1: Create the Go module**

Run:
```bash
cd /media/xuanlocserver/DellEMC12T/workingspace/system_monitor_service
go mod init system-monitor
go mod edit -go=1.23
```

- [ ] **Step 2: Write the failing config test**

Create `internal/config/config_test.go`:
```go
package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "INTERVAL_MS", "HISTORY_SECONDS", "PROC_TOP_N", "HOST_PROC"} {
		os.Unsetenv(k)
	}
	c := Load()
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080", c.Port)
	}
	if c.IntervalMS != 1000 {
		t.Errorf("IntervalMS = %d, want 1000", c.IntervalMS)
	}
	if c.HostProc != "/host/proc" {
		t.Errorf("HostProc = %q, want /host/proc", c.HostProc)
	}
	if len(c.DiskExclude) != 4 {
		t.Errorf("DiskExclude len = %d, want 4", len(c.DiskExclude))
	}
}

func TestLoadOverride(t *testing.T) {
	os.Setenv("INTERVAL_MS", "500")
	defer os.Unsetenv("INTERVAL_MS")
	if got := Load().IntervalMS; got != 500 {
		t.Errorf("IntervalMS = %d, want 500", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/`
Expected: FAIL (package has no `Load`/`Config`).

- [ ] **Step 4: Implement config**

Create `internal/config/config.go`:
```go
package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port           string
	IntervalMS     int
	HistorySec     int
	ProcTopN       int
	ProcIntervalMS int
	HostProc       string
	HostSys        string
	HostRoot       string
	DiskExclude    []string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() Config {
	interval := envInt("INTERVAL_MS", 1000)
	return Config{
		Port:           env("PORT", "8080"),
		IntervalMS:     interval,
		HistorySec:     envInt("HISTORY_SECONDS", 60),
		ProcTopN:       envInt("PROC_TOP_N", 8),
		ProcIntervalMS: envInt("PROC_INTERVAL_MS", interval),
		HostProc:       env("HOST_PROC", "/host/proc"),
		HostSys:        env("HOST_SYS", "/host/sys"),
		HostRoot:       env("HOST_ROOT", "/host/root"),
		DiskExclude:    strings.Split(env("DISK_EXCLUDE", "loop,ram,zram,dm-"), ","),
	}
}
```

- [ ] **Step 5: Create the model structs**

Create `internal/model/snapshot.go`:
```go
package model

// Snapshot is one full sample broadcast each tick. All byte fields are bytes,
// rates are bytes/second.
type Snapshot struct {
	T    int64      `json:"t"` // unix seconds
	Host HostInfo   `json:"host"`
	CPU  CPUInfo    `json:"cpu"`
	Mem  MemInfo    `json:"mem"`
	Net  NetInfo    `json:"net"`
	Disk DiskInfo   `json:"disk"`
	GPU  []GPUInfo  `json:"gpu"`
	FS   []FSInfo   `json:"fs"`
	Proc []ProcInfo `json:"proc"`
}

type HostInfo struct {
	Name   string     `json:"name"`
	Uptime int64      `json:"uptime"`
	Load   [3]float64 `json:"load"`
}

type CPUInfo struct {
	Agg   float64   `json:"agg"`
	Cores []float64 `json:"cores"`
	Temp  float64   `json:"temp"` // 0 if unknown
}

type MemInfo struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Cache     uint64  `json:"cache"`
	Pct       float64 `json:"pct"`
	SwapTotal uint64  `json:"swap_total"`
	SwapUsed  uint64  `json:"swap_used"`
	SwapPct   float64 `json:"swap_pct"`
}

type NetInfo struct {
	RX      uint64 `json:"rx"`
	TX      uint64 `json:"tx"`
	RXTotal uint64 `json:"rx_total"`
	TXTotal uint64 `json:"tx_total"`
}

type DiskDev struct {
	Name  string  `json:"name"`
	Util  float64 `json:"util"`
	Read  uint64  `json:"read"`
	Write uint64  `json:"write"`
	Model string  `json:"model"`
}

type DiskInfo struct {
	Read       uint64    `json:"read"`
	Write      uint64    `json:"write"`
	ReadTotal  uint64    `json:"read_total"`
	WriteTotal uint64    `json:"write_total"`
	Devs       []DiskDev `json:"devs"`
}

type GPUInfo struct {
	Name     string `json:"name"`
	Util     int    `json:"util"`
	MemUsed  uint64 `json:"mem_used"`  // bytes
	MemTotal uint64 `json:"mem_total"` // bytes
	Temp     int    `json:"temp"`
	Power    int    `json:"power"`  // watts
	ClkSM    int    `json:"clk_sm"` // MHz
	Fan      int    `json:"fan"`    // percent, -1 if N/A
}

type FSInfo struct {
	Mount string  `json:"mount"`
	Used  uint64  `json:"used"`
	Total uint64  `json:"total"`
	Pct   float64 `json:"pct"`
}

type ProcInfo struct {
	PID  int     `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"` // percent of one core (can exceed 100)
	RSS  uint64  `json:"rss"` // bytes
}
```

- [ ] **Step 6: Run tests and build**

Run: `go test ./... && go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 7: Commit**

```bash
git add go.mod internal/config internal/model
git commit -m "feat: scaffold module, config, and model structs"
```

---

### Task 2: CPU collector

**Files:**
- Create: `internal/collect/cpu.go`
- Test: `internal/collect/cpu_test.go`

**Interfaces:**
- Produces: `CPUTimes{Total, Idle uint64}`; `ParseCPUStat(io.Reader) (agg CPUTimes, cores []CPUTimes, err error)`; `CPUPercent(prev, cur CPUTimes) float64`; `ReadCPUStat(hostProc string) (CPUTimes, []CPUTimes, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/cpu_test.go`:
```go
package collect

import (
	"strings"
	"testing"
)

func TestParseCPUStat(t *testing.T) {
	in := "cpu  100 0 50 800 50 0 0 0 0 0\n" +
		"cpu0 40 0 20 400 40 0 0 0 0 0\n" +
		"cpu1 60 0 30 400 10 0 0 0 0 0\n" +
		"intr 12345\n"
	agg, cores, err := ParseCPUStat(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	// agg total = 100+0+50+800+50 = 1000; idle = idle(800)+iowait(50) = 850
	if agg.Total != 1000 || agg.Idle != 850 {
		t.Fatalf("agg = %+v, want {1000 850}", agg)
	}
	if len(cores) != 2 {
		t.Fatalf("cores len = %d, want 2", len(cores))
	}
}

func TestCPUPercent(t *testing.T) {
	prev := CPUTimes{Total: 1000, Idle: 850}
	cur := CPUTimes{Total: 1100, Idle: 900} // dt=100, didle=50, busy=50 -> 50%
	if got := CPUPercent(prev, cur); got != 50 {
		t.Errorf("CPUPercent = %v, want 50", got)
	}
}

func TestCPUPercentZeroDelta(t *testing.T) {
	s := CPUTimes{Total: 1000, Idle: 850}
	if got := CPUPercent(s, s); got != 0 {
		t.Errorf("CPUPercent = %v, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run CPU`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement**

Create `internal/collect/cpu.go`:
```go
package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type CPUTimes struct {
	Total uint64
	Idle  uint64
}

// ParseCPUStat parses /proc/stat cpu lines into cumulative totals.
// idle counts the idle (field 4) + iowait (field 5) columns.
func ParseCPUStat(r io.Reader) (agg CPUTimes, cores []CPUTimes, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		var t CPUTimes
		for i := 1; i < len(f); i++ {
			v, e := strconv.ParseUint(f[i], 10, 64)
			if e != nil {
				continue
			}
			t.Total += v
			if i == 4 || i == 5 { // idle, iowait
				t.Idle += v
			}
		}
		if f[0] == "cpu" {
			agg = t
		} else {
			cores = append(cores, t)
		}
	}
	return agg, cores, sc.Err()
}

func CPUPercent(prev, cur CPUTimes) float64 {
	dt := cur.Total - prev.Total
	if dt == 0 {
		return 0
	}
	di := cur.Idle - prev.Idle
	pct := float64(dt-di) / float64(dt) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func ReadCPUStat(hostProc string) (CPUTimes, []CPUTimes, error) {
	f, err := os.Open(filepath.Join(hostProc, "stat"))
	if err != nil {
		return CPUTimes{}, nil, err
	}
	defer f.Close()
	return ParseCPUStat(f)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run CPU -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/cpu.go internal/collect/cpu_test.go
git commit -m "feat: CPU collector (/proc/stat parse + percent)"
```

---

### Task 3: Memory collector

**Files:**
- Create: `internal/collect/mem.go`
- Test: `internal/collect/mem_test.go`

**Interfaces:**
- Produces: `ParseMeminfo(io.Reader) (model.MemInfo, error)`; `ReadMeminfo(hostProc string) (model.MemInfo, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/mem_test.go`:
```go
package collect

import (
	"strings"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	in := "MemTotal:       1000 kB\n" +
		"MemAvailable:    600 kB\n" +
		"Buffers:          50 kB\n" +
		"Cached:          200 kB\n" +
		"SReclaimable:     50 kB\n" +
		"SwapTotal:       800 kB\n" +
		"SwapFree:        300 kB\n"
	m, err := ParseMeminfo(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 1000*1024 || m.Used != 400*1024 {
		t.Errorf("Total=%d Used=%d", m.Total, m.Used)
	}
	if m.Cache != 300*1024 { // 50+200+50
		t.Errorf("Cache=%d, want %d", m.Cache, 300*1024)
	}
	if m.SwapUsed != 500*1024 {
		t.Errorf("SwapUsed=%d, want %d", m.SwapUsed, 500*1024)
	}
	if m.Pct != 40 {
		t.Errorf("Pct=%v, want 40", m.Pct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run Meminfo`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/collect/mem.go`:
```go
package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"system-monitor/internal/model"
)

func ParseMeminfo(r io.Reader) (model.MemInfo, error) {
	v := map[string]uint64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		v[strings.TrimSuffix(f[0], ":")] = n * 1024 // kB -> bytes
	}
	m := model.MemInfo{
		Total:     v["MemTotal"],
		Cache:     v["Cached"] + v["Buffers"] + v["SReclaimable"],
		SwapTotal: v["SwapTotal"],
	}
	if m.Total >= v["MemAvailable"] {
		m.Used = m.Total - v["MemAvailable"]
	}
	if m.SwapTotal >= v["SwapFree"] {
		m.SwapUsed = m.SwapTotal - v["SwapFree"]
	}
	if m.Total > 0 {
		m.Pct = float64(m.Used) / float64(m.Total) * 100
	}
	if m.SwapTotal > 0 {
		m.SwapPct = float64(m.SwapUsed) / float64(m.SwapTotal) * 100
	}
	return m, sc.Err()
}

func ReadMeminfo(hostProc string) (model.MemInfo, error) {
	f, err := os.Open(filepath.Join(hostProc, "meminfo"))
	if err != nil {
		return model.MemInfo{}, err
	}
	defer f.Close()
	return ParseMeminfo(f)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run Meminfo -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/mem.go internal/collect/mem_test.go
git commit -m "feat: memory collector (/proc/meminfo parse)"
```

---

### Task 4: Network collector

**Files:**
- Create: `internal/collect/net.go`
- Test: `internal/collect/net_test.go`

**Interfaces:**
- Produces: `NetCounters{RX, TX uint64}`; `ParseNetDev(io.Reader) NetCounters` (sums non-virtual ifaces); `ReadNetDev(hostProc string) (NetCounters, error)`.
- Virtual iface prefixes excluded: `lo`, `docker`, `veth`, `br-`, `virbr`, `vnet`.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/net_test.go`:
```go
package collect

import (
	"strings"
	"testing"
)

func TestParseNetDev(t *testing.T) {
	in := "Inter-|   Receive                    |  Transmit\n" +
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets\n" +
		"    lo:  1000    10 0 0 0 0 0 0  1000 10 0 0 0 0 0 0\n" +
		"  eth0:  5000    50 0 0 0 0 0 0  2000 20 0 0 0 0 0 0\n" +
		" veth1:  9999    99 0 0 0 0 0 0  9999 99 0 0 0 0 0 0\n"
	c := ParseNetDev(strings.NewReader(in))
	if c.RX != 5000 || c.TX != 2000 { // only eth0 counted
		t.Errorf("got RX=%d TX=%d, want 5000/2000", c.RX, c.TX)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run NetDev`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/collect/net.go`:
```go
package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type NetCounters struct {
	RX uint64
	TX uint64
}

var netExclude = []string{"lo", "docker", "veth", "br-", "virbr", "vnet"}

func isVirtualIface(name string) bool {
	for _, p := range netExclude {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ParseNetDev sums rx/tx bytes over physical interfaces. In /proc/net/dev the
// value after the interface colon has rx-bytes at index 0 and tx-bytes at index 8.
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
	}
	return c
}

func ReadNetDev(hostProc string) (NetCounters, error) {
	f, err := os.Open(filepath.Join(hostProc, "net", "dev"))
	if err != nil {
		return NetCounters{}, err
	}
	defer f.Close()
	return ParseNetDev(f), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run NetDev -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/net.go internal/collect/net_test.go
git commit -m "feat: network collector (/proc/net/dev parse + iface filter)"
```

---

### Task 5: Disk collector

**Files:**
- Create: `internal/collect/disk.go`
- Test: `internal/collect/disk_test.go`

**Interfaces:**
- Produces: `DiskCounters{SectorsRead, SectorsWritten, IOTicks uint64}`; `ParseDiskstats(io.Reader, keep map[string]bool) map[string]DiskCounters`; `ListDisks(hostSys string, exclude []string) ([]string, error)`; `DiskModel(hostSys, name string) string`; `DiskUtil(prevTicks, curTicks uint64, dtMS float64) float64`.
- diskstats indexes (0-based `Fields`): 2=name, 5=sectors_read, 9=sectors_written, 12=io_ticks. Sector size = 512 bytes.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/disk_test.go`:
```go
package collect

import (
	"strings"
	"testing"
)

func TestParseDiskstats(t *testing.T) {
	// major minor name reads rmerg sread msread writes wmerg swrit mswrit inprog ioticks weighted
	in := "   8       0 sda 100 0 2000 0 50 0 1000 0 0 500 0\n" +
		" 259       0 nvme0n1 10 0 200 0 5 0 100 0 0 20 0\n" +
		"   7       0 loop0 1 0 2 0 0 0 0 0 0 0 0\n"
	keep := map[string]bool{"sda": true, "nvme0n1": true}
	m := ParseDiskstats(strings.NewReader(in), keep)
	if len(m) != 2 {
		t.Fatalf("len=%d want 2 (loop0 excluded)", len(m))
	}
	if m["sda"].SectorsRead != 2000 || m["sda"].SectorsWritten != 1000 || m["sda"].IOTicks != 500 {
		t.Errorf("sda = %+v", m["sda"])
	}
}

func TestDiskUtil(t *testing.T) {
	// 500ms of io over a 1000ms interval -> 50%
	if got := DiskUtil(1000, 1500, 1000); got != 50 {
		t.Errorf("DiskUtil = %v, want 50", got)
	}
	// clamp: 1200ms io over 1000ms -> 100
	if got := DiskUtil(0, 1200, 1000); got != 100 {
		t.Errorf("DiskUtil = %v, want 100", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run Disk`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/collect/disk.go`:
```go
package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const sectorSize = 512

type DiskCounters struct {
	SectorsRead    uint64
	SectorsWritten uint64
	IOTicks        uint64
}

func ParseDiskstats(r io.Reader, keep map[string]bool) map[string]DiskCounters {
	out := map[string]DiskCounters{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 13 {
			continue
		}
		name := f[2]
		if !keep[name] {
			continue
		}
		sr, _ := strconv.ParseUint(f[5], 10, 64)
		sw, _ := strconv.ParseUint(f[9], 10, 64)
		io, _ := strconv.ParseUint(f[12], 10, 64)
		out[name] = DiskCounters{SectorsRead: sr, SectorsWritten: sw, IOTicks: io}
	}
	return out
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// ListDisks returns whole-disk device names from /sys/block minus excluded prefixes.
func ListDisks(hostSys string, exclude []string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(hostSys, "block"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if hasAnyPrefix(name, exclude) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func DiskModel(hostSys, name string) string {
	b, err := os.ReadFile(filepath.Join(hostSys, "block", name, "device", "model"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func DiskUtil(prevTicks, curTicks uint64, dtMS float64) float64 {
	if dtMS <= 0 || curTicks < prevTicks {
		return 0
	}
	pct := float64(curTicks-prevTicks) / dtMS * 100
	if pct > 100 {
		return 100
	}
	if pct < 0 {
		return 0
	}
	return pct
}

func ReadDiskstats(hostProc string, keep map[string]bool) (map[string]DiskCounters, error) {
	f, err := os.Open(filepath.Join(hostProc, "diskstats"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseDiskstats(f, keep), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run Disk -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/disk.go internal/collect/disk_test.go
git commit -m "feat: disk collector (/proc/diskstats parse + %util + disk list)"
```

---

### Task 6: Filesystem collector

**Files:**
- Create: `internal/collect/fs.go`
- Test: `internal/collect/fs_test.go`

**Interfaces:**
- Produces: `Mount{Device, Mountpoint, FSType string}`; `ParseMounts(io.Reader) []Mount` (keeps only real disk filesystems, dedupes by device); `ReadFS(hostProc, hostRoot string) []model.FSInfo` (statfs each mount under hostRoot).
- Allowed fs types: `ext2 ext3 ext4 xfs btrfs vfat exfat ntfs ntfs3 f2fs zfs`.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/fs_test.go`:
```go
package collect

import (
	"strings"
	"testing"
)

func TestParseMounts(t *testing.T) {
	in := "sysfs /sys sysfs rw 0 0\n" +
		"/dev/sda1 / ext4 rw 0 0\n" +
		"tmpfs /run tmpfs rw 0 0\n" +
		"/dev/nvme0n1p2 /data xfs rw 0 0\n" +
		"/dev/sda1 / ext4 rw 0 0\n" // duplicate device
	ms := ParseMounts(strings.NewReader(in))
	if len(ms) != 2 {
		t.Fatalf("len=%d want 2, got %+v", len(ms), ms)
	}
	if ms[0].Mountpoint != "/" || ms[0].FSType != "ext4" {
		t.Errorf("ms[0]=%+v", ms[0])
	}
	if ms[1].Mountpoint != "/data" {
		t.Errorf("ms[1]=%+v", ms[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run Mounts`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/collect/fs.go`:
```go
package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"system-monitor/internal/model"
)

type Mount struct {
	Device     string
	Mountpoint string
	FSType     string
}

var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "f2fs": true, "zfs": true,
}

func ParseMounts(r io.Reader) []Mount {
	var out []Mount
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		dev, mp, fstype := f[0], f[1], f[2]
		if !realFS[fstype] || seen[dev] {
			continue
		}
		seen[dev] = true
		out = append(out, Mount{Device: dev, Mountpoint: mp, FSType: fstype})
	}
	return out
}

// ReadFS statfs's each real mount. Host mountpoints are resolved under hostRoot
// (the host filesystem bind-mounted read-only into the container).
func ReadFS(hostProc, hostRoot string) []model.FSInfo {
	f, err := os.Open(filepath.Join(hostProc, "mounts"))
	if err != nil {
		return nil
	}
	defer f.Close()
	mounts := ParseMounts(f)
	var out []model.FSInfo
	for _, m := range mounts {
		p := filepath.Join(hostRoot, m.Mountpoint)
		var st syscall.Statfs_t
		if err := syscall.Statfs(p, &st); err != nil {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		free := st.Bavail * bs
		if total == 0 {
			continue
		}
		used := total - st.Bfree*bs
		out = append(out, model.FSInfo{
			Mount: m.Mountpoint,
			Used:  used,
			Total: total,
			Pct:   float64(total-free) / float64(total) * 100,
		})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run Mounts -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/fs.go internal/collect/fs_test.go
git commit -m "feat: filesystem collector (/proc/mounts parse + statfs)"
```

---

### Task 7: Process collector

**Files:**
- Create: `internal/collect/proc.go`
- Test: `internal/collect/proc_test.go`

**Interfaces:**
- Produces: `ProcSample{PID int, Name string, Jiffies uint64, RSS uint64}`; `ParseProcStat(pid int, content string) (ProcSample, bool)`; `ScanProcs(hostProc string) []ProcSample`.
- `stat` layout after last `)`: index 11 = utime, 12 = stime, 21 = rss (pages). `Jiffies = utime+stime`. `RSS = rssPages * 4096`.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/proc_test.go`:
```go
package collect

import "testing"

func TestParseProcStat(t *testing.T) {
	// comm contains spaces and a ")" to exercise last-paren splitting.
	// fields after ')': state(0) ppid(1)...utime@11 stime@12 ... rss@21
	// build: pid (my (weird) proc) R 1 1 1 0 -1 0 0 0 0 0 [utime=7] [stime=3] ...
	content := "42 (my (weird) proc) R 1 1 1 0 -1 0 0 0 0 0 7 3 0 0 20 0 1 0 999 123456 2048 " +
		"18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0"
	s, ok := ParseProcStat(42, content)
	if !ok {
		t.Fatal("parse failed")
	}
	if s.Name != "my (weird) proc" {
		t.Errorf("Name=%q", s.Name)
	}
	if s.Jiffies != 10 { // 7+3
		t.Errorf("Jiffies=%d, want 10", s.Jiffies)
	}
	if s.RSS != 2048*4096 {
		t.Errorf("RSS=%d, want %d", s.RSS, 2048*4096)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run ProcStat`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/collect/proc.go`:
```go
package collect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const pageSize = 4096

type ProcSample struct {
	PID     int
	Name    string
	Jiffies uint64
	RSS     uint64
}

func ParseProcStat(pid int, content string) (ProcSample, bool) {
	open := strings.IndexByte(content, '(')
	close := strings.LastIndexByte(content, ')')
	if open < 0 || close < 0 || close < open {
		return ProcSample{}, false
	}
	name := content[open+1 : close]
	rest := strings.Fields(content[close+1:])
	// rest[0]=state(field3); utime=field14->rest[11]; stime=field15->rest[12]; rss=field24->rest[21]
	if len(rest) < 22 {
		return ProcSample{}, false
	}
	utime, _ := strconv.ParseUint(rest[11], 10, 64)
	stime, _ := strconv.ParseUint(rest[12], 10, 64)
	rss, _ := strconv.ParseUint(rest[21], 10, 64)
	return ProcSample{PID: pid, Name: name, Jiffies: utime + stime, RSS: rss * pageSize}, true
}

// ScanProcs reads every /host/proc/[pid]/stat. Host PIDs are visible because
// /host/proc is a bind mount of the host procfs.
func ScanProcs(hostProc string) []ProcSample {
	entries, err := os.ReadDir(hostProc)
	if err != nil {
		return nil
	}
	var out []ProcSample
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(hostProc, e.Name(), "stat"))
		if err != nil {
			continue
		}
		if s, ok := ParseProcStat(pid, string(b)); ok {
			out = append(out, s)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run ProcStat -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/proc.go internal/collect/proc_test.go
git commit -m "feat: process collector (/proc/[pid]/stat parse + scan)"
```

---

### Task 8: Temperature collector

**Files:**
- Create: `internal/collect/temp.go`
- Test: `internal/collect/temp_test.go`

**Interfaces:**
- Produces: `FindCPUTemp(hwmonDir string) float64` (0 if none); `ReadCPUTemp(hostSys string) float64`.
- Strategy: for each `hwmonDir/hwmon*`, read `name`; if it is `coretemp`, `k10temp`, or `zenpower`, read the first `temp*_input` (millidegrees) and return /1000.

- [ ] **Step 1: Write the failing test**

Create `internal/collect/temp_test.go`:
```go
package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCPUTemp(t *testing.T) {
	root := t.TempDir()
	h := filepath.Join(root, "hwmon0")
	if err := os.MkdirAll(h, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(h, "name"), []byte("coretemp\n"), 0644)
	os.WriteFile(filepath.Join(h, "temp1_input"), []byte("58000\n"), 0644)

	if got := FindCPUTemp(root); got != 58 {
		t.Errorf("FindCPUTemp = %v, want 58", got)
	}
}

func TestFindCPUTempNone(t *testing.T) {
	if got := FindCPUTemp(t.TempDir()); got != 0 {
		t.Errorf("FindCPUTemp = %v, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run CPUTemp`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/collect/temp.go`:
```go
package collect

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var cpuTempSensors = map[string]bool{"coretemp": true, "k10temp": true, "zenpower": true}

// FindCPUTemp scans hwmon directories for a CPU sensor and returns its first
// temperature input in °C, or 0 if none is found.
func FindCPUTemp(hwmonDir string) float64 {
	dirs, err := os.ReadDir(hwmonDir)
	if err != nil {
		return 0
	}
	for _, d := range dirs {
		base := filepath.Join(hwmonDir, d.Name())
		name, err := os.ReadFile(filepath.Join(base, "name"))
		if err != nil || !cpuTempSensors[strings.TrimSpace(string(name))] {
			continue
		}
		files, _ := os.ReadDir(base)
		var inputs []string
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "temp") && strings.HasSuffix(f.Name(), "_input") {
				inputs = append(inputs, f.Name())
			}
		}
		if len(inputs) == 0 {
			continue
		}
		sort.Strings(inputs)
		b, err := os.ReadFile(filepath.Join(base, inputs[0]))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		if err != nil {
			continue
		}
		return milli / 1000
	}
	return 0
}

func ReadCPUTemp(hostSys string) float64 {
	return FindCPUTemp(filepath.Join(hostSys, "class", "hwmon"))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run CPUTemp -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/temp.go internal/collect/temp_test.go
git commit -m "feat: CPU temperature collector (/sys/class/hwmon)"
```

---

### Task 9: GPU collector (NVML)

**Files:**
- Create: `internal/collect/gpu.go`

**Interfaces:**
- Produces: `GPUReader` interface `{ Read() []model.GPUInfo; Close() }`; `NewGPUReader() GPUReader` (returns NVML-backed reader, or a nop reader if NVML init fails); `nopGPU` type for tests/no-GPU hosts.
- Consumes: `github.com/NVIDIA/go-nvml/pkg/nvml`.

Note: NVML requires cgo + a GPU at runtime, so this task has no unit test; it is
exercised by the container smoke test (Task 15). The interface lets `collector_test.go`
inject a fake in Task 10.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/NVIDIA/go-nvml/pkg/nvml@latest
go mod tidy
```

- [ ] **Step 2: Implement the GPU reader**

Create `internal/collect/gpu.go`:
```go
package collect

import (
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"system-monitor/internal/model"
)

type GPUReader interface {
	Read() []model.GPUInfo
	Close()
}

type nopGPU struct{}

func (nopGPU) Read() []model.GPUInfo { return nil }
func (nopGPU) Close()                {}

type nvmlGPU struct{}

// NewGPUReader initialises NVML. If unavailable (no GPU / no driver), it returns a
// nop reader so the rest of the app runs normally with the GPU tile hidden.
func NewGPUReader() GPUReader {
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		return nopGPU{}
	}
	return &nvmlGPU{}
}

func (g *nvmlGPU) Close() { nvml.Shutdown() }

func (g *nvmlGPU) Read() []model.GPUInfo {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil
	}
	var out []model.GPUInfo
	for i := 0; i < count; i++ {
		d, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}
		info := model.GPUInfo{Fan: -1}
		if name, ret := d.GetName(); ret == nvml.SUCCESS {
			info.Name = name
		}
		if u, ret := d.GetUtilizationRates(); ret == nvml.SUCCESS {
			info.Util = int(u.Gpu)
		}
		if m, ret := d.GetMemoryInfo(); ret == nvml.SUCCESS {
			info.MemUsed = m.Used
			info.MemTotal = m.Total
		}
		if temp, ret := d.GetTemperature(nvml.TEMPERATURE_GPU); ret == nvml.SUCCESS {
			info.Temp = int(temp)
		}
		if p, ret := d.GetPowerUsage(); ret == nvml.SUCCESS {
			info.Power = int(p) / 1000 // mW -> W
		}
		if c, ret := d.GetClockInfo(nvml.CLOCK_SM); ret == nvml.SUCCESS {
			info.ClkSM = int(c)
		}
		if f, ret := d.GetFanSpeed(); ret == nvml.SUCCESS {
			info.Fan = int(f)
		}
		out = append(out, info)
	}
	return out
}
```

- [ ] **Step 3: Verify it builds**

Run: `CGO_ENABLED=1 go build ./internal/collect/`
Expected: build succeeds (needs gcc + go-nvml headers; NVML is dlopen'd at runtime).

- [ ] **Step 4: Commit**

```bash
git add internal/collect/gpu.go go.mod go.sum
git commit -m "feat: GPU collector via NVML with nop fallback"
```

---

### Task 10: Collector orchestrator

**Files:**
- Create: `internal/collect/collector.go`
- Test: `internal/collect/collector_test.go`

**Interfaces:**
- Consumes: all collectors above; `config.Config`; `GPUReader`; `model.Snapshot`.
- Produces: `type Collector`; `New(cfg config.Config, gpu GPUReader) *Collector`; `(*Collector) Tick(now time.Time) model.Snapshot`. First `Tick` returns zero rates (no previous sample).

- [ ] **Step 1: Write the failing test**

Create `internal/collect/collector_test.go`. It builds a fake `/host/proc` + `/host/sys` tree and asserts `Tick` produces a well-formed snapshot (second tick yields 0 rates because inputs are unchanged):
```go
package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"system-monitor/internal/config"
	"system-monitor/internal/model"
)

type fakeGPU struct{}

func (fakeGPU) Read() []model.GPUInfo { return []model.GPUInfo{{Name: "test", Util: 10}} }
func (fakeGPU) Close()                {}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorTick(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	sys := filepath.Join(root, "sys")
	writeFile(t, filepath.Join(proc, "stat"), "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 100 0 50 800 50 0 0 0 0 0\n")
	writeFile(t, filepath.Join(proc, "meminfo"), "MemTotal: 1000 kB\nMemAvailable: 600 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	writeFile(t, filepath.Join(proc, "net/dev"), "  eth0: 5000 0 0 0 0 0 0 0 2000 0 0 0 0 0 0 0\n")
	writeFile(t, filepath.Join(proc, "diskstats"), "8 0 sda 0 0 2000 0 0 0 1000 0 0 500 0\n")
	writeFile(t, filepath.Join(proc, "loadavg"), "0.50 0.40 0.30 1/100 12345\n")
	writeFile(t, filepath.Join(proc, "uptime"), "3600.00 1000.00\n")
	writeFile(t, filepath.Join(sys, "block/sda/device/model"), "TEST DISK\n")

	cfg := config.Config{HostProc: proc, HostSys: sys, HostRoot: root, ProcTopN: 5, DiskExclude: []string{"loop"}}
	c := New(cfg, fakeGPU{})

	t0 := time.Unix(1000, 0)
	c.Tick(t0) // primes previous sample
	snap := c.Tick(t0.Add(time.Second))

	if len(snap.CPU.Cores) != 1 {
		t.Errorf("cores = %d, want 1", len(snap.CPU.Cores))
	}
	if snap.Mem.Total != 1000*1024 {
		t.Errorf("mem total = %d", snap.Mem.Total)
	}
	if len(snap.GPU) != 1 || snap.GPU[0].Name != "test" {
		t.Errorf("gpu = %+v", snap.GPU)
	}
	if len(snap.Disk.Devs) != 1 || snap.Disk.Devs[0].Name != "sda" {
		t.Errorf("disk devs = %+v", snap.Disk.Devs)
	}
	if snap.Host.Load[0] != 0.5 {
		t.Errorf("load = %v", snap.Host.Load)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/ -run CollectorTick`
Expected: FAIL (no `New`/`Collector`).

- [ ] **Step 3: Implement the orchestrator**

Create `internal/collect/collector.go`:
```go
package collect

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"system-monitor/internal/config"
	"system-monitor/internal/model"
)

type Collector struct {
	cfg   config.Config
	gpu   GPUReader
	disks []string

	prevCPUAgg   CPUTimes
	prevCPUCores []CPUTimes
	prevNet      NetCounters
	prevDisk     map[string]DiskCounters
	prevProc     map[int]uint64
	prevTime     time.Time
	primed       bool
}

func New(cfg config.Config, gpu GPUReader) *Collector {
	disks, _ := ListDisks(cfg.HostSys, cfg.DiskExclude)
	return &Collector{cfg: cfg, gpu: gpu, disks: disks, prevDisk: map[string]DiskCounters{}, prevProc: map[int]uint64{}}
}

func (c *Collector) Tick(now time.Time) model.Snapshot {
	dt := now.Sub(c.prevTime).Seconds()
	if !c.primed {
		dt = 0
	}
	snap := model.Snapshot{T: now.Unix(), GPU: c.gpu.Read()}

	snap.Host = c.host()
	snap.CPU = c.cpu()
	if m, err := ReadMeminfo(c.cfg.HostProc); err == nil {
		snap.Mem = m
	}
	snap.CPU.Temp = ReadCPUTemp(c.cfg.HostSys)
	snap.Net = c.net(dt)
	snap.Disk = c.disk(dt)
	snap.FS = ReadFS(c.cfg.HostProc, c.cfg.HostRoot)
	snap.Proc = c.procs(dt)

	c.prevTime = now
	c.primed = true
	return snap
}

func (c *Collector) host() model.HostInfo {
	h := model.HostInfo{}
	name, _ := os.Hostname()
	h.Name = name
	if b, err := os.ReadFile(filepath.Join(c.cfg.HostProc, "uptime")); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			sec, _ := strconv.ParseFloat(f[0], 64)
			h.Uptime = int64(sec)
		}
	}
	if b, err := os.ReadFile(filepath.Join(c.cfg.HostProc, "loadavg")); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			h.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	return h
}

func (c *Collector) cpu() model.CPUInfo {
	agg, cores, err := ReadCPUStat(c.cfg.HostProc)
	if err != nil {
		return model.CPUInfo{}
	}
	info := model.CPUInfo{Cores: make([]float64, len(cores))}
	if c.primed {
		info.Agg = CPUPercent(c.prevCPUAgg, agg)
		for i := range cores {
			if i < len(c.prevCPUCores) {
				info.Cores[i] = CPUPercent(c.prevCPUCores[i], cores[i])
			}
		}
	}
	c.prevCPUAgg, c.prevCPUCores = agg, cores
	return info
}

func (c *Collector) net(dt float64) model.NetInfo {
	cur, err := ReadNetDev(c.cfg.HostProc)
	if err != nil {
		return model.NetInfo{}
	}
	n := model.NetInfo{RXTotal: cur.RX, TXTotal: cur.TX}
	if c.primed && dt > 0 {
		n.RX = rate(c.prevNet.RX, cur.RX, dt)
		n.TX = rate(c.prevNet.TX, cur.TX, dt)
	}
	c.prevNet = cur
	return n
}

func (c *Collector) disk(dt float64) model.DiskInfo {
	keep := map[string]bool{}
	for _, d := range c.disks {
		keep[d] = true
	}
	cur, err := ReadDiskstats(c.cfg.HostProc, keep)
	if err != nil {
		return model.DiskInfo{}
	}
	var di model.DiskInfo
	for _, name := range c.disks {
		cc, ok := cur[name]
		if !ok {
			continue
		}
		rTot := cc.SectorsRead * sectorSize
		wTot := cc.SectorsWritten * sectorSize
		di.ReadTotal += rTot
		di.WriteTotal += wTot
		dev := model.DiskDev{Name: name, Model: DiskModel(c.cfg.HostSys, name)}
		if p, ok := c.prevDisk[name]; c.primed && ok && dt > 0 {
			dev.Read = rate(p.SectorsRead*sectorSize, rTot, dt)
			dev.Write = rate(p.SectorsWritten*sectorSize, wTot, dt)
			dev.Util = DiskUtil(p.IOTicks, cc.IOTicks, dt*1000)
			di.Read += dev.Read
			di.Write += dev.Write
		}
		di.Devs = append(di.Devs, dev)
	}
	c.prevDisk = cur
	return di
}

func (c *Collector) procs(dt float64) []model.ProcInfo {
	samples := ScanProcs(c.cfg.HostProc)
	next := make(map[int]uint64, len(samples))
	var out []model.ProcInfo
	for _, s := range samples {
		next[s.PID] = s.Jiffies
		cpu := 0.0
		if prev, ok := c.prevProc[s.PID]; c.primed && ok && dt > 0 && s.Jiffies >= prev {
			// (djiffies/USER_HZ)/dt*100 simplifies to djiffies/dt when USER_HZ=100.
			cpu = float64(s.Jiffies-prev) / dt
		}
		out = append(out, model.ProcInfo{PID: s.PID, Name: s.Name, CPU: cpu, RSS: s.RSS})
	}
	c.prevProc = next
	sort.Slice(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	if len(out) > c.cfg.ProcTopN {
		out = out[:c.cfg.ProcTopN]
	}
	return out
}

func rate(prev, cur uint64, dt float64) uint64 {
	if cur < prev || dt <= 0 {
		return 0
	}
	return uint64(float64(cur-prev) / dt)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ -run CollectorTick -v`
Expected: PASS.

- [ ] **Step 5: Run the whole collect package**

Run: `go test ./internal/collect/`
Expected: PASS (all collectors).

- [ ] **Step 6: Commit**

```bash
git add internal/collect/collector.go internal/collect/collector_test.go
git commit -m "feat: collector orchestrator producing snapshots with deltas"
```

---

### Task 11: Broadcast hub

**Files:**
- Create: `internal/server/hub.go`
- Test: `internal/server/hub_test.go`

**Interfaces:**
- Produces: `type Hub`; `NewHub() *Hub`; `(*Hub) Register() chan []byte`; `(*Hub) Unregister(chan []byte)`; `(*Hub) Broadcast(msg []byte)`. Broadcast is non-blocking (drops to a slow client rather than stalling).

- [ ] **Step 1: Write the failing test**

Create `internal/server/hub_test.go`:
```go
package server

import (
	"testing"
	"time"
)

func TestHubBroadcast(t *testing.T) {
	h := NewHub()
	ch := h.Register()
	defer h.Unregister(ch)

	h.Broadcast([]byte("hello"))
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Errorf("got %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received")
	}
}

func TestHubUnregister(t *testing.T) {
	h := NewHub()
	ch := h.Register()
	h.Unregister(ch)
	h.Broadcast([]byte("x")) // must not panic on a closed/removed client
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run Hub`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/server/hub.go`:
```go
package server

import "sync"

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

func (h *Hub) Register() chan []byte {
	ch := make(chan []byte, 4)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unregister(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *Hub) Broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client: drop this frame
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run Hub -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/hub.go internal/server/hub_test.go
git commit -m "feat: broadcast hub for websocket clients"
```

---

### Task 12: HTTP server + WebSocket + ticker loop

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/ws.go`
- Create: `cmd/monitor/main.go`
- Create: `web/index.html` (placeholder page so `embed` compiles; real UI in Tasks 13–14)

**Interfaces:**
- Consumes: `collect.Collector`, `Hub`, `config.Config`.
- Produces: `type Server`; `New(cfg config.Config, c *collect.Collector) *Server`; `(*Server) Run() error` (starts ticker + HTTP). Routes: `GET /` (UI), `GET /ws` (stream), `GET /api/snapshot` (latest snapshot JSON), `GET /healthz`.
- WS/init envelope: on connect send `{"type":"init","history":[<snap>...]}`, then each tick `{"type":"tick","snap":<snap>}`.

- [ ] **Step 1: Add the websocket dependency**

Run:
```bash
go get github.com/gorilla/websocket@latest
go mod tidy
```

- [ ] **Step 2: Create the embedded web placeholder**

Create `web/index.html`:
```html
<!doctype html>
<html><head><meta charset="utf-8"><title>System Monitor</title></head>
<body>loading…</body></html>
```

- [ ] **Step 3: Implement the server (ticker + ring buffer + routes)**

Create `internal/server/server.go`:
```go
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
	"system-monitor/internal/model"
)

//go:embed all:../../web
var webFS embed.FS

type Server struct {
	cfg  config.Config
	col  *collect.Collector
	hub  *Hub
	mu   sync.Mutex
	ring []json.RawMessage // last HistorySec snapshots
	last json.RawMessage
}

func New(cfg config.Config, c *collect.Collector) *Server {
	return &Server{cfg: cfg, col: c, hub: NewHub()}
}

func (s *Server) Run() error {
	go s.loop()

	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	return http.ListenAndServe(":"+s.cfg.Port, mux)
}

func (s *Server) loop() {
	ticker := time.NewTicker(time.Duration(s.cfg.IntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for now := range ticker.C {
		snap := s.col.Tick(now)
		raw, err := json.Marshal(snap)
		if err != nil {
			continue
		}
		s.store(raw)
		s.hub.Broadcast(s.wrap("tick", raw))
	}
}

func (s *Server) store(raw json.RawMessage) {
	s.mu.Lock()
	s.last = raw
	s.ring = append(s.ring, raw)
	if len(s.ring) > s.cfg.HistorySec {
		s.ring = s.ring[len(s.ring)-s.cfg.HistorySec:]
	}
	s.mu.Unlock()
}

func (s *Server) wrap(kind string, snap json.RawMessage) []byte {
	b, _ := json.Marshal(struct {
		Type string          `json:"type"`
		Snap json.RawMessage `json:"snap"`
	}{kind, snap})
	return b
}

func (s *Server) initMessage() []byte {
	s.mu.Lock()
	hist := make([]json.RawMessage, len(s.ring))
	copy(hist, s.ring)
	s.mu.Unlock()
	b, _ := json.Marshal(struct {
		Type    string            `json:"type"`
		History []json.RawMessage `json:"history"`
	}{"init", hist})
	return b
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if last == nil {
		w.Write([]byte(`{}`))
		return
	}
	w.Write(last)
}

var _ = model.Snapshot{} // keep model import if unused elsewhere
```

- [ ] **Step 4: Implement the WebSocket handler**

Create `internal/server/ws.go`:
```go
package server

import (
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // internal tool, no auth
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := s.hub.Register()
	defer s.hub.Unregister(ch)

	if err := conn.WriteMessage(websocket.TextMessage, s.initMessage()); err != nil {
		return
	}

	// Reader goroutine: detect disconnect and drain control frames.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
```

- [ ] **Step 5: Implement main**

Create `cmd/monitor/main.go`:
```go
package main

import (
	"log"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
	"system-monitor/internal/server"
)

func main() {
	cfg := config.Load()
	gpu := collect.NewGPUReader()
	defer gpu.Close()

	col := collect.New(cfg, gpu)
	srv := server.New(cfg, col)

	log.Printf("system-monitor listening on :%s (interval=%dms)", cfg.Port, cfg.IntervalMS)
	log.Fatal(srv.Run())
}
```

- [ ] **Step 6: Build and run the server tests**

Run: `CGO_ENABLED=1 go build ./... && go test ./internal/server/`
Expected: build succeeds, hub tests PASS.

- [ ] **Step 7: Smoke-run locally against host paths**

Run:
```bash
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ PORT=8080 go run ./cmd/monitor &
sleep 2
curl -s localhost:8080/api/snapshot | head -c 400 ; echo
kill %1
```
Expected: JSON with non-empty `cpu.cores` (length 32 on this host) after ~2s.

- [ ] **Step 8: Commit**

```bash
git add internal/server/server.go internal/server/ws.go cmd/monitor/main.go web/index.html go.mod go.sum
git commit -m "feat: HTTP server, websocket stream, ticker loop, and main"
```

---

### Task 13: Frontend — layout (HTML + CSS)

**Files:**
- Modify: `web/index.html` (replace placeholder)
- Create: `web/style.css`

**Interfaces:**
- Produces: DOM element IDs consumed by `app.js` in Task 14: canvases `cpuChart`, `netChart`, `diskChart`, `gpuChart`; the `coreGrid` container; value spans listed in the JS task. A 3-row CSS grid tuned for 1920×1080, no scroll (`height:100vh; overflow:hidden`).

- [ ] **Step 1: Write the layout HTML**

Replace `web/index.html` with:
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
    <span id="host">—</span>
    <span id="uptime"></span>
    <span id="load"></span>
    <span id="clock"></span>
    <span id="conn" class="off">offline</span>
  </header>

  <main id="grid">
    <section class="tile" id="tile-cpu">
      <h2>CPU <span class="val" id="cpuAgg">0%</span> <span class="sub" id="cpuTemp"></span></h2>
      <canvas id="cpuChart"></canvas>
      <div id="coreGrid" class="core-grid"></div>
    </section>

    <section class="tile" id="tile-gpu">
      <h2>GPU <span class="sub" id="gpuName">—</span> <span class="val" id="gpuUtil">0%</span></h2>
      <canvas id="gpuChart"></canvas>
      <div class="kv" id="gpuStats"></div>
    </section>

    <section class="tile" id="tile-mem">
      <h2>Memory &amp; Swap</h2>
      <div class="bar"><div class="fill mem" id="memBar"></div></div>
      <div class="kv" id="memText"></div>
      <div class="bar"><div class="fill swap" id="swapBar"></div></div>
      <div class="kv" id="swapText"></div>
    </section>

    <section class="tile" id="tile-net">
      <h2>Network</h2>
      <canvas id="netChart"></canvas>
      <div class="kv" id="netText"></div>
    </section>

    <section class="tile" id="tile-disk">
      <h2>Disk</h2>
      <canvas id="diskChart"></canvas>
      <div class="kv" id="diskText"></div>
      <div id="diskUtil"></div>
    </section>

    <section class="tile" id="tile-fs">
      <h2>Filesystems</h2>
      <div id="fsList" class="list"></div>
    </section>

    <section class="tile" id="tile-proc">
      <h2>Top processes</h2>
      <div id="procList" class="list"></div>
    </section>
  </main>
</div>
<script src="chart.js"></script>
<script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Write the CSS (dark theme, no-scroll grid)**

Create `web/style.css`:
```css
:root {
  --bg: #1e1e2e; --tile: #252537; --line: #3a3a52; --text: #d9d9e3;
  --sub: #8a8aa0; --accent: #7aa2f7; --mem: #f7768e; --swap: #9ece6a;
  --rx: #7aa2f7; --tx: #e0af68; --read: #7dcfff; --write: #bb9af7; --util: #f7768e;
}
* { box-sizing: border-box; margin: 0; }
html, body { height: 100%; }
body {
  background: var(--bg); color: var(--text);
  font: 13px/1.3 system-ui, sans-serif; overflow: hidden;
}
#app { height: 100vh; display: flex; flex-direction: column; padding: 6px; gap: 6px; }
#topbar { display: flex; gap: 18px; align-items: center; padding: 2px 8px; color: var(--sub); }
#topbar #host { color: var(--text); font-weight: 600; }
#conn { margin-left: auto; padding: 1px 8px; border-radius: 8px; font-size: 11px; }
#conn.on { background: #2b4a2b; color: #9ece6a; }
#conn.off { background: #4a2b2b; color: #f7768e; }

#grid {
  flex: 1; display: grid; gap: 6px; min-height: 0;
  grid-template-columns: 1.4fr 1fr 1fr;
  grid-template-rows: 1.5fr 1fr 1fr;
  grid-template-areas:
    "cpu cpu gpu"
    "mem net disk"
    "fs  proc proc";
}
#tile-cpu { grid-area: cpu; } #tile-gpu { grid-area: gpu; }
#tile-mem { grid-area: mem; } #tile-net { grid-area: net; }
#tile-disk { grid-area: disk; } #tile-fs { grid-area: fs; }
#tile-proc { grid-area: proc; }

.tile {
  background: var(--tile); border: 1px solid var(--line); border-radius: 8px;
  padding: 8px; display: flex; flex-direction: column; min-height: 0; overflow: hidden;
}
.tile h2 { font-size: 12px; color: var(--sub); font-weight: 600; margin-bottom: 6px; display: flex; gap: 8px; align-items: baseline; }
.tile h2 .val { color: var(--accent); font-size: 14px; margin-left: auto; }
.tile h2 .sub { color: var(--sub); font-weight: 400; }
canvas { width: 100%; flex: 1; min-height: 0; display: block; }

.core-grid { display: grid; grid-template-columns: repeat(16, 1fr); gap: 2px; margin-top: 6px; height: 34px; }
.core-cell { background: #2f2f45; border-radius: 2px; position: relative; overflow: hidden; }
.core-cell > i { position: absolute; bottom: 0; left: 0; right: 0; background: var(--accent); }

.bar { height: 12px; background: #2f2f45; border-radius: 6px; overflow: hidden; margin: 3px 0; }
.fill { height: 100%; width: 0; transition: width .3s; }
.fill.mem { background: var(--mem); } .fill.swap { background: var(--swap); }
.kv { color: var(--sub); font-size: 12px; margin: 2px 0; }
.kv b { color: var(--text); font-weight: 600; }

#diskUtil { margin-top: 4px; display: flex; flex-direction: column; gap: 3px; }
.util-row { display: grid; grid-template-columns: 70px 1fr 44px; gap: 6px; align-items: center; font-size: 11px; }
.util-row .track { height: 8px; background: #2f2f45; border-radius: 4px; overflow: hidden; }
.util-row .track > i { display: block; height: 100%; background: var(--util); }

.list { overflow: hidden; display: flex; flex-direction: column; gap: 2px; }
.list .row { display: grid; grid-template-columns: 1fr auto auto; gap: 10px; font-size: 12px; }
.list .row .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.list .row .n { color: var(--sub); font-variant-numeric: tabular-nums; }
#gpuStats { display: grid; grid-template-columns: 1fr 1fr; gap: 2px 12px; }
```

- [ ] **Step 3: Verify it builds and serves**

Run:
```bash
CGO_ENABLED=1 go build ./... && \
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ go run ./cmd/monitor &
sleep 1; curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/ ; kill %1
```
Expected: `200`.

- [ ] **Step 4: Commit**

```bash
git add web/index.html web/style.css
git commit -m "feat: frontend layout (1080p no-scroll grid, dark theme)"
```

---

### Task 14: Frontend — chart engine + live client

**Files:**
- Create: `web/chart.js`
- Create: `web/app.js`

**Interfaces:**
- Consumes: WebSocket messages `{type:"init",history:[...]}` and `{type:"tick",snap:{...}}` from Task 12; DOM IDs from Task 13.
- Produces: a `Chart` class and the running client. No external libraries.

- [ ] **Step 1: Write the canvas chart engine**

Create `web/chart.js`:
```javascript
// Minimal rolling line chart on a canvas. Fixed or auto Y scale.
class Chart {
  constructor(canvas, { series, maxPoints = 60, yMax = null, fill = false }) {
    this.c = canvas;
    this.ctx = canvas.getContext('2d');
    this.series = series;            // [{color}]
    this.maxPoints = maxPoints;
    this.yMax = yMax;                // null = auto
    this.fill = fill;
    this.data = series.map(() => []);// per-series array of values
    this._resize();
    window.addEventListener('resize', () => this._resize());
  }
  _resize() {
    const r = this.c.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    this.c.width = Math.max(1, Math.floor(r.width * dpr));
    this.c.height = Math.max(1, Math.floor(r.height * dpr));
    this.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    this.w = r.width; this.h = r.height;
    this.render();
  }
  push(values) {
    for (let i = 0; i < this.data.length; i++) {
      this.data[i].push(values[i] ?? 0);
      if (this.data[i].length > this.maxPoints) this.data[i].shift();
    }
    this.render();
  }
  seed(rows) { // rows: array of value-arrays, oldest first
    this.data = this.series.map(() => []);
    for (const row of rows) this.push(row);
  }
  render() {
    const { ctx, w, h } = this;
    ctx.clearRect(0, 0, w, h);
    let ymax = this.yMax;
    if (ymax == null) {
      ymax = 1;
      for (const s of this.data) for (const v of s) if (v > ymax) ymax = v;
      ymax *= 1.15;
    }
    // gridlines
    ctx.strokeStyle = 'rgba(255,255,255,0.06)'; ctx.lineWidth = 1;
    for (let g = 1; g < 4; g++) {
      const y = (h * g) / 4; ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
    }
    const n = this.maxPoints;
    const x = i => (w * i) / (n - 1);
    const y = v => h - (v / ymax) * h;
    for (let s = 0; s < this.data.length; s++) {
      const d = this.data[s];
      if (d.length < 2) continue;
      const off = n - d.length;
      ctx.strokeStyle = this.series[s].color; ctx.lineWidth = 1.4;
      ctx.beginPath();
      for (let i = 0; i < d.length; i++) {
        const px = x(off + i), py = y(d[i]);
        i ? ctx.lineTo(px, py) : ctx.moveTo(px, py);
      }
      ctx.stroke();
      if (this.fill) {
        ctx.lineTo(x(off + d.length - 1), h); ctx.lineTo(x(off), h); ctx.closePath();
        ctx.fillStyle = this.series[s].color + '22'; ctx.fill();
      }
    }
  }
}
```

- [ ] **Step 2: Write the live client**

Create `web/app.js`:
```javascript
const $ = id => document.getElementById(id);
const css = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

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

let cpuChart, netChart, diskChart, gpuChart, coreCells = [];

function initCharts(coreCount) {
  cpuChart = new Chart($('cpuChart'), {
    series: Array.from({ length: coreCount }, (_, i) =>
      ({ color: `hsl(${(i * 360 / coreCount) | 0} 70% 60%)` })), yMax: 100 });
  netChart = new Chart($('netChart'), { series: [{ color: css('--rx') }, { color: css('--tx') }], fill: true });
  diskChart = new Chart($('diskChart'), { series: [{ color: css('--read') }, { color: css('--write') }], fill: true });
  gpuChart = new Chart($('gpuChart'), { series: [{ color: css('--accent') }], yMax: 100 });

  const grid = $('coreGrid'); grid.innerHTML = '';
  coreCells = [];
  for (let i = 0; i < coreCount; i++) {
    const cell = document.createElement('div'); cell.className = 'core-cell';
    const bar = document.createElement('i'); cell.appendChild(bar);
    grid.appendChild(cell); coreCells.push(bar);
  }
}

function applySnap(s) {
  if (!cpuChart) initCharts(s.cpu.cores.length);
  // CPU
  $('cpuAgg').textContent = s.cpu.agg.toFixed(0) + '%';
  $('cpuTemp').textContent = s.cpu.temp ? s.cpu.temp.toFixed(0) + '°C' : '';
  cpuChart.push(s.cpu.cores);
  s.cpu.cores.forEach((v, i) => { if (coreCells[i]) {
    coreCells[i].style.height = v + '%';
    coreCells[i].style.background = v > 80 ? css('--util') : css('--accent');
  }});
  // Mem
  $('memBar').style.width = s.mem.pct + '%';
  $('swapBar').style.width = s.mem.swap_pct + '%';
  $('memText').innerHTML = `<b>${fmtBytes(s.mem.used)}</b> (${s.mem.pct.toFixed(0)}%) of ${fmtBytes(s.mem.total)} · cache ${fmtBytes(s.mem.cache)}`;
  $('swapText').innerHTML = `Swap <b>${fmtBytes(s.mem.swap_used)}</b> (${s.mem.swap_pct.toFixed(0)}%) of ${fmtBytes(s.mem.swap_total)}`;
  // Net
  netChart.push([s.net.rx, s.net.tx]);
  $('netText').innerHTML = `<b style="color:${css('--rx')}">↓ ${fmtRate(s.net.rx)}</b> · <b style="color:${css('--tx')}">↑ ${fmtRate(s.net.tx)}</b> — tot ${fmtBytes(s.net.rx_total)} / ${fmtBytes(s.net.tx_total)}`;
  // Disk
  diskChart.push([s.disk.read, s.disk.write]);
  $('diskText').innerHTML = `<b style="color:${css('--read')}">R ${fmtRate(s.disk.read)}</b> · <b style="color:${css('--write')}">W ${fmtRate(s.disk.write)}</b>`;
  $('diskUtil').innerHTML = s.disk.devs.map(d =>
    `<div class="util-row"><span class="name" title="${d.model || ''}">${d.name}</span>` +
    `<span class="track"><i style="width:${d.util.toFixed(0)}%"></i></span>` +
    `<span class="n">${d.util.toFixed(0)}%</span></div>`).join('');
  // GPU
  if (s.gpu && s.gpu.length) {
    const g = s.gpu[0];
    $('gpuName').textContent = g.name;
    $('gpuUtil').textContent = g.util + '%';
    gpuChart.push([g.util]);
    $('gpuStats').innerHTML =
      `<div class="kv">mem <b>${fmtBytes(g.mem_used)}</b>/${fmtBytes(g.mem_total)}</div>` +
      `<div class="kv">temp <b>${g.temp}°C</b></div>` +
      `<div class="kv">power <b>${g.power} W</b></div>` +
      `<div class="kv">clk <b>${g.clk_sm} MHz</b></div>` +
      (g.fan >= 0 ? `<div class="kv">fan <b>${g.fan}%</b></div>` : '');
  } else {
    $('tile-gpu').style.display = 'none';
  }
  // FS
  $('fsList').innerHTML = s.fs.map(f =>
    `<div class="row"><span class="name">${f.mount}</span>` +
    `<span class="n">${fmtBytes(f.used)}/${fmtBytes(f.total)}</span>` +
    `<span class="n">${f.pct.toFixed(0)}%</span></div>`).join('');
  // Proc
  $('procList').innerHTML = s.proc.map(p =>
    `<div class="row"><span class="name">${p.name}</span>` +
    `<span class="n">${p.cpu.toFixed(0)}%</span>` +
    `<span class="n">${fmtBytes(p.rss)}</span></div>`).join('');
  // Header
  $('host').textContent = s.host.name;
  $('uptime').textContent = 'up ' + fmtUptime(s.host.uptime);
  $('load').textContent = 'load ' + s.host.load.map(x => x.toFixed(2)).join(' ');
  $('clock').textContent = new Date(s.t * 1000).toLocaleTimeString();
}

function seedHistory(history) {
  if (!history.length) return;
  const first = history[0];
  if (!cpuChart) initCharts(first.cpu.cores.length);
  cpuChart.seed(history.map(s => s.cpu.cores));
  netChart.seed(history.map(s => [s.net.rx, s.net.tx]));
  diskChart.seed(history.map(s => [s.disk.read, s.disk.write]));
  gpuChart.seed(history.map(s => [s.gpu && s.gpu[0] ? s.gpu[0].util : 0]));
  applySnap(history[history.length - 1]);
}

let ws, backoff = 500;
function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.onopen = () => { backoff = 500; $('conn').className = 'on'; $('conn').textContent = 'live'; };
  ws.onmessage = ev => {
    const m = JSON.parse(ev.data);
    if (m.type === 'init') seedHistory(m.history || []);
    else if (m.type === 'tick') applySnap(m.snap);
  };
  ws.onclose = () => {
    $('conn').className = 'off'; $('conn').textContent = 'offline';
    setTimeout(connect, backoff); backoff = Math.min(backoff * 2, 5000);
  };
  ws.onerror = () => ws.close();
}
connect();
```

- [ ] **Step 3: Build and visually verify**

Run:
```bash
CGO_ENABLED=1 go build ./... && \
HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT=/ go run ./cmd/monitor &
sleep 2
curl -s localhost:8080/api/snapshot | python3 -c "import sys,json;d=json.load(sys.stdin);print('cores',len(d['cpu']['cores']),'disks',[x['name'] for x in d['disk']['devs']],'gpu',[g['name'] for g in d['gpu']],'fs',len(d['fs']),'proc',len(d['proc']))"
kill %1
```
Expected: `cores 32 disks ['sda','nvme0n1','nvme1n1'] gpu ['NVIDIA GeForce RTX 3060'] fs >=1 proc 8`. (Open `http://localhost:8080` in a browser to confirm the page renders with live charts and no scrollbar at 1920×1080.)

- [ ] **Step 4: Commit**

```bash
git add web/chart.js web/app.js
git commit -m "feat: canvas chart engine and live websocket client"
```

---

### Task 15: Docker packaging + end-to-end smoke test

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.dockerignore`

**Interfaces:**
- Consumes: the full app. Produces the deployable container.

- [ ] **Step 1: Write .dockerignore**

Create `.dockerignore`:
```
.git
docs
*.md
/bin
```

- [ ] **Step 2: Write the Dockerfile (multi-stage, cgo + distroless)**

Create `Dockerfile`:
```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# cgo required by go-nvml (NVML itself is dlopen'd at runtime, not linked here)
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/monitor ./cmd/monitor

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/monitor /monitor
EXPOSE 8080
ENTRYPOINT ["/monitor"]
```

- [ ] **Step 3: Write docker-compose.yml**

Create `docker-compose.yml`:
```yaml
services:
  monitor:
    build: .
    image: system-monitor:latest
    container_name: system-monitor
    restart: unless-stopped
    runtime: nvidia
    network_mode: host
    environment:
      - PORT=8080
      - INTERVAL_MS=1000
      - HISTORY_SECONDS=60
      - PROC_TOP_N=8
      - HOST_PROC=/host/proc
      - HOST_SYS=/host/sys
      - HOST_ROOT=/host/root
      - NVIDIA_VISIBLE_DEVICES=all
      - NVIDIA_DRIVER_CAPABILITIES=utility
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - type: bind
        source: /
        target: /host/root
        read_only: true
        bind:
          propagation: rslave
```

- [ ] **Step 4: Build and start the container**

Run:
```bash
docker compose up -d --build
sleep 4
docker compose ps
docker image inspect system-monitor:latest --format '{{.Size}}' | awk '{printf "image bytes: %s\n", $1}'
```
Expected: container `Up`; image size roughly ≤ ~40 MB.

- [ ] **Step 5: End-to-end smoke test (host metrics + GPU inside container)**

Run:
```bash
curl -s localhost:8080/healthz; echo
curl -s localhost:8080/api/snapshot | python3 -c "import sys,json;d=json.load(sys.stdin);assert len(d['cpu']['cores'])>0,'no cores';assert d['mem']['total']>0,'no mem';print('OK cores=%d gpu=%s disks=%s fs=%d proc=%d'%(len(d['cpu']['cores']),[g['name'] for g in d['gpu']],[x['name']+':%0.0f%%'%x['util'] for x in d['disk']['devs']],len(d['fs']),len(d['proc'])))"
```
Expected: `ok` then `OK cores=32 gpu=['NVIDIA GeForce RTX 3060'] disks=[...] fs>=1 proc=8`. GPU list must be non-empty (proves NVML works inside the container).

- [ ] **Step 6: Measure resource usage against budget**

Run:
```bash
docker stats --no-stream system-monitor
```
Expected: MEM USAGE well under 30 MB, CPU% low single digits. (If the per-process scan pushes CPU up, set `PROC_INTERVAL_MS` higher in a later tuning pass — noted as future work.)

- [ ] **Step 7: Commit**

```bash
git add Dockerfile docker-compose.yml .dockerignore
git commit -m "feat: docker multi-stage build and compose with nvidia runtime + host mounts"
```

---

## Self-Review

**Spec coverage check (spec §-by-§ → task):**
- Resemble System Monitor Resources view → Tasks 13–14 (layout + charts). ✓
- Real-time ~1s updates → Task 12 ticker + WS broadcast. ✓
- Disk I/O + **per-disk %util** → Task 5 (`DiskUtil`) + Task 10 wiring + Task 14 util bars. ✓
- GPU like nvidia-smi → Task 9 (NVML) + Task 14 GPU tile. ✓
- One 1080p page, no scroll → Task 13 CSS grid (`height:100vh; overflow:hidden`). ✓
- Docker Compose → Task 15. ✓
- Lightweight → Go stdlib, custom canvas, distroless; budget checked in Task 15 step 6. ✓
- No auth → no auth code anywhere; WS `CheckOrigin` always true (documented). ✓
- Filesystem usage → Task 6 + host-root mount in Task 15. ✓
- Top processes → Task 7 + Task 10 sort/topN + Task 14 list. ✓
- Temperatures → Task 8 (CPU) + Task 9 (GPU temp field). ✓
- Metric sources/formulas from spec §6 → Tasks 2–9 match verbatim (idle+iowait, io_ticks, sectors×512, USER_HZ). ✓
- History-on-connect for populated charts → Task 12 ring + `initMessage`, Task 14 `seedHistory`. ✓
- Config env table (spec §14) → Task 1 `config.go` covers all keys (+ `HOST_ROOT` added). ✓

**Deviations from spec (intentional, toward the same goals):**
1. Added `HOST_ROOT` + `/:/host/root:ro,rslave` mount — **required** for host filesystem statfs (spec §6 said statfs real mounts but omitted the mount; corrected here).
2. Frontend uses a small custom canvas `Chart` instead of uPlot — keeps the build self-contained (no fetched asset), aligns with the lightweight goal. Spec §5/§8 mentioned uPlot; this is an implementation-level substitution.
3. Runtime image `distroless/base-debian12` (glibc) instead of `scratch` — **required** because go-nvml needs cgo. Image target relaxed to ≤ ~40 MB (spec said < 30 MB); still tiny.
4. Added `/api/snapshot` + `/healthz` endpoints — enable dependency-free smoke testing and a polling fallback.

**Placeholder scan:** no TBD/TODO/"handle errors appropriately" left; every code step contains full code. ✓
**Type consistency:** `CPUTimes`, `NetCounters`, `DiskCounters`, `ProcSample`, `GPUReader`, `model.*` names are used identically across Tasks 2–14; `Tick(now time.Time)`, `New(cfg, gpu)`, hub `Register/Unregister/Broadcast`, WS envelope `{type,snap}` / `{type,history}` all match between producer and consumer tasks. ✓

---

## Execution Notes

- Run `CGO_ENABLED=1` for any local build/test that pulls in `internal/collect/gpu.go` (Tasks 9+). Pure-parser tests (Tasks 2–8) build without cgo.
- On a host without an NVIDIA GPU, `NewGPUReader()` returns the nop reader and the GPU tile hides itself — the app still runs.
- Tune `PROC_INTERVAL_MS` upward only if Task 15 step 6 shows the process scan is costly.
