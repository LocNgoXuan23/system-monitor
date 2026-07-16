package config

import (
	"os"
	"testing"
)

func TestWebDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "INTERVAL_MS", "HISTORY_SECONDS", "PROC_TOP_N", "HOST_PROC", "HOST_ROOT"} {
		os.Unsetenv(k)
	}
	c := Load(WebDefaults())
	if c.Port != "8090" {
		t.Errorf("Port = %q, want 8090", c.Port)
	}
	if c.IntervalMS != 1000 {
		t.Errorf("IntervalMS = %d, want 1000", c.IntervalMS)
	}
	if c.HostProc != "/host/proc" {
		t.Errorf("HostProc = %q, want /host/proc", c.HostProc)
	}
	if c.HostRoot != "/host/root" {
		t.Errorf("HostRoot = %q, want /host/root", c.HostRoot)
	}
	if len(c.DiskExclude) != 4 {
		t.Errorf("DiskExclude len = %d, want 4", len(c.DiskExclude))
	}
}

func TestDesktopDefaults(t *testing.T) {
	for _, k := range []string{"HOST_PROC", "HOST_SYS", "HOST_ROOT"} {
		os.Unsetenv(k)
	}
	c := Load(DesktopDefaults())
	if c.HostProc != "/proc" {
		t.Errorf("HostProc = %q, want /proc", c.HostProc)
	}
	if c.HostSys != "/sys" {
		t.Errorf("HostSys = %q, want /sys", c.HostSys)
	}
	if c.HostRoot != "" {
		t.Errorf("HostRoot = %q, want empty", c.HostRoot)
	}
}

func TestEnvOverridesDefaults(t *testing.T) {
	t.Setenv("HOST_PROC", "/custom/proc")
	t.Setenv("INTERVAL_MS", "500")
	c := Load(DesktopDefaults())
	if c.HostProc != "/custom/proc" {
		t.Errorf("HostProc = %q, want /custom/proc", c.HostProc)
	}
	if c.IntervalMS != 500 {
		t.Errorf("IntervalMS = %d, want 500", c.IntervalMS)
	}
}

func TestNonPositiveIntervalFallsBackToDefault(t *testing.T) {
	for _, v := range []string{"0", "-5", "abc"} {
		t.Setenv("INTERVAL_MS", v)
		if got := Load(WebDefaults()).IntervalMS; got != 1000 {
			t.Errorf("INTERVAL_MS=%q: IntervalMS = %d, want default 1000", v, got)
		}
	}
}
