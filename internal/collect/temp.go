package collect

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var cpuTempSensors = map[string]bool{"coretemp": true, "k10temp": true, "zenpower": true}

// FindCPUTemp scans hwmon directories for a CPU sensor and returns its first
// temperature input in °C, or 0 if none is found.
func FindCPUTemp(hwmonDir string) float64 {
	dirs, err := os.ReadDir(hwmonDir)
	if err != nil {
		return 0
	}
	for _, d := range dirs {
		base := filepath.Join(hwmonDir, d.Name())
		name, err := os.ReadFile(filepath.Join(base, "name"))
		if err != nil || !cpuTempSensors[strings.TrimSpace(string(name))] {
			continue
		}
		files, _ := os.ReadDir(base)
		var inputs []string
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "temp") && strings.HasSuffix(f.Name(), "_input") {
				inputs = append(inputs, f.Name())
			}
		}
		if len(inputs) == 0 {
			continue
		}
		sort.Strings(inputs)
		b, err := os.ReadFile(filepath.Join(base, inputs[0]))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		if err != nil {
			continue
		}
		return milli / 1000
	}
	return 0
}

func ReadCPUTemp(hostSys string) float64 {
	return FindCPUTemp(filepath.Join(hostSys, "class", "hwmon"))
}
