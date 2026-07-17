package collect

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"system-monitor/internal/config"
	"system-monitor/internal/model"
)

type Collector struct {
	cfg   config.Config
	gpu   GPUReader
	disks []string

	// Immutable for the process lifetime, so they are read once in New()
	// rather than on every tick.
	osName  string
	kernel  string
	cpuModel string

	prevCPUAgg   CPUTimes
	prevCPUCores []CPUTimes
	prevNet      NetCounters
	prevDisk     map[string]DiskCounters
	prevProc     map[int]uint64
	prevTime     time.Time
	primed       bool
}

func New(cfg config.Config, gpu GPUReader) *Collector {
	disks, _ := ListDisks(cfg.HostSys, cfg.DiskExclude)
	return &Collector{
		cfg:      cfg,
		gpu:      gpu,
		disks:    disks,
		osName:   ReadOSName(cfg.HostRoot),
		kernel:   ReadKernel(cfg.HostProc),
		cpuModel: ReadCPUModel(cfg.HostProc),
		prevDisk: map[string]DiskCounters{},
		prevProc: map[int]uint64{},
	}
}

func (c *Collector) Tick(now time.Time) model.Snapshot {
	dt := now.Sub(c.prevTime).Seconds()
	if !c.primed {
		dt = 0
	}
	snap := model.Snapshot{T: now.Unix(), GPU: c.gpu.Read()}

	snap.Host = c.host()
	snap.CPU = c.cpu()
	if m, err := ReadMeminfo(c.cfg.HostProc); err == nil {
		snap.Mem = m
	}
	snap.CPU.Temp = ReadCPUTemp(c.cfg.HostSys)
	snap.CPU.Model = c.cpuModel
	snap.Net = c.net(dt)
	snap.Disk = c.disk(dt)
	snap.FS = ReadFS(c.cfg.HostProc, c.cfg.HostRoot)
	snap.Proc = c.procs(dt)

	c.prevTime = now
	c.primed = true
	return snap
}

func (c *Collector) host() model.HostInfo {
	h := model.HostInfo{}
	name, _ := os.Hostname()
	h.Name = name
	h.OS = c.osName
	h.Kernel = c.kernel
	if b, err := os.ReadFile(filepath.Join(c.cfg.HostProc, "uptime")); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			sec, _ := strconv.ParseFloat(f[0], 64)
			h.Uptime = int64(sec)
		}
	}
	if b, err := os.ReadFile(filepath.Join(c.cfg.HostProc, "loadavg")); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			h.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	return h
}

func (c *Collector) cpu() model.CPUInfo {
	agg, cores, err := ReadCPUStat(c.cfg.HostProc)
	if err != nil {
		return model.CPUInfo{}
	}
	info := model.CPUInfo{Cores: make([]float64, len(cores))}
	if c.primed {
		info.Agg = CPUPercent(c.prevCPUAgg, agg)
		for i := range cores {
			if i < len(c.prevCPUCores) {
				info.Cores[i] = CPUPercent(c.prevCPUCores[i], cores[i])
			}
		}
	}
	c.prevCPUAgg, c.prevCPUCores = agg, cores
	return info
}

func (c *Collector) net(dt float64) model.NetInfo {
	cur, err := ReadNetDev(c.cfg.HostProc)
	if err != nil {
		return model.NetInfo{}
	}
	n := model.NetInfo{RXTotal: cur.RX, TXTotal: cur.TX, Ifaces: cur.Ifaces}
	if c.primed && dt > 0 {
		n.RX = rate(c.prevNet.RX, cur.RX, dt)
		n.TX = rate(c.prevNet.TX, cur.TX, dt)
	}
	c.prevNet = cur
	return n
}

func (c *Collector) disk(dt float64) model.DiskInfo {
	keep := map[string]bool{}
	for _, d := range c.disks {
		keep[d] = true
	}
	cur, err := ReadDiskstats(c.cfg.HostProc, keep)
	if err != nil {
		return model.DiskInfo{}
	}
	var di model.DiskInfo
	for _, name := range c.disks {
		cc, ok := cur[name]
		if !ok {
			continue
		}
		rTot := cc.SectorsRead * sectorSize
		wTot := cc.SectorsWritten * sectorSize
		di.ReadTotal += rTot
		di.WriteTotal += wTot
		dev := model.DiskDev{Name: name, Model: DiskModel(c.cfg.HostSys, name)}
		if p, ok := c.prevDisk[name]; c.primed && ok && dt > 0 {
			dev.Read = rate(p.SectorsRead*sectorSize, rTot, dt)
			dev.Write = rate(p.SectorsWritten*sectorSize, wTot, dt)
			dev.Util = DiskUtil(p.IOTicks, cc.IOTicks, dt*1000)
			di.Read += dev.Read
			di.Write += dev.Write
		}
		di.Devs = append(di.Devs, dev)
	}
	c.prevDisk = cur
	return di
}

func (c *Collector) procs(dt float64) []model.ProcInfo {
	samples := ScanProcs(c.cfg.HostProc)
	next := make(map[int]uint64, len(samples))
	var out []model.ProcInfo
	for _, s := range samples {
		next[s.PID] = s.Jiffies
		cpu := 0.0
		if prev, ok := c.prevProc[s.PID]; c.primed && ok && dt > 0 && s.Jiffies >= prev {
			// (djiffies/USER_HZ)/dt*100 simplifies to djiffies/dt when USER_HZ=100.
			cpu = float64(s.Jiffies-prev) / dt
		}
		out = append(out, model.ProcInfo{PID: s.PID, Name: s.Name, CPU: cpu, RSS: s.RSS})
	}
	c.prevProc = next
	sort.Slice(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	if len(out) > c.cfg.ProcTopN {
		out = out[:c.cfg.ProcTopN]
	}
	return out
}

func rate(prev, cur uint64, dt float64) uint64 {
	if cur < prev || dt <= 0 {
		return 0
	}
	return uint64(float64(cur-prev) / dt)
}
