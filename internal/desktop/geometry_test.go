//go:build linux

package desktop

import "testing"

func TestWindowSizeRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 1200, Height: 800}); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowSize()
	if got.Width != 1200 || got.Height != 800 {
		t.Errorf("got %+v, want {1200 800}", got)
	}
}

// A size saved before the minimum existed (or by a window manager that ignored
// the hint) must not reopen a window too small to lay out.
func TestLoadClampsBelowMinimum(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 800, Height: 600}); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowSize()
	if got.Width != MinWidth || got.Height != MinHeight {
		t.Errorf("got %+v, want clamped to {%d %d}", got, MinWidth, MinHeight)
	}
}

func TestLoadDefaultWhenMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got := LoadWindowSize()
	if got.Width != defaultWidth || got.Height != defaultHeight {
		t.Errorf("got %+v, want default {%d %d}", got, defaultWidth, defaultHeight)
	}
}

func TestSaveRejectsNonPositive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 0, Height: -1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := LoadWindowSize() // nothing was written; default returned
	if got.Width != defaultWidth {
		t.Errorf("got %+v, want default", got)
	}
}
