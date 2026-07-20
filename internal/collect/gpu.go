package collect

import (
	"sort"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"system-monitor/internal/model"
)

type GPUReader interface {
	Read() []model.GPUInfo
	ReadProcs() []GPUProcSample
	Close()
}

type nopGPU struct{}

func (nopGPU) Read() []model.GPUInfo      { return nil }
func (nopGPU) ReadProcs() []GPUProcSample { return nil }
func (nopGPU) Close()                     {}

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

// ReadProcs lists every process holding VRAM on every device. NVML splits these
// across two calls by context type, and a process using both appears in both —
// MergeGPUProcs folds that back together.
func (g *nvmlGPU) ReadProcs() []GPUProcSample {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil
	}
	var out []GPUProcSample
	for i := 0; i < count; i++ {
		d, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}
		if ps, ret := d.GetComputeRunningProcesses(); ret == nvml.SUCCESS {
			for _, p := range ps {
				out = append(out, GPUProcSample{GPU: i, PID: int(p.Pid), VRAM: p.UsedGpuMemory, Compute: true})
			}
		}
		if ps, ret := d.GetGraphicsRunningProcesses(); ret == nvml.SUCCESS {
			for _, p := range ps {
				out = append(out, GPUProcSample{GPU: i, PID: int(p.Pid), VRAM: p.UsedGpuMemory, Graphics: true})
			}
		}
	}
	return out
}

// GPUProcSample is one process holding VRAM on one GPU, as NVML reports it: a
// device index, a PID, a byte count, and which context type it was listed
// under. NVML has no process names, so the collector resolves those from procfs.
type GPUProcSample struct {
	GPU      int
	PID      int
	VRAM     uint64
	Compute  bool
	Graphics bool
}

// vramUnavailable is NVML's NVML_VALUE_NOT_AVAILABLE sentinel for
// UsedGpuMemory, returned in configurations where per-process VRAM cannot be
// attributed (notably MIG).
const vramUnavailable = ^uint64(0)

// MergeGPUProcs folds per-device samples into one row per PID, and a PID listed
// under both context types becomes "C+G". Sorted by VRAM descending, ties by
// PID ascending so row order is stable between ticks.
//
// VRAM sums across devices but NOT across context types: a process holding both
// a compute and a graphics context appears in both NVML lists reporting the
// same device memory twice, so adding them would double what nvidia-smi shows.
// Per device we take the largest report; the two differ only by the moment each
// call sampled.
func MergeGPUProcs(samples []GPUProcSample) []model.GPUProcInfo {
	type acc struct {
		vram              uint64
		compute, graphics bool
	}
	type devKey struct{ gpu, pid int }
	byPID := map[int]*acc{}
	perDev := map[devKey]uint64{}
	var order []int
	for _, s := range samples {
		a, ok := byPID[s.PID]
		if !ok {
			a = &acc{}
			byPID[s.PID] = a
			order = append(order, s.PID)
		}
		a.compute = a.compute || s.Compute
		a.graphics = a.graphics || s.Graphics
		if s.VRAM == vramUnavailable {
			continue
		}
		if k := (devKey{s.GPU, s.PID}); s.VRAM > perDev[k] {
			perDev[k] = s.VRAM
		}
	}
	for k, vram := range perDev {
		byPID[k.pid].vram += vram
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
