package metrics

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type HostInfo struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platformVersion"`
	KernelVersion   string `json:"kernelVersion"`
	Arch            string `json:"arch"`
	CPUCores        int    `json:"cpuCores"`
	UptimeSeconds   uint64 `json:"uptimeSeconds"`
}

type SystemMetrics struct {
	Timestamp      int64    `json:"timestamp"`
	CPUPercent     float64  `json:"cpuPercent"`
	MemoryTotalMB  uint64   `json:"memoryTotalMb"`
	MemoryUsedMB   uint64   `json:"memoryUsedMb"`
	MemoryFreeMB   uint64   `json:"memoryFreeMb"`
	MemoryPercent  float64  `json:"memoryPercent"`
	DiskTotalGB    uint64   `json:"diskTotalGb"`
	DiskUsedGB     uint64   `json:"diskUsedGb"`
	DiskPercent    float64  `json:"diskPercent"`
	NetBytesSent   uint64   `json:"netBytesSent"`
	NetBytesRecv   uint64   `json:"netBytesRecv"`
	DockerRunning  bool     `json:"dockerRunning"`
	ContainerCount int      `json:"containerCount"`
	Host           HostInfo `json:"host"`
}

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) GetHostInfo() HostInfo {
	hostname, _ := os.Hostname()
	info := HostInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCores: runtime.NumCPU(),
	}

	if hInfo, err := host.Info(); err == nil {
		info.Hostname = hInfo.Hostname
		info.Platform = hInfo.Platform
		info.PlatformVersion = hInfo.PlatformVersion
		info.KernelVersion = hInfo.KernelVersion
		info.UptimeSeconds = hInfo.Uptime
	}

	return info
}

func (c *Collector) Collect(ctx context.Context) (*SystemMetrics, error) {
	metrics := &SystemMetrics{
		Timestamp: time.Now().Unix(),
		Host:      c.GetHostInfo(),
	}

	// CPU Percentage
	if cpuPercents, err := cpu.PercentWithContext(ctx, 500*time.Millisecond, false); err == nil && len(cpuPercents) > 0 {
		metrics.CPUPercent = cpuPercents[0]
	}

	// Memory
	if vMem, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		metrics.MemoryTotalMB = vMem.Total / (1024 * 1024)
		metrics.MemoryUsedMB = vMem.Used / (1024 * 1024)
		metrics.MemoryFreeMB = vMem.Available / (1024 * 1024)
		metrics.MemoryPercent = vMem.UsedPercent
	}

	// Disk (root partition)
	path := "/"
	if runtime.GOOS == "windows" {
		path = "C:"
	}
	if dUsage, err := disk.UsageWithContext(ctx, path); err == nil {
		metrics.DiskTotalGB = dUsage.Total / (1024 * 1024 * 1024)
		metrics.DiskUsedGB = dUsage.Used / (1024 * 1024 * 1024)
		metrics.DiskPercent = dUsage.UsedPercent
	}

	// Network
	if netCounters, err := net.IOCountersWithContext(ctx, false); err == nil && len(netCounters) > 0 {
		metrics.NetBytesSent = netCounters[0].BytesSent
		metrics.NetBytesRecv = netCounters[0].BytesRecv
	}

	return metrics, nil
}
