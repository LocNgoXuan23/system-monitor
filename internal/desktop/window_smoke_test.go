//go:build linux

package desktop

import (
	"os"
	"testing"
)

// TestRunWindowSmoke opens a real window briefly. It needs a display, so it is
// skipped unless RUN_GUI_SMOKE is set. Run it manually with:
//
//	RUN_GUI_SMOKE=1 CGO_ENABLED=1 go test ./internal/desktop -run Smoke -v
func TestRunWindowSmoke(t *testing.T) {
	if os.Getenv("RUN_GUI_SMOKE") == "" {
		t.Skip("set RUN_GUI_SMOKE=1 (needs a display) to run the webview smoke test")
	}
	var gotW, gotH int
	RunWindow(WindowConfig{
		Title:       "smoke",
		URL:         "data:text/html,<h1>smoke</h1>",
		Width:       800,
		Height:      600,
		AutoCloseMS: 700,
		OnClose:     func(w, h int) { gotW, gotH = w, h },
	})
	if gotW <= 0 || gotH <= 0 {
		t.Fatalf("OnClose got size %dx%d, want positive dimensions", gotW, gotH)
	}
}
