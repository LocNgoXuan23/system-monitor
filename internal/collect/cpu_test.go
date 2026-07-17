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

func TestParseCPUStatExcludesGuest(t *testing.T) {
	// guest (col 9) and guest_nice (col 10) are already included in user/nice,
	// so they must NOT be added to Total again.
	in := "cpu  100 0 50 800 50 0 0 0 30 5\n"
	agg, _, err := ParseCPUStat(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	// Total = user+nice+system+idle+iowait+irq+softirq+steal = 100+0+50+800+50 = 1000
	// (guest=30, guest_nice=5 excluded); Idle = idle+iowait = 850.
	if agg.Total != 1000 || agg.Idle != 850 {
		t.Fatalf("agg = %+v, want {1000 850}", agg)
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
