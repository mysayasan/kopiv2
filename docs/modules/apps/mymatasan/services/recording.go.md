# Module: apps/mymatasan/services/recording.go

## Purpose

Implements `IRecordingService`, persisting per-camera recording configs and clip segment metadata, and satisfying the `recording.SegmentSink` interface so the infra recorder can save clips without a database dependency.

## Responsibilities

- List, fetch by ID, and filter recording segments by camera ID or alert ID.
- Create a segment row from a `recording.SegmentResult` produced by the infra recorder.
- Delete a segment row and securely remove the corresponding file from disk.
- Fetch, create, and update per-camera `RecordingConfig` rows; upsert by camera ID.
- Persist all config fields including `LiveStreamUrl`, `StreamURL`, `FallbackStreamUrl`, and `MetadataEnabled`/`MetadataGapSeconds` (the object metadata recorder / Object Search toggle + presence-interval close window) on save. `MetadataEnabled` is written from the request's `Enabled` field, not a separate `MetadataEnabled` input — object-metadata capture always tracks whether recording itself is on (without footage there is nothing for a search result to link to), so there is no independent UI toggle for it.
- Purge segments older than the camera's configured `RetentionDays` by iterating all enabled configs, querying segments by `StartedAt < cutoff`, deleting files, and removing rows.
- `PurgeAllForCamera(ctx, cameraId)`: delete EVERY segment for one camera regardless of expiry — reads oldest-first batches of 500 (deleting each batch before the next read so the window advances without offset drift), securely removing each file via `recording.SecureRemove`. Returns the count deleted. Powers the per-camera "Purge now" action (`POST /api/recording/purge-camera`), paired with `visionService.PurgeAlertsForCamera` for the camera's AI-event snapshots.
- `PurgeOldestSegments(ctx, keepAfter, wantBytes)`: delete the globally oldest segments (across all cameras, ignoring per-camera `RetentionDays`) — sorted `StartedAt` ascending, stopping once `wantBytes` have been freed — but never a segment starting at or after `keepAfter`. Returns the count deleted and bytes freed. Used by the machine-health monitor's disk-mitigation "overwrite oldest" (continuous recording) mode instead of pausing.

## Notes

- `SaveSegment` satisfies `recording.SegmentSink`; it is called from a background goroutine in the infra recorder and must be safe for concurrent use (each call creates its own DB statement through the generic repo).
- File removal in `DeleteSegment`, `PurgeOldSegments`, and `PurgeOldestSegments` uses `recording.SecureRemove(path, shredPasses)` — a secure multi-pass overwrite-then-unlink when shredding is enabled, otherwise a plain delete; missing-file errors are silently ignored to avoid blocking row cleanup. `shredPasses` is passed to `NewRecordingService` (resolved from the `recording.shred` config; 0 = plain delete). A manual `POST /recording/segments/purge` invokes `PurgeOldSegments` on demand.
- `GetConfig` returns `nil, nil` when no config exists for the requested camera ID rather than an error, allowing callers to detect a first-time save.
- `PurgeOldSegments` is designed to be called on a schedule (e.g., at startup and periodically); it is not called automatically by the service.
- `LiveStreamUrl` is saved and returned in the API response so the Recording UI can restore the selected live stream across page reloads without reverting to the camera's default RTSP URL.
