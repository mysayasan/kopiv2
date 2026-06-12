# Module: infra/recording/types.go

## Purpose

Defines the shared contracts and configuration types for the reusable recording module.

## Types

- `RecorderConfig` — per-camera recording configuration carrying:
  - `CameraId`, `Enabled`, `StoragePath`, `FFmpegPath`
  - `RTSPURI` — primary RTSP stream used for recording
  - `FallbackRTSPURI` — alternative RTSP stream tried after 2 consecutive quick failures of the primary; empty disables fallback switching
  - `RTSPTransport`, `PreRollSec`, `PostRollSec`, `SegmentMinutes`, `RetentionDays`
- `FrameEntry` — one captured JPEG frame with its Unix-second capture timestamp; the atomic unit held in the ring buffer.
- `SegmentResult` — produced by a recorder after a clip is written to disk; carries camera ID, alert ID, file path, start/end timestamps, and file size.
- `SegmentSink` — interface implemented by apps to persist segment metadata; decouples the infra recorder from any app-specific storage layer.

## Notes

- `ModeRTSP` and `ModeTick` are the only valid mode constants; any other value is treated as `tick`.
- `SegmentSink.SaveSegment` is called from a background goroutine; implementations must be safe for concurrent calls.
- The package deliberately does not import any app-specific or database packages; apps implement `SegmentSink` and pass the concrete implementation into the manager.
- `FallbackRTSPURI` is intended for cameras that expose a sub-stream on a different RTSP path than the main stream; the manager automatically toggles between primary and fallback after repeated connection failures.
