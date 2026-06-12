# Module: apps/mymatasan/services/runtime_auto_tune.go

## Purpose

Produces hardware-aware decoder settings recommendations by inspecting the local ffmpeg build, GPU hardware, and saved camera RTSP metadata.

## Responsibilities

- Gather the auto-tune environment via `DetectDecoderAutoTuneEnvironment`: ffmpeg availability and supported hardware acceleration methods, GPU device detection across all platforms, and container detection on Linux.
- Apply conservative baseline defaults (TCP transport, low-delay, no-buffer, probe/analyze limits, MJPEG quality/threads) before layering hardware-specific tuning.
- Adjust MJPEG quality and probe/analyze limits upward when many cameras are saved or H.265/HEVC stream metadata is detected.
- Select the best hardware decoder and device via `chooseDecoderHWAccel`, using detected GPU device information rather than hardcoded paths.
- Return a `RuntimeAutoTuneResult` with the applied settings, a summary string, and a list of observations explaining each decision.

## Hardware Selection Priority

### Linux (primary deployment target)
1. **CUDA** — selected when ffmpeg reports `cuda` support and `nvidia-smi` confirms Nvidia hardware. Device value is the nvidia-smi GPU index (e.g. `0`).
2. **VAAPI** — selected when ffmpeg reports `vaapi` support and a VAAPI render node is found in the detected device list (e.g. `/dev/dri/renderD128`). Falls back to the legacy `/dev/dri/renderD128` stat check if GPU detection returns nothing.
3. **Software** — selected when no hardware can be confirmed.

### Windows
1. **CUDA** — selected when ffmpeg reports `cuda` and `nvidia-smi` confirms Nvidia hardware. Device value is the nvidia-smi GPU index.
2. **d3d11va + discrete GPU** — selected when a Nvidia/AMD/Radeon device is identified in the DXGI-ordered adapter list. Device value is the DXGI adapter index matching Task Manager GPU numbering.
3. **d3d11va default** — selected when d3d11va is available but no discrete GPU label is found. ffmpeg uses the default DXGI adapter.
4. **dxva2** — legacy fallback.

### macOS
1. **VideoToolbox** — selected when ffmpeg reports `videotoolbox`. No device index is needed.

## Container Detection

`isRunningInContainer` returns true when any of the following are present:
- `/.dockerenv` file (Docker)
- `/proc/1/cgroup` containing `docker`, `containerd`, `kubepods`, or `/lxc/` (Docker, containerd, Kubernetes, LXC)

When a container is detected, the first auto-tune observation instructs the user to add the appropriate device passthrough flags to their `docker run` command before GPU hardware decode can be selected.

## Key Types

- `DecoderAutoTuneEnvironment` — ffmpeg status, supported hwaccels, detected GPU devices (`GPUDevices DecoderGPUDeviceResult`), first VAAPI render node path (`VAAPIDevice`), and `InContainer` flag.
- `RuntimeAutoTuneResult` — `Applied bool`, `Summary string`, `Observations []string`, `Settings RuntimeSettings`.

## Notes

- `DetectDecoderAutoTuneEnvironment` calls `DetectDecoderGPUDevices` so GPU detection and ffmpeg probing happen in a single auto-tune request.
- `VAAPIDevice` is populated from the first VAAPI device returned by GPU detection, not from a hardcoded path, so it reflects whichever render node is actually accessible (including custom Docker device passthrough paths).
- Auto-tune saves the result immediately via the settings service; the caller marks `Applied: true` after a successful save.
- Run RTSP Test on saved cameras before auto-tuning so stored stream codec metadata is available for H.265/HEVC-aware tuning adjustments.
