package collect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const pageSize = 4096

type ProcSample struct {
	PID     int
	Name    string
	Jiffies uint64
	RSS     uint64
}

func ParseProcStat(pid int, content string) (ProcSample, bool) {
	open := strings.IndexByte(content, '(')
	closeIdx := strings.LastIndexByte(content, ')')
	if open < 0 || closeIdx < 0 || closeIdx < open {
		return ProcSample{}, false
	}
	name := content[open+1 : closeIdx]
	rest := strings.Fields(content[closeIdx+1:])
	// rest[0]=state(field3); utime=field14->rest[11]; stime=field15->rest[12]; rss=field24->rest[21]
	if len(rest) < 22 {
		return ProcSample{}, false
	}
	utime, _ := strconv.ParseUint(rest[11], 10, 64)
	stime, _ := strconv.ParseUint(rest[12], 10, 64)
	rss, _ := strconv.ParseUint(rest[21], 10, 64)
	return ProcSample{PID: pid, Name: name, Jiffies: utime + stime, RSS: rss * pageSize}, true
}

// ScanProcs reads every /host/proc/[pid]/stat. Host PIDs are visible because
// /host/proc is a bind mount of the host procfs.
func ScanProcs(hostProc string) []ProcSample {
	entries, err := os.ReadDir(hostProc)
	if err != nil {
		return nil
	}
	var out []ProcSample
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(hostProc, e.Name(), "stat"))
		if err != nil {
			continue
		}
		if s, ok := ParseProcStat(pid, string(b)); ok {
			out = append(out, s)
		}
	}
	return out
}
