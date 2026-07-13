# Module: apps/mymatasan/entities/alert_event.go

## Purpose

Defines the persisted alert event record raised by a MyMataSan detection rule.

## Fields

- Alert identity: `Id`.
- Source binding: `RuleId`, `CameraId`.
- Detection result: `DetectionType`, `Label`, `Confidence`, `ZonePolygon`, `BoundingBox`, `SnapshotPath`, `Metadata`.
- Acknowledgement state: `IsAcknowledged`, `AcknowledgedBy`, `AcknowledgedAt`.
- Audit fields: created/updated user and timestamps.

## Indexes

The highest-write table in the app — every rule, on every sampled frame, on every camera lands here (real detections plus `sampled`/`capture_failed` diagnostics) — carries two secondary indexes via the `idx` struct tag:

- `cam_time` (`camera_id`, `created_at`) — the camera-scoped alerts grid.
- `time` (`created_at`) — the retention purge (`created_at < cutoff`) and the unfiltered grid sort.

`CreatedAt` joins both groups via the comma-separated form `idx:"cam_time,time"` (see `infra/db/bootstrap/schema.go.md`). Without these the alerts grid and the retention purge degraded into full table scans within weeks of install, and the purge's scan-per-delete contended with the recorder's segment writes for the SQLite writer lock — surfacing as failed segment saves, i.e. lost footage. Existing installs pick the new indexes up automatically on the next bootstrap run (`CREATE INDEX IF NOT EXISTS`); no seeder/migration needed.

## Notes

- `Metadata` is JSON text used by detectors and monitor diagnostics for extra details such as source, status, message, and changed-frame ratio.
- The AI alert UI renders these rows as a table and opens full metadata through a details action.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB engine starts.
