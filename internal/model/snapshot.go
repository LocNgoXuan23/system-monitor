package model

// Snapshot is one full sample broadcast each tick. All byte fields are bytes,
// rates are bytes/second.
type Snapshot struct {
	Host HostInfo   `json:"host"`
	CPU  CPUInfo    `json:"cpu"`
	Mem  MemInfo    `json:"mem"`
	Net  NetInfo    `json:"net"`
	Disk DiskInfo   `json:"disk"`
	GPU  []GPUInfo  `json:"gpu"`
	FS   []FSInfo   `json:"fs"`
	Proc []ProcInfo `json:"proc"`
}

type HostInfo struct {
	Name   string `json:"name"`
	OS     string `json:"os"`     // distro PRETTY_NAME; "" if unknown
	Kernel string `json:"kernel"` // kernel release; "" if unknown
	Uptime int64  `json:"uptime"`
}

type CPUInfo struct {
	Agg   float64   `json:"agg"`
	Cores []float64 `json:"cores"`
	Temp  float64   `json:"temp"`  // 0 if unknown
	Model string    `json:"model"` // e.g. "Intel(R) Core(TM) i9-14900K"; "" if unknown
}

type MemInfo struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Cache     uint64  `json:"cache"`
	Pct       float64 `json:"pct"`
	SwapTotal uint64  `json:"swap_total"`
	SwapUsed  uint64  `json:"swap_used"`
	SwapPct   float64 `json:"swap_pct"`
}

type NetInfo struct {
	RX      uint64   `json:"rx"`
	TX      uint64   `json:"tx"`
	RXTotal uint64   `json:"rx_total"`
	TXTotal uint64   `json:"tx_total"`
	Ifaces  []string `json:"ifaces"` // physical interfaces being summed
}

type DiskDev struct {
	Name  string  `json:"name"`
	Util  float64 `json:"util"`
	Read  uint64  `json:"read"`
	Write uint64  `json:"write"`
	Model string  `json:"model"`
}

type DiskInfo struct {
	Read       uint64    `json:"read"`
	Write      uint64    `json:"write"`
	ReadTotal  uint64    `json:"read_total"`
	WriteTotal uint64    `json:"write_total"`
	Devs       []DiskDev `json:"devs"`
}

type GPUInfo struct {
	Name     string `json:"name"`
	Util     int    `json:"util"`
	MemUsed  uint64 `json:"mem_used"`  // bytes
	MemTotal uint64 `json:"mem_total"` // bytes
	Temp     int    `json:"temp"`
	Power    int    `json:"power"`  // watts
	ClkSM    int    `json:"clk_sm"` // MHz
	Fan      int    `json:"fan"`    // percent, -1 if N/A
}

type FSInfo struct {
	Dev   string  `json:"dev"` // backing device, e.g. /dev/nvme0n1p2
	Mount string  `json:"mount"`
	Used  uint64  `json:"used"`
	Total uint64  `json:"total"`
	Pct   float64 `json:"pct"`
}

type ProcInfo struct {
	PID  int     `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"` // percent of one core (can exceed 100)
	RSS  uint64  `json:"rss"` // bytes
}
