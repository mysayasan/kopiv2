# Module: apps/mymatasan/services/observation.go

## Purpose

Implements `ObservationService`, the read/maintenance side of the object metadata recorder: searches recorded presence intervals and resolves each to the footage segment covering it, plus retention-aligned purge.

## Responsibilities

- `NewObservationService(repo, recording IRecordingService)` builds the service; `recording` resolves footage links and drives purge retention.
- `GetObservations(ctx, limit, offset, cameraId, extraFilters, extraSorters)` — true server-side filtered/sorted/paged search: `cameraId` (when `>0`) is the mandatory base constraint, `extraFilters`/`extraSorters` come straight from the client `DataTable` (column filters + sort, e.g. a time daterange, object `Label`, or `MaxCount`) so paging runs over the real filtered set. Defaults to `StartedAt DESC` when no sort is supplied. Each result is an `ObservationResult` — the raw `ObjectObservation` plus `SegmentId`/`SegmentCodec`/`SeekSeconds` for click-to-play.
- `coveringSegment(ctx, cameraId, at)` — returns the recording segment whose time span contains `at` (fetches the single segment with the greatest `StartedAt <= at` via `IRecordingService.GetSegments` and checks it actually spans `at`), or `nil` when the moment wasn't recorded (recording off, or a gap).
- `Labels(ctx, cameraId)` — distinct object labels observed for a camera (or all cameras), scanning a bounded window of recent rows (2000) rather than a `DISTINCT` query, keeping it engine-agnostic. Backs the search UI's label filter list.
- `PurgeOldObservations(ctx)` — deletes presence intervals past retention: each camera's own `RecordingConfig.RetentionDays` wins when set, else `defaultObservationRetentionDays` (30). Batches deletes 500 rows at a time by ascending `EndedAt`. Returns the count deleted.

## Notes

- `ObservationResult` embeds `*entities.ObjectObservation` by pointer, so JSON output flattens the interval fields alongside the added footage-link fields.
- Footage linkage is by time overlap, not a stored foreign key — an observation always resolves against the *current* segment table, so it stays correct even if segments are re-indexed.
- Wired in `app.go` alongside `MetadataRecorder` (write side) and exposed at `GET /api/observations` (+ `/labels`) via `apis/observation.go`; `PurgeOldObservations` runs on the same 6-hourly loop as `IRecordingService.PurgeOldSegments`.
