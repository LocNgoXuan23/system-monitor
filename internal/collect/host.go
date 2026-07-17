package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ParseOSRelease returns the PRETTY_NAME value from an os-release file, or ""
// if the key is absent. Values are optionally quoted; the quotes are stripped.
func ParseOSRelease(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
	}
	return ""
}

// ReadOSName reads the host distro's PRETTY_NAME. hostRoot is the prefix under
// which the host filesystem is visible ("" natively, "/host/root" in the
// container) so the web app reports the host's distro, not the image's.
// The suffix must be absolute (leading /) so filepath.Join behaves correctly
// even when hostRoot is empty: filepath.Join("", "/etc/os-release") == "/etc/os-release".
// Returns "" when unavailable; the UI then omits the field.
func ReadOSName(hostRoot string) string {
	f, err := os.Open(filepath.Join(hostRoot, "/etc/os-release"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return ParseOSRelease(f)
}

// ReadKernel reads the running kernel release. /proc is the host's /proc in
// both form factors, so this is the host kernel either way.
func ReadKernel(hostProc string) string {
	b, err := os.ReadFile(filepath.Join(hostProc, "sys", "kernel", "osrelease"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
