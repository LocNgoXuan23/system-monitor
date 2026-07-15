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

func TestLoadNonPositiveIntervalFallsBackToDefault(t *testing.T) {
	for _, v := range []string{"0", "-5", "abc"} {
		t.Setenv("INTERVAL_MS", v)
		if got := Load().IntervalMS; got != 1000 {
			t.Errorf("INTERVAL_MS=%q: IntervalMS = %d, want default 1000", v, got)
		}
	}
}
