# Module: domain/entities/notification_rollup.go

## Purpose

Defines the `NotificationRollup` entity: an hourly, pre-aggregated tally of notification activity that backs the mymatasan Dashboard Intelligence analytics (heatmaps, expected-activity baselines, statistical anomaly alerts) without re-scanning the raw `notification` table.

## Fields

- `Id`: auto-increment primary key.
- `BucketStart`: the bucket's UTC hour start (unix seconds). Callers re-bucket into local hour-of-day × day-of-week slots when computing baselines/heatmaps.
- `CameraId`, `Category`, `Severity`, `RuleId`, `Label`: the composite bucket key (`ukey:"slot"` on all five plus `BucketStart`) — deliberately wide so per-rule noise baselines and per-label object-mix analysis need no later schema migration. `RuleId`/`Label` populate only for vision-alert rows (`0`/`""` otherwise).
- `Count`: number of notifications folded into this bucket.
- `UpdatedAt`: unix seconds; last increment time.

## Notes

- Maintained incrementally by `domain/notification.RollupMaintainer` (`rollup.go`), which sweeps the `notification` table past a persisted cursor and folds each row into its `(BucketStart, CameraId, Category, Severity, RuleId, Label)` bucket via create-or-increment.
- Read by `domain/notification.Service.Heatmap`/`Baseline`/`AnomalyScan`, which the mymatasan app only wires up when `Service.WithRollups` is called (`apps/mymatasan/app/app.go`).
- The shared bootstrap engine creates and additively migrates the `notification_rollup` table from this entity, same as any other registered entity.
