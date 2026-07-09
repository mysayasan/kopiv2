# Module: apps/mymatasan/services/runtime_gpu_devices.go

## Purpose

Detects available hardware GPU/device options for ffmpeg hardware acceleration, scoped to the current platform.

## Responsibilities

- `DetectDecoderGPUDevices` (the exported entry point) serves results from an in-process **stale-while-revalidate cache** (`gpuDeviceCacheTTL` = 5 minutes) instead of re-probing hardware on every call: the actual probe logic lives in the unexported `detectDecoderGPUDevices`. Only the first-ever call blocks on a synchronous probe; once warm, an expired entry is still returned immediately while a single background goroutine refreshes it, so callers such as `GET /api/capacity` (polled by the dashboard) and the Settings GPU device list never block on the multi-second probe again. Hardware is effectively static while the process runs, so brief staleness after a driver/hardware change is acceptable.
- Return a `DecoderGPUDeviceResult` containing a list of `DecoderGPUDeviceOption` entries and human-readable observations.
- **Windows**: query `Win32_VideoController` via PowerShell, sorting adapters so the primary display adapter (non-zero `CurrentHorizontalResolution`) comes first. This matches the DXGI adapter enumeration order used by both Task Manager and ffmpeg `d3d11va`. Also runs `nvidia-smi -L` to expose CUDA device indices separately.
- **Linux**: scan `/dev/dri/renderD*` nodes for VAAPI render devices; run `nvidia-smi -L` for CUDA GPU indices; fall back to `/dev/nvidia[0-9]*` device nodes when nvidia-smi is unavailable.
- **macOS**: query `system_profiler SPDisplaysDataType` for chipset names; all VideoToolbox options carry an empty device value because VideoToolbox selects the platform default device.
- Append a diagnostic observation when no selectable device is detected.

## Key Types

- `DecoderGPUDeviceOption` — `Value` (device identifier passed to ffmpeg `-hwaccel_device`), `Label` (human-readable name), `HWAccel` (acceleration method), `Kind` (platform-specific category).
- `DecoderGPUDeviceResult` — `GOOS`, `Devices []DecoderGPUDeviceOption`, `Observations []string`.

## Notes

- Windows GPU indices in the returned list match DXGI adapter order and Task Manager GPU numbering. Selecting index 0 in ffmpeg `-hwaccel_device` corresponds to Task Manager GPU 0.
- Windows exposes both `d3d11va` (DXGI-indexed) and `cuda` (nvidia-smi-indexed) options for Nvidia GPUs. CUDA is preferred on Optimus/hybrid systems because it targets the Nvidia driver directly and bypasses DXGI adapter routing.
- Linux VAAPI device paths (e.g. `/dev/dri/renderD128`) are passed directly as `-hwaccel_device`. In Docker containers, render nodes are only visible when the host device is mounted with `--device /dev/dri/renderD128`.
- Each platform detection call uses a two-second timeout via `runTool`.
- Caching cut `GET /api/capacity` latency roughly 12x (~3.1s to ~0.26s on a cold-cache-miss-free run) since it no longer respawns `powershell`/`nvidia-smi` per request.
