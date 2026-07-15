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
