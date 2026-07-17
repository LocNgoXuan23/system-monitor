//go:build linux

package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WindowSize is the persisted native-window size.
type WindowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

const (
	defaultWidth  = 1440
	defaultHeight = 900

	// MinWidth/MinHeight are the smallest window the dashboard lays out
	// correctly in. Below this the charts collapse to unreadable slivers and
	// the 32-core grid disappears, so GTK is told to refuse smaller sizes.
	MinWidth  = 1100
	MinHeight = 780
)

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "system-monitor"), nil
}

// LoadWindowSize returns the saved size, or the default if missing or invalid.
func LoadWindowSize() WindowSize {
	def := WindowSize{Width: defaultWidth, Height: defaultHeight}
	dir, err := configDir()
	if err != nil {
		return def
	}
	b, err := os.ReadFile(filepath.Join(dir, "window.json"))
	if err != nil {
		return def
	}
	var ws WindowSize
	if json.Unmarshal(b, &ws) != nil || ws.Width <= 0 || ws.Height <= 0 {
		return def
	}
	if ws.Width < MinWidth {
		ws.Width = MinWidth
	}
	if ws.Height < MinHeight {
		ws.Height = MinHeight
	}
	return ws
}

// SaveWindowSize persists the size, creating the config dir if needed. A
// non-positive size is ignored (returns nil) so a bad close reading can't
// clobber a good saved value.
func SaveWindowSize(ws WindowSize) error {
	if ws.Width <= 0 || ws.Height <= 0 {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(ws)
	return os.WriteFile(filepath.Join(dir, "window.json"), b, 0o644)
}
