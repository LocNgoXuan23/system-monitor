package collect

import (
	"sort"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"system-monitor/internal/model"
)

type GPUReader interface {
	Read() []model.GPUInfo
	Close()
}

type nopGPU struct{}

func (nopGPU) Read() []model.GPUInfo { return nil }
func (nopGPU) Close()                {}

type nvmlGPU struct{}

// NewGPUReader initialises NVML. If unavailable (no GPU / no driver), it returns a
// nop reader so the rest of the app runs normally with the GPU tile hidden.
func NewGPUReader() GPUReader {
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		return nopGPU{}
	}
	return &nvmlGPU{}
}

func (g *nvmlGPU) Close() { nvml.Shutdown() }

func (g *nvmlGPU) Read() []model.GPUInfo {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil
	}
	var out []model.GPUInfo
	for i := 0; i < count; i++ {
		d, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}
		info := model.GPUInfo{Fan: -1}
		if name, ret := d.GetName(); ret == nvml.SUCCESS {
			info.Name = name
		}
		if u, ret := d.GetUtilizationRates(); ret == nvml.SUCCESS {
			info.Util = int(u.Gpu)
		}
		if m, ret := d.GetMemoryInfo(); ret == nvml.SUCCESS {
			info.MemUsed = m.Used
			info.MemTotal = m.Total
		}
		if temp, ret := d.GetTemperature(nvml.TEMPERATURE_GPU); ret == nvml.SUCCESS {
			info.Temp = int(temp)
		}
		if p, ret := d.GetPowerUsage(); ret == nvml.SUCCESS {
			info.Power = int(p) / 1000 // mW -> W
		}
		if c, ret := d.GetClockInfo(nvml.CLOCK_SM); ret == nvml.SUCCESS {
			info.ClkSM = int(c)
		}
		if f, ret := d.GetFanSpeed(); ret == nvml.SUCCESS {
			info.Fan = int(f)
		}
		out = append(out, info)
	}
	return out
}

// GPUProcSample is one process holding VRAM on one GPU, as NVML reports it: a
// PID, a byte count, and which context type it was listed under. NVML has no
// process names, so the collector resolves those from procfs.
type GPUProcSample struct {
	PID      int
	VRAM     uint64
	Compute  bool
	Graphics bool
}

// vramUnavailable is NVML's NVML_VALUE_NOT_AVAILABLE sentinel for
// UsedGpuMemory, returned in configurations where per-process VRAM cannot be
// attributed (notably MIG).
const vramUnavailable = ^uint64(0)

// MergeGPUProcs folds per-device samples into one row per PID: VRAM sums across
// devices, and a PID listed under both context types becomes "C+G". Sorted by
// VRAM descending, ties by PID ascending so row order is stable between ticks.
func MergeGPUProcs(samples []GPUProcSample) []model.GPUProcInfo {
	type acc struct {
		vram              uint64
		compute, graphics bool
	}
	byPID := map[int]*acc{}
	var order []int
	for _, s := range samples {
		a, ok := byPID[s.PID]
		if !ok {
			a = &acc{}
			byPID[s.PID] = a
			order = append(order, s.PID)
		}
		if s.VRAM != vramUnavailable {
			a.vram += s.VRAM
		}
		a.compute = a.compute || s.Compute
		a.graphics = a.graphics || s.Graphics
	}
	out := make([]model.GPUProcInfo, 0, len(order))
	for _, pid := range order {
		a := byPID[pid]
		t := ""
		switch {
		case a.compute && a.graphics:
			t = "C+G"
		case a.compute:
			t = "C"
		case a.graphics:
			t = "G"
		}
		out = append(out, model.GPUProcInfo{PID: pid, Type: t, VRAM: a.vram})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VRAM != out[j].VRAM {
			return out[i].VRAM > out[j].VRAM
		}
		return out[i].PID < out[j].PID
	})
	return out
}
