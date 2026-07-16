//go:build linux

package desktop

import (
	"os"
	"path/filepath"
)

const autostartFilename = "system-monitor.desktop"

func autostartDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "autostart"), nil
}

// desktopEntry renders an autostart .desktop entry launching execPath.
func desktopEntry(execPath string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=System Monitor\n" +
		"Comment=Live system resource monitor\n" +
		"Exec=" + execPath + "\n" +
		"Icon=system-monitor\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n"
}

// InstallAutostart writes the autostart entry pointing at execPath and returns
// the file path written.
func InstallAutostart(execPath string) (string, error) {
	dir, err := autostartDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, autostartFilename)
	if err := os.WriteFile(p, []byte(desktopEntry(execPath)), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// RemoveAutostart deletes the autostart entry if present. It is idempotent.
func RemoveAutostart() (string, error) {
	dir, err := autostartDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, autostartFilename)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return p, nil
}
