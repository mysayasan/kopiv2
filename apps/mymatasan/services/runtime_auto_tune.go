package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/externaltools"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

type DecoderAutoTuneEnvironment struct {
	GOOS        string                 `json:"goos"`
	FFmpegFound bool                   `json:"ffmpegFound"`
	FFmpegPath  string                 `json:"ffmpegPath"`
	FFmpegSource string                `json:"ffmpegSource"`
	HWAccels    []string               `json:"hwaccels"`
	FFmpegError string                 `json:"ffmpegError"`
	VAAPIDevice string                 `json:"vaapiDevice"`
	GPUDevices  DecoderGPUDeviceResult `json:"gpuDevices"`
	InContainer bool                   `json:"inContainer"`
}

type RuntimeAutoTuneResult struct {
	Applied      bool            `json:"applied"`
	Summary      string          `json:"summary"`
	Observations []string        `json:"observations"`
	Settings     RuntimeSettings `json:"settings"`
}

// DetectDecoderAutoTuneEnvironment gathers local ffmpeg and OS hints used by runtime decoder auto-tune.
func DetectDecoderAutoTuneEnvironment(ctx context.Context, ffmpegPath string) DecoderAutoTuneEnvironment {
	env := DecoderAutoTuneEnvironment{GOOS: runtime.GOOS}
	status := externaltools.CheckExecutable(ctx, externaltools.ExecutableSpec{
		Name:           "FFmpeg",
		ConfiguredPath: strings.TrimSpace(ffmpegPath),
		ExecutableName: "ffmpeg",
		CandidatePaths: ffmpegCandidatePaths(),
		ProbeArgs:      []string{"-hide_banner", "-hwaccels"},
		Timeout:        2 * time.Second,
	})
	env.FFmpegFound = status.Found
	env.FFmpegPath = status.Path
	env.FFmpegSource = status.Source
	if status.Error != "" {
		env.FFmpegError = status.Error
	}
	env.HWAccels = parseFFmpegHWAccels(status.ProbeOutput)
	env.GPUDevices = DetectDecoderGPUDevices(ctx)
	if runtime.GOOS == "linux" {
		env.InContainer = isRunningInContainer()
		// Populate VAAPIDevice from the detected device list so it reflects whatever
		// render node is actually accessible (important in Docker with device passthrough).
		if dev := firstGPUDevice(env.GPUDevices.Devices, "vaapi"); dev != nil {
			env.VAAPIDevice = dev.Value
		} else if _, statErr := os.Stat("/dev/dri/renderD128"); statErr == nil {
			env.VAAPIDevice = "/dev/dri/renderD128"
		}
	}
	return env
}

// isRunningInContainer returns true when the process is running inside a
// Docker, containerd, Kubernetes, or LXC container.
func isRunningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	content := string(data)
	for _, hint := range []string{"docker", "containerd", "kubepods", "/lxc/"} {
		if strings.Contains(content, hint) {
			return true
		}
	}
	return false
}

func ffmpegCandidatePaths() []string {
	switch runtime.GOOS {
	case "windows":
		candidates := []string{
			`C:\ffmpeg\bin\ffmpeg.exe`,
			filepath.Join(".tools", "ffmpeg", "bin", "ffmpeg.exe"),
		}
		if matches, err := filepath.Glob(filepath.Join(".tools", "ffmpeg", "*", "bin", "ffmpeg.exe")); err == nil {
			candidates = append(candidates, matches...)
		}
		return candidates
	case "linux":
		return []string{"/usr/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/snap/bin/ffmpeg"}
	case "darwin":
		return []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"}
	default:
		return nil
	}
}

