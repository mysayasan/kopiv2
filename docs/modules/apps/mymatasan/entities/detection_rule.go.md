# Module: apps/mymatasan/entities/detection_rule.go

## Purpose

Defines the persisted AI detection rule record for standalone `mymatasan`.

## Fields

- Rule identity: `Id`, `Name`.
- Camera binding: `CameraId`.
- Detection behavior: `DetectionType`, `ZonePolygon`, `RuleConfig`, `SchedulePolicy`, `Threshold`, `MinFrames`, `CooldownSeconds`, `SoundEnabled`, `IsEnabled`.
- Runtime state: `LastTriggeredAt`.
- Audit fields: created/updated user and timestamps.

## Notes

- `ZonePolygon` is JSON text containing normalized video points from `0` to `1`. It accepts either a single polygon `[[x,y],...]` (legacy) or a list of polygons `[[[x,y],...],...]` (multi-zone); `infra/vision` (`parseZones`) evaluates multiple zones as a union — a detection counts if it falls inside any one zone. No DB migration is needed; the column is unchanged text.
- `RuleConfig` is optional JSON text for detector-specific rule configuration, including line-crossing class filters and ordered line points.
- `SchedulePolicy` is JSON text evaluated per rule. Empty policy means always active.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB engine starts.
