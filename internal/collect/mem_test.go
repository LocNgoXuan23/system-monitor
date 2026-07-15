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
