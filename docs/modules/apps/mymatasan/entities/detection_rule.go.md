# Module: apps/mymatasan/entities/detection_rule.go

## Purpose

Defines the persisted AI detection rule record for standalone `mymatasan`.

## Fields

- Rule identity: `Id`, `Name`.
- Camera binding: `CameraId`.
- Detection behavior: `DetectionType`, `ZonePolygon`, `RuleConfig`, `SchedulePolicy`, `Threshold`, `MinFrames`, `CooldownSeconds`, `SoundEnabled`, `IsEnabled`.
- `ArchiveClip` (W2-3) — asks the control plane to keep a copy of this rule's event clip
  and snapshot, OFF this appliance, so the footage survives the box being stolen, burned
  or wiped by whoever triggered the alert. **Default false.** Per RULE, not per camera and
  not fleet-wide, because that is the granularity at which "this matters" is known: a
  line-crossing rule on the perimeter gate at night is evidence, and the same camera's
  daytime person-detection is noise. The flag lives on the node because the fleet has no
  way to know what a rule means — the person who wrote the rule does — and it rides
  upstream on each alert as the notification Data key `archiveClip`
  (`infra/notification.DataArchiveClip`), which is how the control plane learns to fetch
  the clip without reading the node's rule table. See
  `docs/modules/apps/myseliasan/services/clip_archive.go.md`.
- Runtime state: `LastTriggeredAt`.
- Audit fields: created/updated user and timestamps.

## Notes

- `ZonePolygon` is JSON text containing normalized video points from `0` to `1`. It accepts either a single polygon `[[x,y],...]` (legacy) or a list of polygons `[[[x,y],...],...]` (multi-zone); `infra/vision` (`parseZones`) evaluates multiple zones as a union — a detection counts if it falls inside any one zone. No DB migration is needed; the column is unchanged text.
- `RuleConfig` is optional JSON text for detector-specific rule configuration, including line-crossing class filters and ordered line points.
- `SchedulePolicy` is JSON text evaluated per rule. Empty policy means always active.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB engine starts.
