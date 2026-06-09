# Module: apps/mymatasan/services/runtime_settings.go

## Purpose

Manages SQLite-backed runtime settings for `mymatasan`.

## Responsibilities

- Seed missing settings from app config defaults.
- Read and save decoder and live stream settings as one JSON payload.
- Reset runtime settings to startup defaults.
- Validate ICE server URL entries before saving.
- Provide focused reads for decoder and stream callers.

## Notes

- Settings changes apply to new WebRTC/MJPEG sessions without app restart.
- Existing live sessions are not forcefully interrupted when settings change.
