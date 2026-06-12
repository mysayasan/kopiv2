# Module: infra/recording/tick.go

## Purpose

Implements the tick-mode per-camera recorder that buffers JPEG frames fed from an external loop and flushes a pre+post-roll MP4 clip on each alert event.

## Responsibilities

- Accept incoming `FrameEntry` values via `WriteFrame` and store them in the ring buffer.
- On `TriggerEvent`: snapshot the current ring buffer contents as pre-roll, record the alert ID and wall-clock trigger time, and switch to post-roll collection state.
- Continue collecting post-roll frames until `frame.CapturedAt >= postEnd`.
- Flush the concatenated pre-roll and post-roll frames to an MP4 file in a background goroutine via `WriteMP4`.
- Notify the `SegmentSink` with the resulting `SegmentResult` after the file is written.
- Return to idle state after each flush so subsequent alerts can trigger new clips.

## Notes

- Concurrent calls to `WriteFrame` and `TriggerEvent` are serialized by a mutex; the flush runs in a separate goroutine.
- A second `TriggerEvent` while post-roll is active is a no-op; the ongoing clip continues to its natural end.
- Clip files are named `alert_{alertId}_{startedAt}.mp4` and placed under `{storagePath}/cam{cameraId}/`.
- The storage directory is created on first flush; the recorder does not pre-create directories at startup.
