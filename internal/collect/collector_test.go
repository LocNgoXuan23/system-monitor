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