func AutoTuneRuntimeSettings(current RuntimeSettings, devices []*CameraDetail, env DecoderAutoTuneEnvironment) RuntimeAutoTuneResult {
	tuned := normalizeRuntimeSettings(current)
	tuned.Decoder.MJPEG.Quality = defaultDecoderQuality
	tuned.Decoder.MJPEG.Threads = defaultDecoderThreads
	tuned.Decoder.FFmpeg.RTSPTransport = defaultDecoderRTSPTransport
	tuned.Decoder.FFmpeg.HWAccel = "none"
	tuned.Decoder.FFmpeg.HWAccelDevice = ""
	tuned.Decoder.FFmpeg.InitHWDevice = ""
	tuned.Decoder.FFmpeg.VideoDecoder = ""
	tuned.Decoder.FFmpeg.ProbeSize = defaultDecoderProbeSize
	tuned.Decoder.FFmpeg.AnalyzeDuration = defaultDecoderAnalyzeDuration
	lowDelay := true
	noBuffer := true
	tuned.Decoder.FFmpeg.LowDelay = &lowDelay
	tuned.Decoder.FFmpeg.NoBuffer = &noBuffer

	observations := []string{}
	if env.InContainer {
		observations = append(observations, "Running inside a container. GPU hardware decode requires device passthrough: add --device /dev/dri/renderD128 (VAAPI/Intel/AMD) or --gpus all (CUDA/Nvidia) to your docker run command.")
	}
	videoCodecs, devicesWithTracks := deviceVideoCodecs(devices)
	observations = append(observations, fmt.Sprintf("Saved cameras inspected: %d", len(devices)))
	if devicesWithTracks == 0 {
		observations = append(observations, "No saved RTSP track metadata found; run RTSP Test on cameras for better tuning.")
	} else {
		observations = append(observations, fmt.Sprintf("Cameras with RTSP track metadata: %d", devicesWithTracks))
		observations = append(observations, "Observed video codecs: "+strings.Join(sortedSetKeys(videoCodecs), ", "))
	}

	if len(devices) > 4 {
		tuned.Decoder.MJPEG.Quality = 8
		observations = append(observations, "Multiple saved cameras detected; MJPEG quality relaxed slightly to reduce CPU and bandwidth.")
	}
	if videoCodecs["h265"] || videoCodecs["hevc"] {
		tuned.Decoder.MJPEG.Quality = 8
		tuned.Decoder.FFmpeg.ProbeSize = 2000000
		tuned.Decoder.FFmpeg.AnalyzeDuration = 2000000
		observations = append(observations, "H265/HEVC stream metadata detected; probe/analyze limits increased for decoder startup stability.")
	}

	if env.FFmpegError != "" {
		observations = append(observations, "FFmpeg capability check failed: "+env.FFmpegError)
		observations = append(observations, "Hardware decode left disabled because ffmpeg capabilities could not be confirmed.")
		return RuntimeAutoTuneResult{
			Summary:      "Auto-tune applied CPU software decode with conservative ffmpeg settings.",
			Observations: observations,
			Settings:     normalizeRuntimeSettings(tuned),
		}
	}

	if len(env.HWAccels) == 0 {
		observations = append(observations, "No ffmpeg hardware acceleration methods were reported.")
	} else {
		observations = append(observations, "FFmpeg hardware acceleration methods: "+strings.Join(env.HWAccels, ", "))
	}
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}

	hwaccel, device, reason := chooseDecoderHWAccel(env)
	if hwaccel != "" {
		tuned.Decoder.FFmpeg.HWAccel = hwaccel
		tuned.Decoder.FFmpeg.HWAccelDevice = device
		observations = append(observations, reason)
		return RuntimeAutoTuneResult{
			Summary:      fmt.Sprintf("Auto-tune applied %s hardware decode with TCP transport and low-latency flags.", hwaccel),
			Observations: observations,
			Settings:     normalizeRuntimeSettings(tuned),
		}
	}

	observations = append(observations, reason)
	return RuntimeAutoTuneResult{
		Summary:      "Auto-tune applied CPU software decode with conservative ffmpeg settings.",
		Observations: observations,
		Settings:     normalizeRuntimeSettings(tuned),
	}
}

func parseFFmpegHWAccels(output string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		item := strings.ToLower(strings.TrimSpace(line))
		if item == "" || strings.Contains(item, "hardware acceleration") {
			continue
		}
		if strings.ContainsAny(item, " \t:") {
			continue
		}
		seen[item] = true
	}
	return sortedSetKeys(seen)
}

