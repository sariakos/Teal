package docker

import (
	"encoding/json"
	"fmt"
	"io"
)

// dockerStatsJSON is the subset of the daemon's stats payload we read.
// Defined locally (not imported from the SDK) so this package is the
// only one that needs the raw shape — the public API returns
// ContainerStats which is just scalars.
type dockerStatsJSON struct {
	CPUStats    cpuStats    `json:"cpu_stats"`
	PreCPUStats cpuStats    `json:"precpu_stats"`
	MemoryStats memoryStats `json:"memory_stats"`
	Networks    map[string]netStats  `json:"networks"`
	BlkioStats  blkioStats  `json:"blkio_stats"`
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage  uint64 `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
}

type memoryStats struct {
	Usage uint64            `json:"usage"`
	Limit uint64            `json:"limit"`
	Stats map[string]uint64 `json:"stats"`
}

type netStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type blkioStats struct {
	IoServiceBytesRecursive []blkEntry `json:"io_service_bytes_recursive"`
}

type blkEntry struct {
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}

// decodeStatsJSON reads one stats object and converts it into the scalar
// ContainerStats our API exposes. CPU percent matches what the CLI prints
// (delta_container_cpu / delta_system_cpu * online_cpus * 100).
func decodeStatsJSON(r io.Reader) (ContainerStats, error) {
	var s dockerStatsJSON
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return ContainerStats{}, fmt.Errorf("decode stats: %w", err)
	}

	// Memory: daemon reports RSS + cache + buffers; subtract cache to
	// match what the CLI shows as "memory used".
	mem := int64(s.MemoryStats.Usage)
	if cache, ok := s.MemoryStats.Stats["cache"]; ok {
		mem -= int64(cache)
	}
	if mem < 0 {
		mem = int64(s.MemoryStats.Usage)
	}

	// CPU % — protect against pre-data reads where precpu is zero.
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemCPUUsage - s.PreCPUStats.SystemCPUUsage)
	cpuPct := 0.0
	if cpuDelta > 0 && sysDelta > 0 {
		cores := float64(s.CPUStats.OnlineCPUs)
		if cores == 0 {
			cores = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
			if cores == 0 {
				cores = 1
			}
		}
		cpuPct = (cpuDelta / sysDelta) * cores * 100.0
	}

	// Network: sum across interfaces.
	var netRx, netTx int64
	for _, n := range s.Networks {
		netRx += int64(n.RxBytes)
		netTx += int64(n.TxBytes)
	}

	// Block IO: sum by operation.
	var blkRead, blkWrite int64
	for _, e := range s.BlkioStats.IoServiceBytesRecursive {
		switch e.Op {
		case "read", "Read":
			blkRead += int64(e.Value)
		case "write", "Write":
			blkWrite += int64(e.Value)
		}
	}

	return ContainerStats{
		CPUPercent: cpuPct,
		MemBytes:   mem,
		MemLimit:   int64(s.MemoryStats.Limit),
		NetRx:      netRx,
		NetTx:      netTx,
		BlkRead:    blkRead,
		BlkWrite:   blkWrite,
	}, nil
}
