# Module: apps/mymatasan/services/rollup_cursor.go

## Purpose

Implements `notification.Cursor` (from the app-neutral `domain/notification` package) backed by the mymatasan `RuntimeSetting` table, so the hourly rollup maintainer's watermark survives restarts without the domain package depending on any particular app's settings storage.

## Responsibilities

- `NewRollupCursor(repo dbsql.IGenericRepo[entities.RuntimeSetting]) notification.Cursor` — returns a cursor persisted under the single runtime-setting key `notification.rollup.cursor` (`rollupCursorKey`).
- `Get(ctx)` — reads the last-folded notification id; returns `0` (not an error) when the key doesn't exist yet or its stored value fails to parse (a corrupt value deliberately restarts the rollup sweep from zero rather than wedging — safe because the maintainer's fold is idempotent per bucket only when the rollup table is also cleared, so this is a rare safety valve, not a hot path).
- `Set(ctx, lastID)` — upserts the watermark (create-or-update by the `key` unique field).

## Notes

- Used by `notification.RollupMaintainer` (`app.go` wires `services.NewRollupCursor(runtimeSettingsRepo)` into `notification.NewRollupMaintainer`), so a fresh install's first sweep backfills all existing notification history, and every later sweep resumes from where it left off.
