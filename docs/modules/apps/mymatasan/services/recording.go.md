# Module: apps/mymatasan/services/recording.go

## Purpose

Implements `IRecordingService`, persisting per-camera recording configs and clip segment metadata, and satisfying the `recording.SegmentSink` interface so the infra recorder can save clips without a database dependency.

## Responsibilities

- List, fetch by ID, and filter recording segments by camera ID or alert ID.
- Create a segment row from a `recording.SegmentResult` produced by the infra recorder.
- Delete a segment row and remove the corresponding file from disk.
- Fetch, create, and update per-camera `RecordingConfig` rows; upsert by camera ID.
- Persist all config fields including `LiveStreamUrl`, `StreamURL`, and `FallbackStreamUrl` on save.
- Purge segments older than the camera's configured `RetentionDays` by iterating all enabled configs, querying segments by `StartedAt < cutoff`, deleting files, and removing rows.

## Notes

- `SaveSegment` satisfies `recording.SegmentSink`; it is called from a background goroutine in the infra recorder and must be safe for concurrent use (each call creates its own DB statement through the generic repo).
- File removal in `DeleteSegment` and `PurgeOldSegments` uses `os.Remove`; missing-file errors are silently ignored to avoid blocking row cleanup.
- `GetConfig` returns `nil, nil` when no config exists for the requested camera ID rather than an error, allowing callers to detect a first-time save.
- `PurgeOldSegments` is designed to be called on a schedule (e.g., at startup and periodically); it is not called automatically by the service.
- `LiveStreamUrl` is saved and returned in the API response so the Recording UI can restore the selected live stream across page reloads without reverting to the camera's default RTSP URL.
