package collect

import (
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
