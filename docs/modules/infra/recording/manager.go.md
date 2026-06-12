# Module: infra/recording/manager.go

## Purpose

Provides the app-level entry point for the recording module, holding all per-camera recorders and dispatching frame and event calls.

## Responsibilities

- Create and manage a map of per-camera recorders keyed by camera ID.
- Add or replace a camera recorder via `Configure`; start RTSP recorders immediately and remove the existing recorder cleanly before replacing it.
- Remove a camera recorder when `cfg.Enabled` is false.
- Dispatch `WriteFrame` calls to the matching camera recorder under a read lock.
- Dispatch `TriggerEvent(cameraId, alertId, frameCapturedAt)` calls to the matching camera recorder under a read lock. `frameCapturedAt` is the Unix second timestamp of the frame that produced the alert; pass `0` to fall back to the current wall clock (used by the manual alert API).
- Return a `[]CameraStatus` snapshot via `Statuses()` for all configured cameras; used by the `/api/recording/status` endpoint to power the live recorder status panel.
- Cancel the shared context and close all recorders cleanly in `Close`.

## CameraStatus fields

| Field                | Type   | Notes |
|----------------------|--------|-------|
| `cameraId`           | int64  | Camera identifier. |
| `mode`               | string | `rtsp` or `tick`. |
| `state`              | string | `streaming`, `stopped`, or `error`. |
| `ffmpegRunning`      | bool   | Whether the ffmpeg subprocess is currently alive (RTSP mode only). |
| `liveFiles`          | int    | Number of `.ts` segment files currently in the live directory. |
| `liveDir`            | string | Absolute path to the live segment directory. |
| `lastError`          | string | Most recent meaningful ffmpeg error line; omitted when empty. Noisy harmless warnings are filtered. |
| `activeStreamUrl`    | string | RTSP URI currently in use (primary or fallback). |
| `usingFallback`      | bool   | True when the fallback RTSP URI is active. |
| `ringBufferFrames`   | int    | Frames currently held in the tick-mode ring buffer. |
| `ringBufferCapacity` | int    | Configured capacity of the tick-mode ring buffer. |

## Notes

- `NewManager` accepts a `SegmentSink` that is forwarded to every recorder created through `Configure`; apps pass their recording service as the sink.
- `WriteFrame` and `TriggerEvent` are no-ops when no recorder is configured for the given camera ID.
- `Configure` is safe to call at runtime to enable, disable, or change recording settings for one camera without affecting others; changes take effect immediately.
- `Close` should be called during graceful app shutdown before the database connection is released, because RTSP recorders may still be writing segment files.