func chooseDecoderHWAccel(env DecoderAutoTuneEnvironment) (string, string, string) {
	supported := map[string]bool{}
	for _, item := range env.HWAccels {
		supported[strings.ToLower(strings.TrimSpace(item))] = true
	}

	switch strings.ToLower(env.GOOS) {
	case "windows":
		// CUDA targets the Nvidia driver directly and is reliable on Optimus systems.
		if supported["cuda"] {
			if dev := firstGPUDevice(env.GPUDevices.Devices, "cuda"); dev != nil {
				return "cuda", dev.Value, fmt.Sprintf("Nvidia GPU detected (%s); CUDA hardware decode selected.", dev.Label)
			}
		}
		// d3d11va with the best discrete GPU device found during detection.
		if supported["d3d11va"] {
			if dev := bestDiscreteGPUDevice(env.GPUDevices.Devices, "d3d11va"); dev != nil {
				return "d3d11va", dev.Value, fmt.Sprintf("Discrete GPU detected (%s); d3d11va hardware decode selected.", dev.Label)
			}
			return "d3d11va", "", "Windows host and ffmpeg d3d11va support detected; using default adapter."
		}
		if supported["dxva2"] {
			return "dxva2", "", "Windows host and ffmpeg dxva2 support detected."
		}
	case "darwin":
		if supported["videotoolbox"] {
			return "videotoolbox", "", "macOS host and ffmpeg videotoolbox support detected."
		}
	case "linux":
		// Prefer CUDA when nvidia-smi confirmed hardware is present.
		if supported["cuda"] {
			if dev := firstGPUDevice(env.GPUDevices.Devices, "cuda"); dev != nil {
				return "cuda", dev.Value, fmt.Sprintf("Nvidia GPU detected (%s); CUDA hardware decode selected.", dev.Label)
			}
			// cuda reported by ffmpeg but no GPU confirmed via nvidia-smi — fall through to VAAPI.
		}
		// VAAPI covers Intel and AMD on Linux and is the primary hw decoder for Docker setups.
		if supported["vaapi"] {
			if dev := firstGPUDevice(env.GPUDevices.Devices, "vaapi"); dev != nil {
				return "vaapi", dev.Value, fmt.Sprintf("VAAPI render node detected at %s; VAAPI hardware decode selected.", dev.Value)
			}
			if env.VAAPIDevice != "" {
				return "vaapi", env.VAAPIDevice, "Linux VAAPI render device detected at " + env.VAAPIDevice + "."
			}
		}
	}
	return "", "", "No safely verifiable platform hardware decoder was found; software decode selected."
}

// firstGPUDevice returns the first detected device matching the given hwaccel type.
func firstGPUDevice(devices []DecoderGPUDeviceOption, hwaccel string) *DecoderGPUDeviceOption {
	for i := range devices {
		if strings.EqualFold(devices[i].HWAccel, hwaccel) {
			return &devices[i]
		}
	}
	return nil
}

// bestDiscreteGPUDevice returns the first d3d11va device whose label suggests a discrete GPU
// (Nvidia, AMD, Radeon, etc.). Returns nil when only integrated adapters are present,
// allowing the caller to fall back to the ffmpeg default device.
func bestDiscreteGPUDevice(devices []DecoderGPUDeviceOption, hwaccel string) *DecoderGPUDeviceOption {
	discreteHints := []string{"nvidia", "geforce", "rtx", "gtx", "quadro", "radeon", "amd"}
	for i := range devices {
		if !strings.EqualFold(devices[i].HWAccel, hwaccel) {
			continue
		}
		lower := strings.ToLower(devices[i].Label)
		for _, hint := range discreteHints {
			if strings.Contains(lower, hint) {
				return &devices[i]
			}
		}
	}
	return nil
}

func deviceVideoCodecs(devices []*CameraDetail) (map[string]bool, int) {
	codecs := map[string]bool{}
	devicesWithTracks := 0
	for _, device := range devices {
		if device == nil || strings.TrimSpace(device.RTSPTracks) == "" {
			continue
		}
		var tracks []rtsp.Track
		if err := json.Unmarshal([]byte(device.RTSPTracks), &tracks); err != nil {
			continue
		}
		if len(tracks) > 0 {
			devicesWithTracks++
		}
		for _, track := range tracks {
			if !strings.EqualFold(track.MediaType, "video") {
				continue
			}
			codec := strings.ToLower(strings.TrimSpace(track.Codec))
			switch {
			case strings.Contains(codec, "h265"), strings.Contains(codec, "hevc"):
				codecs["h265"] = true
			case strings.Contains(codec, "h264"), strings.Contains(codec, "avc"):
				codecs["h264"] = true
			case codec != "":
				codecs[codec] = true
			}
		}
	}
	return codecs, devicesWithTracks
}

func sortedSetKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, ok := range values {
		if ok && strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return []string{"none"}
	}
	return keys
}
