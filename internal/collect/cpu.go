package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type CPUTimes struct {
	Total uint64
	Idle  uint64
}

// ParseCPUStat parses /proc/stat cpu lines into cumulative totals.
// idle counts the idle (field 4) + iowait (field 5) columns.
func ParseCPUStat(r io.Reader) (agg CPUTimes, cores []CPUTimes, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		var t CPUTimes
		// Sum only user..steal (cols 1-8). Columns 9-10 (guest, guest_nice) are
		// already folded into user/nice by the kernel; adding them again would
		// double-count guest time and inflate CPU%.
		for i := 1; i < len(f) && i <= 8; i++ {
			v, e := strconv.ParseUint(f[i], 10, 64)
			if e != nil {
				continue
			}
			t.Total += v
			if i == 4 || i == 5 { // idle, iowait
				t.Idle += v
			}
		}
		if f[0] == "cpu" {
			agg = t
		} else {
			cores = append(cores, t)
		}
	}
	return agg, cores, sc.Err()
}

func CPUPercent(prev, cur CPUTimes) float64 {
	dt := cur.Total - prev.Total
	if dt == 0 {
		return 0
	}
	di := cur.Idle - prev.Idle
	pct := float64(dt-di) / float64(dt) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func ReadCPUStat(hostProc string) (CPUTimes, []CPUTimes, error) {
	f, err := os.Open(filepath.Join(hostProc, "stat"))
	if err != nil {
		return CPUTimes{}, nil, err
	}
	defer f.Close()
	return ParseCPUStat(f)
}

// ParseCPUModel returns the first "model name" value from /proc/cpuinfo, or ""
// if the key is absent (some ARM kernels omit it).
func ParseCPUModel(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "model name") {
			continue
		}
		if i := strings.IndexByte(line, ':'); i >= 0 {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
}

// ReadCPUModel reads the CPU model name. Constant for the process lifetime, so
// callers should read it once rather than per tick.
func ReadCPUModel(hostProc string) string {
	f, err := os.Open(filepath.Join(hostProc, "cpuinfo"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return ParseCPUModel(f)
}
