// Package collect leest systeemmetrics uit /proc, /sys en hwmon.
package collect

type CPUSample struct {
	Total        float64    `json:"total"`
	User         float64    `json:"user"`
	System       float64    `json:"system"`
	IOWait       float64    `json:"iowait"`
	Steal        float64    `json:"steal"`
	Cores        []float64  `json:"cores"`
	FreqMHz      []int      `json:"freq_mhz,omitempty"`
	Load         [3]float64 `json:"load"`
	ProcsRunning int        `json:"procs_running"`
	ProcsTotal   int        `json:"procs_total"`
}

type MemSample struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Cached    uint64  `json:"cached"`
	Buffers   uint64  `json:"buffers"`
	SwapTotal uint64  `json:"swap_total"`
	SwapUsed  uint64  `json:"swap_used"`
	Percent   float64 `json:"percent"`
}

type StorageSample struct {
	Mount    string  `json:"mount"`
	Device   string  `json:"device"`
	FSType   string  `json:"fstype"`
	Total    uint64  `json:"total"`
	Used     uint64  `json:"used"`
	Percent  float64 `json:"percent"`
	Remote   bool    `json:"remote"`
	ReadBps  uint64  `json:"read_bps"`
	WriteBps uint64  `json:"write_bps"`
}

type NetIface struct {
	Name      string `json:"name"`
	Up        bool   `json:"up"`
	SpeedMbps int    `json:"speed_mbps,omitempty"`
	Virtual   bool   `json:"virtual"`
	RxBps     uint64 `json:"rx_bps"`
	TxBps     uint64 `json:"tx_bps"`
	RxTotal   uint64 `json:"rx_total"`
	TxTotal   uint64 `json:"tx_total"`
}

type NetSample struct {
	RxBps      uint64     `json:"rx_bps"`
	TxBps      uint64     `json:"tx_bps"`
	RxTotal    uint64     `json:"rx_total"`
	TxTotal    uint64     `json:"tx_total"`
	Interfaces []NetIface `json:"interfaces"`
}

type Temp struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Chip     string  `json:"chip"`
	Celsius  float64 `json:"celsius"`
	High     float64 `json:"high,omitempty"`
	Critical float64 `json:"critical,omitempty"`
	Status   string  `json:"status"`
	Primary  bool    `json:"primary,omitempty"`
}

// GPUEngine is een deelmotor van een geïntegreerde GPU (render, video,
// blitter). Intel rapporteert belasting per motor in plaats van één getal.
type GPUEngine struct {
	Name string  `json:"name"`
	Busy float64 `json:"busy"`
}

type GPU struct {
	Index       int         `json:"index"`
	Vendor      string      `json:"vendor"`
	Name        string      `json:"name"`
	Driver      string      `json:"driver,omitempty"`
	UtilPercent float64     `json:"util_percent"`
	MemUsed     uint64      `json:"mem_used"`
	MemTotal    uint64      `json:"mem_total"`
	SharedMem   bool        `json:"shared_memory,omitempty"`
	TempC       float64     `json:"temp_c,omitempty"`
	PowerW      float64     `json:"power_w,omitempty"`
	FanPercent  float64     `json:"fan_percent,omitempty"`
	ClockMHz    int         `json:"clock_mhz,omitempty"`
	ClockMaxMHz int         `json:"clock_max_mhz,omitempty"`
	Engines     []GPUEngine `json:"engines,omitempty"`
	Note        string      `json:"note,omitempty"`
}

// Sample is één momentopname; dit is exact wat over de SSE-stream gaat.
type Sample struct {
	T       float64         `json:"t"`
	CPU     CPUSample       `json:"cpu"`
	Memory  MemSample       `json:"memory"`
	Storage []StorageSample `json:"storage"`
	Network NetSample       `json:"network"`
	Temps   []Temp          `json:"temps"`
	GPU     []GPU           `json:"gpu"`
	// PowerW is nil zolang er geen RAPL-domein is (geen Intel-CPU, of geen
	// powercap-ondersteuning) — de app verbergt de widget dan, net als bij
	// GPU. Pas gezet vanaf de tweede meting; de eerste geeft nog geen snelheid.
	PowerW *float64 `json:"power_w,omitempty"`
}
