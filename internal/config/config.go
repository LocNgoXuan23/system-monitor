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
	ProcIntervalMS int
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

func Load() Config {
	interval := envInt("INTERVAL_MS", 1000)
	return Config{
		Port:           env("PORT", "8080"),
		IntervalMS:     interval,
		HistorySec:     envInt("HISTORY_SECONDS", 60),
		ProcTopN:       envInt("PROC_TOP_N", 8),
		ProcIntervalMS: envInt("PROC_INTERVAL_MS", interval),
		HostProc:       env("HOST_PROC", "/host/proc"),
		HostSys:        env("HOST_SYS", "/host/sys"),
		HostRoot:       env("HOST_ROOT", "/host/root"),
		DiskExclude:    strings.Split(env("DISK_EXCLUDE", "loop,ram,zram,dm-"), ","),
	}
}
