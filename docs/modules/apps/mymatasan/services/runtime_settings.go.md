# Module: apps/mymatasan/services/runtime_settings.go

## Purpose

Manages SQLite-backed runtime settings for `mymatasan`.

## Responsibilities

- Seed missing settings from app config defaults.
- Read and save decoder and live stream settings as one JSON payload.
- Reset runtime settings to startup defaults.
- Normalize legacy decoder payloads so older rows with only `decoder.mjpeg.ffmpegPath` receive safe ffmpeg tuning defaults.
- Validate decoder transport, hardware acceleration mode, optional decoder name, and ICE server URL entries before saving.
- Provide focused reads for decoder and stream callers.
- Convert runtime decoder settings into `infra/rtsp.MJPEGOptions` for live MJPEG and vision frame capture.
- Build decoder auto-tune recommendations from saved camera RTSP metadata and ffmpeg hardware-acceleration capability checks.

## Notes

- Settings changes apply to new WebRTC/MJPEG sessions without app restart.
- Existing live sessions are not forcefully interrupted when settings change.
- Decoder auto-tune logic lives in `runtime_auto_tune.go`; GPU device detection lives in `runtime_gpu_devices.go`. Both are consumed by the settings API handler.
