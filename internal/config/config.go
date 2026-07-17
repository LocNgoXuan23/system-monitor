package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port           string
	IntervalMS     int
	HistorySec     int
	ProcTopN       int
	ProcIntervalMS int // reserved: optional slower cadence for process scans (not yet wired)
	HostProc       string
	HostSys        string
	HostRoot       string
	DiskExclude    []string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// Defaults holds the per-head default values that differ between the web and
// desktop form factors. Environment variables still override these.
type Defaults struct {
	Port     string
	HostProc string
	HostSys  string
	HostRoot string
}

// WebDefaults are the defaults for the Docker web head (host paths bind-mounted
// under /host).
func WebDefaults() Defaults {
	return Defaults{Port: "8090", HostProc: "/host/proc", HostSys: "/host/sys", HostRoot: "/host/root"}
}

// DesktopDefaults are the defaults for the native desktop head, which reads the
// real host filesystem directly. HostRoot is "" so filesystem mount paths are
// used as-is (filepath.Join("", "/x") == "/x").
func DesktopDefaults() Defaults {
	return Defaults{Port: "0", HostProc: "/proc", HostSys: "/sys", HostRoot: ""}
}

func Load(d Defaults) Config {
	interval := envInt("INTERVAL_MS", 1000)
	return Config{
		Port:           env("PORT", d.Port),
		IntervalMS:     interval,
		HistorySec:     envInt("HISTORY_SECONDS", 60),
		ProcTopN:       envInt("PROC_TOP_N", 25),
		ProcIntervalMS: envInt("PROC_INTERVAL_MS", interval),
		HostProc:       env("HOST_PROC", d.HostProc),
		HostSys:        env("HOST_SYS", d.HostSys),
		HostRoot:       env("HOST_ROOT", d.HostRoot),
		DiskExclude:    strings.Split(env("DISK_EXCLUDE", "loop,ram,zram,dm-"), ","),
	}
}
