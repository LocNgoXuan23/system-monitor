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
