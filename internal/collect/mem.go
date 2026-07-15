package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"system-monitor/internal/model"
)

func ParseMeminfo(r io.Reader) (model.MemInfo, error) {
	v := map[string]uint64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		v[strings.TrimSuffix(f[0], ":")] = n * 1024 // kB -> bytes
	}
	m := model.MemInfo{
		Total:     v["MemTotal"],
		Cache:     v["Cached"] + v["Buffers"] + v["SReclaimable"],
		SwapTotal: v["SwapTotal"],
	}
	if m.Total >= v["MemAvailable"] {
		m.Used = m.Total - v["MemAvailable"]
	}
	if m.SwapTotal >= v["SwapFree"] {
		m.SwapUsed = m.SwapTotal - v["SwapFree"]
	}
	if m.Total > 0 {
		m.Pct = float64(m.Used) / float64(m.Total) * 100
	}
	if m.SwapTotal > 0 {
		m.SwapPct = float64(m.SwapUsed) / float64(m.SwapTotal) * 100
	}
	return m, sc.Err()
}

func ReadMeminfo(hostProc string) (model.MemInfo, error) {
	f, err := os.Open(filepath.Join(hostProc, "meminfo"))
	if err != nil {
		return model.MemInfo{}, err
	}
	defer f.Close()
	return ParseMeminfo(f)
}
