# Module: apps/mymatasan/entities/alert_event.go

## Purpose

Defines the persisted alert event record raised by a MyMataSan detection rule.

## Fields

- Alert identity: `Id`.
- Source binding: `RuleId`, `CameraId`.
- Detection result: `DetectionType`, `Label`, `Confidence`, `ZonePolygon`, `BoundingBox`, `SnapshotPath`, `Metadata`.
- Acknowledgement state: `IsAcknowledged`, `AcknowledgedBy`, `AcknowledgedAt`.
- Audit fields: created/updated user and timestamps.

## Notes

- `Metadata` is JSON text used by detectors and monitor diagnostics for extra details such as source, status, message, and changed-frame ratio.
- The AI alert UI renders these rows as a table and opens full metadata through a details action.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB engine starts.
