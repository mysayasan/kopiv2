# Module: apps/mymatasan/entities/detection_rule.go

## Purpose

Defines the persisted AI detection rule record for standalone `mymatasan`.

## Fields

- Rule identity: `Id`, `Name`.
- Camera binding: `CameraId`.
- Detection behavior: `DetectionType`, `ZonePolygon`, `SchedulePolicy`, `Threshold`, `MinFrames`, `CooldownSeconds`, `SoundEnabled`, `IsEnabled`.
- Runtime state: `LastTriggeredAt`.
- Audit fields: created/updated user and timestamps.

## Notes

- `ZonePolygon` is JSON text containing normalized video points from `0` to `1`.
- `SchedulePolicy` is JSON text evaluated per rule. Empty policy means always active.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB engine starts.
