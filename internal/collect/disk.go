package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const sectorSize = 512

type DiskCounters struct {
	SectorsRead    uint64
	SectorsWritten uint64
	IOTicks        uint64
}

func ParseDiskstats(r io.Reader, keep map[string]bool) map[string]DiskCounters {
	out := map[string]DiskCounters{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 13 {
			continue
		}
		name := f[2]
		if !keep[name] {
			continue
		}
		sr, _ := strconv.ParseUint(f[5], 10, 64)
		sw, _ := strconv.ParseUint(f[9], 10, 64)
		ticks, _ := strconv.ParseUint(f[12], 10, 64)
		out[name] = DiskCounters{SectorsRead: sr, SectorsWritten: sw, IOTicks: ticks}
	}
	return out
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// ListDisks returns whole-disk device names from /sys/block minus excluded prefixes.
func ListDisks(hostSys string, exclude []string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(hostSys, "block"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if hasAnyPrefix(name, exclude) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func DiskModel(hostSys, name string) string {
	b, err := os.ReadFile(filepath.Join(hostSys, "block", name, "device", "model"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func DiskUtil(prevTicks, curTicks uint64, dtMS float64) float64 {
	if dtMS <= 0 || curTicks < prevTicks {
		return 0
	}
	pct := float64(curTicks-prevTicks) / dtMS * 100
	if pct > 100 {
		return 100
	}
	if pct < 0 {
		return 0
	}
	return pct
}

func ReadDiskstats(hostProc string, keep map[string]bool) (map[string]DiskCounters, error) {
	f, err := os.Open(filepath.Join(hostProc, "diskstats"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseDiskstats(f, keep), nil
}
