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
