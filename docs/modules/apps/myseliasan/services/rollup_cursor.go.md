# Module: apps/myseliasan/services/rollup_cursor.go

## Purpose

Implements `notification.Cursor` (from the app-neutral `domain/notification` package) backed by
myseliasan's `RuntimeSetting` table, so the hourly rollup maintainer's watermark survives restarts
without the domain package depending on any particular app's settings storage. Identical in shape
to `apps/mymatasan/services/rollup_cursor.go.md` — this is the analytics substrate
(rollups + retention purge + `GET /api/notifications/baseline`) landing on myseliasan for the
first time, wired the same way mymatasan already does it.

## Responsibilities

- `NewRollupCursor(repo dbsql.IGenericRepo[sharedentities.RuntimeSetting]) notification.Cursor` —
  returns a cursor persisted under the single runtime-setting key `notification.rollup.cursor`
  (`rollupCursorKey` — the same key mymatasan uses; safe, since the value lives in each app's own
  database and the two never collide).
- `Get(ctx)` — reads the last-folded notification id; returns `0` (not an error) when the key
  doesn't exist yet or its stored value fails to parse (a corrupt value deliberately restarts the
  rollup sweep from zero rather than wedging — a rare safety valve, not a hot path).
- `Set(ctx, lastID)` — upserts the watermark (create-or-update by the `key` unique field).

## Notes

- Used by `notification.RollupMaintainer` (`app.go` wires `services.NewRollupCursor
  (runtimeSettingRepo)` into `notification.NewRollupMaintainer`, alongside the notification and
  rollup repos), so an existing install's **first sweep backfills every historical notification
  row** past the persisted cursor (starts at `0`), and every later sweep resumes from where it left
  off. This is what backs both the Dashboard/Insight chart's expected-activity band
  (`GET /api/notifications/baseline`, `apis/notifications.go.md`) and the AI digest's baseline
  findings (`services/agent_findings.go.md`'s `baselineFindings`).
