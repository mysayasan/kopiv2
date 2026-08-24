# Module: infra/recording/types.go

## Purpose

Defines the shared contracts and configuration types for the reusable recording module.

## Types

- `RecorderConfig` — per-camera recording configuration carrying:
  - `CameraId`, `Enabled`, `StoragePath`, `FFmpegPath`
  - `RTSPURI` — primary RTSP stream used for recording
  - `FallbackRTSPURI` — alternative RTSP stream tried after 2 consecutive quick failures of the primary; empty disables fallback switching
  - `RTSPTransport`, `PreRollSec`, `PostRollSec`, `SegmentMinutes`, `RetentionDays`
  - `SiphonFPS`, `SiphonWidth` — optional decoded-frame tee for the AI detector
  - `ShredPasses` — secure-overwrite pass count applied to segment files on retention purge (`>0` shreds; `0` = plain delete)
  - `RecordCodec` — at-rest video codec for finalized segments: `""`/`copy` (store the camera's native codec, no re-encode — default), `h264`, or `hevc` (re-encode once at remux time on the GPU). Live capture and event clips always stay stream-copy.
  - `RecordQuality` — NVENC constant-quality (CQ) target used when re-encoding (`0` = default 26)
  - `RecordFallbackCopy` — when `true` (the default), a segment is stored as plain stream-copy if the configured GPU re-encode can't run: either the host has no usable NVENC encoder (checked once up front via `NVENCUsable`) or a re-encode fails at runtime. Guarantees the segment is saved rather than dropped. Only matters when `RecordCodec` re-encodes; copy mode never encodes.
  - `Metrics` — optional `telemetry.Metrics` recorder for ffmpeg restarts and segment finalize outcomes; nil is fine, callers use the nil-safe `countMetric` method.
- `FrameEntry` — one captured JPEG frame with its Unix-second capture timestamp; the atomic unit held in the ring buffer.
- `SegmentResult` — produced by a recorder after a clip is written to disk; carries camera ID, alert ID, file path, start/end timestamps, file size, `Codec` (the on-disk video codec, e.g. `h264`/`hevc`, so playback knows whether it must transcode for the browser without re-probing), and `Sha256` (hex SHA-256 of the segment's PLAINTEXT mp4, taken at finalize before at-rest encryption via `infra/recording.HashPlaintextFile` — see `hash.go.md`). Empty means unhashed, not unchanged: a segment adopted after a crash is already encrypted by the time it is seen, and rows written before hashing existed have none either. An evidence export must report that difference rather than paper over it — a digest computed later proves only that the file has not changed since, a materially weaker claim than "not altered since it was recorded".
- `SegmentSink` — interface implemented by apps to persist segment metadata; decouples the infra recorder from any app-specific storage layer.

## Metrics

- `MetricFFmpegRestartsTotal` = `kopiv2_recording_ffmpeg_restarts_total` ({camera}) — counts capture-ffmpeg restarts per camera. A camera thrashing here is failing to hold its RTSP connection; it's the earliest signal of a flapping stream, well before footage goes visibly missing.
- `MetricSegmentFinalizeTotal` = `kopiv2_recording_segment_finalize_total` ({camera, outcome}) — counts every segment finalize attempt by outcome (`saved`, `discarded`, `failed`, `unsaved`, `quarantined`). Anything but `saved` accumulating means footage is not reaching the recordings list; a non-zero `quarantined` count in particular is footage on disk that will never appear in the recordings list — the one recorder metric worth paging on.
- `DescribeMetrics(m telemetry.Metrics)` registers help text for both; called once at startup (`app.go`).
- Both carry the neutral `kopiv2_` prefix because they're emitted by shared infra — any app that records video reports the same numbers.

## Notes

- `ModeRTSP` and `ModeTick` are the only valid mode constants; any other value is treated as `tick`.
- `SegmentSink.SaveSegment` is called from a background goroutine; implementations must be safe for concurrent calls.
- The package deliberately does not import any app-specific or database packages; apps implement `SegmentSink` and pass the concrete implementation into the manager.
- `FallbackRTSPURI` is intended for cameras that expose a sub-stream on a different RTSP path than the main stream; the manager automatically toggles between primary and fallback after repeated connection failures.
- `RecorderConfig.countMetric(name, labels)` is nil-safe (a no-op when `Metrics` is nil) and always adds a `camera` label from `CameraId`, so call sites never need a guard or need to remember the camera label themselves.

## `RecorderConfig.RetentionHold` (W3-3)

An optional predicate `(cameraId, startedAt, endedAt) -> bool`, asked before the recorder's own
retention sweep deletes an expired segment FILE, and keeping it when the answer is true.

It exists because that sweep (`rtsp.go` `purgeOldFiles`) deletes by filename age with no view
of the database, so mymatasan's case files — which hold the footage an open investigation
points at past its retention date — would have their hold undone within the hour, leaving
segment rows pointing at files that no longer exist. The predicate belongs to the app, so
`infra/recording` never learns what a case is; it only knows some footage is spoken for. nil
means nothing is held.

Because a segment's end is not knowable from its filename, the sweep asks about
`[start, start + one segment length)` — over-estimating the span, which errs towards keeping a
file slightly too long rather than shredding evidence.
