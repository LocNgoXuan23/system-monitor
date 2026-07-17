package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	in := "NAME=\"Ubuntu\"\n" +
		"VERSION_ID=\"24.04\"\n" +
		"PRETTY_NAME=\"Ubuntu 24.04.4 LTS\"\n" +
		"ID=ubuntu\n"
	if got := ParseOSRelease(strings.NewReader(in)); got != "Ubuntu 24.04.4 LTS" {
		t.Errorf("got %q, want %q", got, "Ubuntu 24.04.4 LTS")
	}
}

func TestParseOSReleaseUnquoted(t *testing.T) {
	if got := ParseOSRelease(strings.NewReader("PRETTY_NAME=Alpine Linux v3.20\n")); got != "Alpine Linux v3.20" {
		t.Errorf("got %q", got)
	}
}

func TestParseOSReleaseMissingKey(t *testing.T) {
	if got := ParseOSRelease(strings.NewReader("NAME=\"Weird\"\nID=weird\n")); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadOSNameMissingFile(t *testing.T) {
	if got := ReadOSName(t.TempDir()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadOSNameEmptyPrefix(t *testing.T) {
	// Regression test for empty hostRoot prefix (desktop form factor).
	// The bug was filepath.Join("", "etc", "os-release") produced relative path "etc/os-release".
	// Fixed by using absolute suffix: filepath.Join("", "/etc/os-release") == "/etc/os-release".
	f, err := os.Open("/etc/os-release")
	if err != nil {
		t.Skip("no /etc/os-release on this host")
	}
	defer f.Close()
	expected := ParseOSRelease(f)
	if expected == "" {
		t.Skip("PRETTY_NAME not found in /etc/os-release")
	}
	if got := ReadOSName(""); got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestReadKernel(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sys", "kernel"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "sys", "kernel", "osrelease")
	if err := os.WriteFile(p, []byte("6.17.0-40-generic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadKernel(dir); got != "6.17.0-40-generic" {
		t.Errorf("got %q, want %q", got, "6.17.0-40-generic")
	}
}

func TestReadKernelMissingFile(t *testing.T) {
	if got := ReadKernel(t.TempDir()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
