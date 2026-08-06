# Module: domain/entities/notification_rollup.go

## Purpose

Defines the `NotificationRollup` entity: an hourly, pre-aggregated tally of notification activity that backs the mymatasan Dashboard Intelligence analytics (heatmaps, expected-activity baselines, statistical anomaly alerts) without re-scanning the raw `notification` table.

## Fields

- `Id`: auto-increment primary key.
- `BucketStart`: the bucket's UTC hour start (unix seconds). Callers re-bucket into local hour-of-day × day-of-week slots when computing baselines/heatmaps.
- `CameraId`, `Category`, `Severity`, `RuleId`, `Label`, `Source`: the composite bucket key (`ukey:"slot"` on all six plus `BucketStart`) — deliberately wide so per-rule noise baselines and per-label object-mix analysis need no later schema migration. `RuleId`/`Label` populate only for vision-alert rows (`0`/`""` otherwise).
- `Source` (new): the folded notification's `Source` field (e.g. `"node:<id>"` on a control-plane app like myseliasan), enabling **per-node baselines** — a band computed against one source's own history instead of the fleet-wide envelope. Rows folded **before** this column existed carry `""` and keep aggregating into every fleet-wide (`source=""`) read; per-source bands simply stay in "learning" until enough source-split history accumulates after the upgrade.
- `Count`: number of notifications folded into this bucket.
- `UpdatedAt`: unix seconds; last increment time.

## Upgrading an existing database

Adding `Source` to the unique key changes the bucket identity: two rows that used to be one slot
(same camera/category/severity/rule/label, different source) must now be two rows. A database
whose `notification_rollup` table (and its `ux_notification_rollup_slot` unique index) predates
this column needs the index rebuilt **with** `Source` included, or the rollup maintainer's first
source-split insert violates the stale index and folding stops advancing entirely. Both apps that
fold rollups (`mymatasan`, `myseliasan`) register the shared idempotent migration
`domain/notification.MigrateRollupSourceColumn` (`domain/notification/rollup_migrate.go.md`) as
migration id `20260806-01-notification-rollup-source` to handle this automatically on first boot
after the upgrade — no operator action required. A fresh install never runs it (the auto-migrator
creates the complete shape, `Source` included, from this entity directly).

## Notes

- Maintained incrementally by `domain/notification.RollupMaintainer` (`rollup.go`), which sweeps the `notification` table past a persisted cursor and folds each row into its `(BucketStart, CameraId, Category, Severity, RuleId, Label, Source)` bucket via create-or-increment.
- Read by `domain/notification.Service.Heatmap`/`Baseline`/`AnomalyScan`. `Baseline` takes a
  trailing `source string` param (`""` = every source summed, the fleet-wide band) — see
  `docs/modules/apps/myseliasan/apis/notifications.go.md`'s `?nodeId=` and
  `docs/modules/apps/myseliasan/services/agent_findings.go.md`'s `node_baseline_spike`/`quiet`
  findings for the per-node consumers. The mymatasan app only wires rollups up at all when
  `Service.WithRollups` is called (`apps/mymatasan/app/app.go`), and always queries with
  `source=""` (mymatasan has no notion of "source node").
- The shared bootstrap engine creates and additively migrates the `notification_rollup` table from this entity, same as any other registered entity — additive columns need no explicit migration; the **unique-index rebuild** above is the one thing additive reconciliation alone cannot express, hence the versioned migration.
