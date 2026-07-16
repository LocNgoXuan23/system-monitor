//go:build linux

package desktop

import "testing"

func TestWindowSizeRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 1000, Height: 700}); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowSize()
	if got.Width != 1000 || got.Height != 700 {
		t.Errorf("got %+v, want {1000 700}", got)
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
