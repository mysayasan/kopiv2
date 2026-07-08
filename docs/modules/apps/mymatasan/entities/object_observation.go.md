# Module: apps/mymatasan/entities/object_observation.go

## Purpose

Declares the `ObjectObservation` entity: one coalesced presence interval recorded by the object metadata recorder — the "text recording" of what a camera saw, searchable and linked to footage by time.

## Fields

| Field           | Type    | Notes |
|-----------------|---------|-------|
| `Id`            | int64   | Auto-increment primary key. |
| `CameraId`      | int64   | Camera the interval belongs to. |
| `Label`         | string  | Lowercased object label (e.g. `person`, `car`). |
| `StartedAt`     | int64   | Unix seconds; first sample the label was seen. |
| `EndedAt`       | int64   | Unix seconds; last sample before the interval closed (the camera's configured gap window elapsed with no further sighting). |
| `MaxConfidence` | float64 | Highest single-frame confidence observed for the label across the interval. |
| `MaxCount`      | int     | Highest simultaneous count of the label seen in one frame across the interval. |
| `SampleCount`   | int     | Number of sampled frames that contributed to the interval. |
| `PeakBox`       | string  | JSON-encoded `vision.Box` of the highest-confidence sighting, for a representative thumbnail/crop. |
| `Attributes`    | string  | Reserved for Phase-2 attribute enrichment (per-object tracking + color/clothing); ships empty, no migration needed to populate later. |
| `TrackId`       | string  | Reserved for Phase-2 (ByteTrack-style per-object tracking); ships empty. |
| `SegmentId`     | int64   | Reserved; the covering recording segment is currently resolved at read time (`services.ObservationService.coveringSegment`) rather than stored. |
| `CreatedAt`     | int64   | Unix seconds; row write time. |

## Notes

- Written by `services.MetadataRecorder`, which coalesces many sampled frames into one row per continuous presence span so metadata storage scales for long retention.
- Read/searched by `services.ObservationService` (`GET /api/observations`) and linked to footage by time overlap (camera + `StartedAt`/`EndedAt` against `RecordingSegment.StartedAt`/`EndedAt`), not by a stored foreign key.
- Purged by `services.ObservationService.PurgeOldObservations` in step with each camera's recording `RetentionDays` (falling back to a 30-day default for cameras without one).
- Secondary indexes `(camera_id, started_at)` and `(label, started_at)` are added by a dedicated `app.go` seeder (`CREATE INDEX IF NOT EXISTS`, engine-portable) since the ORM only emits unique indexes from struct tags.
