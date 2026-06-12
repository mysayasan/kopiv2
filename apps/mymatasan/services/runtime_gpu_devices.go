package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type DecoderGPUDeviceOption struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	HWAccel string `json:"hwaccel"`
	Kind    string `json:"kind"`
}

type DecoderGPUDeviceResult struct {
	GOOS         string                   `json:"goos"`
	Devices      []DecoderGPUDeviceOption `json:"devices"`
	Observations []string                 `json:"observations"`
}

func DetectDecoderGPUDevices(ctx context.Context) DecoderGPUDeviceResult {
	result := DecoderGPUDeviceResult{GOOS: runtime.GOOS}
	switch runtime.GOOS {
	case "windows":
		result.Devices, result.Observations = detectWindowsGPUDevices(ctx)
	case "linux":
		result.Devices, result.Observations = detectLinuxGPUDevices(ctx)
	case "darwin":
		result.Devices, result.Observations = detectDarwinGPUDevices(ctx)
	default:
		result.Observations = append(result.Observations, "GPU device discovery is not implemented for this operating system.")
	}
	if len(result.Devices) == 0 {
		result.Observations = append(result.Observations, "No selectable GPU device was detected. Leave GPU/device empty to let ffmpeg choose the default device.")
	}
	return result
}

func detectWindowsGPUDevices(ctx context.Context) ([]DecoderGPUDeviceOption, []string) {
	// Sort so the primary display adapter (non-zero resolution = driving a screen) comes first.
	// This matches the DXGI adapter enumeration order that FFmpeg d3d11va and Task Manager both use.
	psCmd := `Get-CimInstance Win32_VideoController | Sort-Object @{Expression={if($_.CurrentHorizontalResolution -gt 0){0}else{1}}},Name | ForEach-Object { $_.Name }`
	output, err := runTool(ctx, "powershell.exe", "-NoProfile", "-Command", psCmd)

	devices := []DecoderGPUDeviceOption{}
	observations := []string{}

	if err == nil {
		for index, name := range nonEmptyLines(output) {
			devices = append(devices, DecoderGPUDeviceOption{
				Value:   fmt.Sprintf("%d", index),
				Label:   fmt.Sprintf("GPU %d - %s (d3d11va)", index, name),
				HWAccel: "d3d11va",
				Kind:    "windows-display-adapter",
			})
		}
		if len(devices) > 0 {
			observations = append(observations, "GPU indices match Task Manager and ffmpeg d3d11va adapter order (primary display adapter = 0).")
		}
	} else {
		observations = append(observations, "Windows GPU query failed: "+err.Error())
	}

	// nvidia-smi works on Windows too; CUDA indices are independent of DXGI order and
	// directly target the Nvidia driver, making them reliable for GPU-specific decoding.
	if nvOutput, nvErr := runTool(ctx, "nvidia-smi", "-L"); nvErr == nil {
		for _, line := range nonEmptyLines(nvOutput) {
			index, label := parseNvidiaSMILine(line)
			if index == "" {
				continue
			}
			devices = append(devices, DecoderGPUDeviceOption{
				Value:   index,
				Label:   label + " (CUDA)",
				HWAccel: "cuda",
				Kind:    "windows-nvidia-cuda",
			})
		}
	}

	return devices, observations
}

func detectLinuxGPUDevices(ctx context.Context) ([]DecoderGPUDeviceOption, []string) {
	devices := []DecoderGPUDeviceOption{}
	observations := []string{}

	if matches, err := filepath.Glob("/dev/dri/renderD*"); err == nil {
		sort.Strings(matches)
		for _, path := range matches {
			if _, statErr := os.Stat(path); statErr != nil {
				continue
			}
			devices = append(devices, DecoderGPUDeviceOption{
				Value:   path,
				Label:   "VAAPI render device - " + path,
				HWAccel: "vaapi",
				Kind:    "linux-vaapi-render-node",
			})
		}
		if len(matches) > 0 {
			observations = append(observations, "Linux VAAPI render nodes are suitable for ffmpeg hwaccel_device.")
		}
	}

	if output, err := runTool(ctx, "nvidia-smi", "-L"); err == nil {
		for _, line := range nonEmptyLines(output) {
			index, label := parseNvidiaSMILine(line)
			if index == "" {
				continue
			}
			devices = append(devices, DecoderGPUDeviceOption{
				Value:   index,
				Label:   label,
				HWAccel: "cuda",
				Kind:    "nvidia-cuda-index",
			})
		}
	} else {
		observations = append(observations, "nvidia-smi was not available; CUDA GPU indices could not be queried.")
	}

	if matches, err := filepath.Glob("/dev/nvidia[0-9]*"); err == nil {
		sort.Strings(matches)
		for _, path := range matches {
			base := filepath.Base(path)
			index := strings.TrimPrefix(base, "nvidia")
			if index == "" || strings.Contains(index, "-") {
				continue
			}
			if containsDeviceValue(devices, index, "cuda") {
				continue
			}
			devices = append(devices, DecoderGPUDeviceOption{
				Value:   index,
				Label:   "NVIDIA CUDA GPU " + index + " - " + path,
				HWAccel: "cuda",
				Kind:    "nvidia-device-node",
			})
		}
	}

	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].HWAccel != devices[j].HWAccel {
			return devices[i].HWAccel < devices[j].HWAccel
		}
		return devices[i].Value < devices[j].Value
	})
	return devices, observations
}

func detectDarwinGPUDevices(ctx context.Context) ([]DecoderGPUDeviceOption, []string) {
	output, err := runTool(ctx, "system_profiler", "SPDisplaysDataType")
	if err != nil {
		return nil, []string{"macOS display query failed: " + err.Error()}
	}
	names := []string{}
	for _, line := range nonEmptyLines(output) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Chipset Model:") {
			continue
		}
		names = append(names, strings.TrimSpace(strings.TrimPrefix(line, "Chipset Model:")))
	}
	devices := make([]DecoderGPUDeviceOption, 0, len(names))
	for index, name := range names {
		devices = append(devices, DecoderGPUDeviceOption{
			Value:   "",
			Label:   fmt.Sprintf("VideoToolbox default GPU %d - %s", index, name),
			HWAccel: "videotoolbox",
			Kind:    "macos-videotoolbox",
		})
	}
	if len(devices) > 0 {
		return devices, []string{"VideoToolbox normally uses the platform default device; leave GPU/device empty unless you have a custom ffmpeg setup."}
	}
	return devices, nil
}

func runTool(ctx context.Context, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, name, args...).CombinedOutput()
	text := strings.TrimSpace(string(output))
	if runCtx.Err() != nil {
		return text, runCtx.Err()
	}
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%w: %s", err, text)
		}
		return text, err
	}
	return text, nil
}

func nonEmptyLines(output string) []string {
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

var nvidiaSMIRe = regexp.MustCompile(`(?i)^GPU\s+(\d+):\s+(.+)$`)

func parseNvidiaSMILine(line string) (string, string) {
	matches := nvidiaSMIRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) < 3 {
		return "", ""
	}
	return matches[1], "CUDA GPU " + matches[1] + " - " + strings.TrimSpace(matches[2])
}

func containsDeviceValue(devices []DecoderGPUDeviceOption, value string, hwaccel string) bool {
	for _, device := range devices {
		if device.Value == value && strings.EqualFold(device.HWAccel, hwaccel) {
			return true
		}
	}
	return false
}
