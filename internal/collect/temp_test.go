package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCPUTemp(t *testing.T) {
	root := t.TempDir()
	h := filepath.Join(root, "hwmon0")
	if err := os.MkdirAll(h, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(h, "name"), []byte("coretemp\n"), 0644)
	os.WriteFile(filepath.Join(h, "temp1_input"), []byte("58000\n"), 0644)

	if got := FindCPUTemp(root); got != 58 {
		t.Errorf("FindCPUTemp = %v, want 58", got)
	}
}

func TestFindCPUTempNone(t *testing.T) {
	if got := FindCPUTemp(t.TempDir()); got != 0 {
		t.Errorf("FindCPUTemp = %v, want 0", got)
	}
}
