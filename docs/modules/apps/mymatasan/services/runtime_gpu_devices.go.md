# Module: apps/mymatasan/services/runtime_gpu_devices.go

## Purpose

Detects available hardware GPU/device options for ffmpeg hardware acceleration, scoped to the current platform.

## Responsibilities

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
