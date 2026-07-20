package collect

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestReadProcName(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "77"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "77", "comm"), []byte("python3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := ReadProcName(root, 77); got != "python3" {
		t.Errorf("name = %q, want %q", got, "python3")
	}
	// A process that exited between the NVML sample and this read is a normal
	// race, not an error: the caller still shows the row it sampled.
	if got := ReadProcName(root, 78); got != "" {
		t.Errorf("name = %q, want empty", got)
	}
}
