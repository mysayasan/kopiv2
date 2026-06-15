package services

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// MachineDiskMetric is the usage of one monitored disk volume.
type MachineDiskMetric struct {
	Path        string  `json:"path"`
	Mountpoint  string  `json:"mountpoint"`
	UsedPercent float64 `json:"usedPercent"`
	UsedBytes   uint64  `json:"usedBytes"`
	TotalBytes  uint64  `json:"totalBytes"`
	FreeBytes   uint64  `json:"freeBytes"`
}

// MachineMetrics is a one-shot snapshot of host resource usage.
type MachineMetrics struct {
	SampledAt        int64               `json:"sampledAt"`
	CPUPercent       float64             `json:"cpuPercent"`
	MemoryPercent    float64             `json:"memoryPercent"`
	MemoryUsedBytes  uint64              `json:"memoryUsedBytes"`
	MemoryTotalBytes uint64              `json:"memoryTotalBytes"`
	Disks            []MachineDiskMetric `json:"disks"`
	RecordingPaused  bool                `json:"recordingPaused"`
}

// sampleHostMetrics reads current CPU, memory, and per-volume disk usage. The CPU
// read blocks briefly to compute a real utilisation figure. diskPaths are
// resolved to their distinct underlying volumes so the same disk is not reported
// twice when several monitored paths share it.
func sampleHostMetrics(ctx context.Context, diskPaths []string) MachineMetrics {
	out := MachineMetrics{SampledAt: time.Now().UTC().Unix()}

	// CPU: a short blocking sample yields a real percentage even on the first call.
	if pcts, err := cpu.PercentWithContext(ctx, 250*time.Millisecond, false); err == nil && len(pcts) > 0 {
		out.CPUPercent = round1(pcts[0])
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil && vm != nil {
		out.MemoryPercent = round1(vm.UsedPercent)
		out.MemoryUsedBytes = vm.Used
		out.MemoryTotalBytes = vm.Total
	}

	out.Disks = sampleDiskUsage(ctx, diskPaths)
	return out
}

// sampleDiskUsage resolves the given paths to distinct volumes and returns usage
// for each. Paths that fail to resolve are skipped.
func sampleDiskUsage(ctx context.Context, paths []string) []MachineDiskMetric {
	parts, _ := disk.PartitionsWithContext(ctx, false)
	seen := map[string]bool{}
	out := make([]MachineDiskMetric, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		mount := mountpointFor(p, parts)
		if seen[mount] {
			continue
		}
		usage, err := disk.UsageWithContext(ctx, mount)
		if err != nil || usage == nil || usage.Total == 0 {
			continue
		}
		seen[mount] = true
		out = append(out, MachineDiskMetric{
			Path:        p,
			Mountpoint:  mount,
			UsedPercent: round1(usage.UsedPercent),
			UsedBytes:   usage.Used,
			TotalBytes:  usage.Total,
			FreeBytes:   usage.Free,
		})
	}
	return out
}

// mountpointFor resolves a path to the mount point of the volume that contains
// it, so multiple paths on the same disk collapse to one entry. Falls back to the
// path's volume name (Windows) or the path itself when no partition matches.
func mountpointFor(path string, parts []disk.PartitionStat) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	best := ""
	for _, p := range parts {
		mp := filepath.Clean(p.Mountpoint)
		if pathHasPrefix(abs, mp) && len(mp) > len(best) {
			best = mp
		}
	}
	if best != "" {
		return best
	}
	if vol := filepath.VolumeName(abs); vol != "" {
		return vol + string(filepath.Separator)
	}
	return abs
}

// pathHasPrefix reports whether prefix is an ancestor of (or equal to) path,
// honoring path-separator boundaries and case-insensitivity on Windows.
func pathHasPrefix(path, prefix string) bool {
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		prefix = strings.ToLower(prefix)
	}
	if path == prefix {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	if !strings.HasSuffix(path, sep) {
		path += sep
	}
	return strings.HasPrefix(path, prefix)
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
