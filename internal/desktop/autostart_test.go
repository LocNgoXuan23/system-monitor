//go:build linux

package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopEntryContainsExec(t *testing.T) {
	entry := desktopEntry("/home/u/.local/bin/system-monitor-desktop")
	if !strings.Contains(entry, "[Desktop Entry]") {
		t.Error("missing [Desktop Entry] header")
	}
	if !strings.Contains(entry, "Exec=/home/u/.local/bin/system-monitor-desktop") {
		t.Errorf("missing Exec line, got:\n%s", entry)
	}
	if !strings.Contains(entry, "Icon=system-monitor") {
		t.Errorf("missing Icon line, got:\n%s", entry)
	}
}

func TestInstallThenRemoveAutostart(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)

	p, err := InstallAutostart("/opt/sm")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cfg, "autostart", "system-monitor.desktop")
	if p != want {
		t.Errorf("path = %q, want %q", p, want)
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "Exec=/opt/sm") {
		t.Fatalf("bad autostart file: %v / %q", err, b)
	}

	if _, err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("file should be gone after remove")
	}
	if _, err := RemoveAutostart(); err != nil { // idempotent
		t.Errorf("second remove should be a no-op, got %v", err)
	}
}
