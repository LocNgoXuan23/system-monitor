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
// the hint) must not reopen a window too small to lay out. Each axis clamps
// independently, so a size under the floor on only one axis keeps the other.
func TestLoadClampsBelowMinimum(t *testing.T) {
	cases := []struct {
		name  string
		saved WindowSize
		wantW int
		wantH int
	}{
		{"both below", WindowSize{800, 600}, MinWidth, MinHeight},
		{"width only below", WindowSize{800, 900}, MinWidth, 900},
		{"height only below", WindowSize{1400, 600}, 1400, MinHeight},
		{"exactly at the floor", WindowSize{MinWidth, MinHeight}, MinWidth, MinHeight},
		{"one pixel under", WindowSize{MinWidth - 1, MinHeight - 1}, MinWidth, MinHeight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if err := SaveWindowSize(tc.saved); err != nil {
				t.Fatal(err)
			}
			got := LoadWindowSize()
			if got.Width != tc.wantW || got.Height != tc.wantH {
				t.Errorf("saved %+v: got %+v, want {%d %d}", tc.saved, got, tc.wantW, tc.wantH)
			}
		})
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
