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
func (fakeGPU) ReadProcs() []GPUProcSample {
	return []GPUProcSample{{PID: 42, VRAM: 512 << 20, Compute: true}}
}
func (fakeGPU) Close() {}

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
	writeFile(t, filepath.Join(proc, "uptime"), "3600.00 1000.00\n")
	writeFile(t, filepath.Join(proc, "42/comm"), "trainer\n")
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
	if len(snap.GPUProc) != 1 || snap.GPUProc[0].Name != "trainer" ||
		snap.GPUProc[0].Type != "C" || snap.GPUProc[0].VRAM != 512<<20 {
		t.Errorf("gpu proc = %+v", snap.GPUProc)
	}
	if len(snap.Disk.Devs) != 1 || snap.Disk.Devs[0].Name != "sda" {
		t.Errorf("disk devs = %+v", snap.Disk.Devs)
	}
}

func TestCollectorTickDeltas(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	sys := filepath.Join(root, "sys")

	// Initial counters. Tick 1 primes on these; tick 2 re-reads them unchanged.
	writeFile(t, filepath.Join(proc, "stat"), "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 100 0 50 800 50 0 0 0 0 0\n")
	writeFile(t, filepath.Join(proc, "meminfo"), "MemTotal: 1000 kB\nMemAvailable: 600 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	writeFile(t, filepath.Join(proc, "net/dev"), "  eth0: 5000 0 0 0 0 0 0 0 2000 0 0 0 0 0 0 0\n")
	writeFile(t, filepath.Join(proc, "diskstats"), "8 0 sda 0 0 2000 0 0 0 1000 0 0 500 0\n")
	writeFile(t, filepath.Join(proc, "uptime"), "3600.00 1000.00\n")
	writeFile(t, filepath.Join(proc, "1234/stat"), "1234 (testproc) R 1 1 1 0 -1 0 0 0 0 0 100 0 0 0 20 0 1 0 999 123456 2048\n")
	writeFile(t, filepath.Join(sys, "block/sda/device/model"), "TEST DISK\n")

	cfg := config.Config{HostProc: proc, HostSys: sys, HostRoot: root, ProcTopN: 5, DiskExclude: []string{"loop"}}
	c := New(cfg, fakeGPU{})

	t0 := time.Unix(1000, 0)
	c.Tick(t0)                          // tick 1: primes previous sample
	zero := c.Tick(t0.Add(time.Second)) // tick 2: inputs unchanged -> all deltas 0

	if zero.CPU.Agg != 0 || zero.Net.RX != 0 || zero.Net.TX != 0 {
		t.Errorf("tick2 should have zero rates: cpu=%v net=%+v", zero.CPU.Agg, zero.Net)
	}
	if len(zero.Disk.Devs) == 1 && (zero.Disk.Devs[0].Read != 0 || zero.Disk.Devs[0].Write != 0 || zero.Disk.Devs[0].Util != 0) {
		t.Errorf("tick2 disk deltas should be zero: %+v", zero.Disk.Devs[0])
	}

	// Advance the counters, then tick again over a 1s interval.
	// total 1000->1100, idle+iowait 850->900 => 50% busy.
	writeFile(t, filepath.Join(proc, "stat"), "cpu  100 0 100 850 50 0 0 0 0 0\ncpu0 100 0 100 850 50 0 0 0 0 0\n")
	// rx +3000, tx +1000 over 1s.
	writeFile(t, filepath.Join(proc, "net/dev"), "  eth0: 8000 0 0 0 0 0 0 0 3000 0 0 0 0 0 0 0\n")
	// sectors read/written +10 each (*512 = 5120 B/s); io_ticks +250ms over 1000ms => 25% util.
	writeFile(t, filepath.Join(proc, "diskstats"), "8 0 sda 0 0 2010 0 0 0 1010 0 0 750 0\n")
	// jiffies 100->200 over 1s, USER_HZ=100 => 100%/core.
	writeFile(t, filepath.Join(proc, "1234/stat"), "1234 (testproc) R 1 1 1 0 -1 0 0 0 0 0 200 0 0 0 20 0 1 0 999 123456 2048\n")

	snap := c.Tick(t0.Add(2 * time.Second))

	if snap.CPU.Agg != 50 {
		t.Errorf("CPU.Agg = %v, want 50", snap.CPU.Agg)
	}
	if len(snap.CPU.Cores) != 1 || snap.CPU.Cores[0] != 50 {
		t.Errorf("CPU.Cores = %+v, want [50]", snap.CPU.Cores)
	}
	if snap.Net.RX != 3000 || snap.Net.TX != 1000 {
		t.Errorf("Net = %+v, want RX=3000 TX=1000", snap.Net)
	}
	if len(snap.Disk.Devs) != 1 {
		t.Fatalf("disk devs = %+v, want 1", snap.Disk.Devs)
	}
	if snap.Disk.Devs[0].Read != 5120 || snap.Disk.Devs[0].Write != 5120 {
		t.Errorf("disk read/write = %d/%d, want 5120/5120", snap.Disk.Devs[0].Read, snap.Disk.Devs[0].Write)
	}
	if snap.Disk.Devs[0].Util != 25 {
		t.Errorf("disk util = %v, want 25", snap.Disk.Devs[0].Util)
	}
	var found bool
	for _, p := range snap.Proc {
		if p.Name == "testproc" {
			found = true
			if p.CPU != 100 {
				t.Errorf("proc testproc CPU = %v, want 100", p.CPU)
			}
		}
	}
	if !found {
		t.Errorf("proc testproc not found in %+v", snap.Proc)
	}
}
