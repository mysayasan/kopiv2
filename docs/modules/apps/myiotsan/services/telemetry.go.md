# Module: apps/myiotsan/services/telemetry.go

## Purpose

Reads the telemetry history back out — charts, latest values — and runs the rollup and
retention workers that keep the hot `device_reading` table from growing without bound.

## Key Type: TelemetryService

```go
func NewTelemetryService(db dbsql.IDbCrud, logf func(string, ...any)) *TelemetryService
```

- `Series(ctx, deviceId, key, fromSec, toSec, maxPoints)` — a device's readings for one key over
  a window, oldest first. `maxPoints` (default 2000) caps what is returned — a month of a busy
  key can be tens of thousands of rows and a 900px-wide chart cannot draw them; over the cap,
  callers are expected to read `Rollups` instead.
- `Latest(ctx, deviceId)` — the most recent reading per key, what the device page shows at the
  top. Implemented by reading the recent tail (500 rows) and folding it down in memory, because
  a correlated per-key subquery is not expressible through the generic repo — the tail stays
  small precisely because the deadband keeps it small.
- `ValueAt(ctx, deviceId, key, atSec)` — the reading **in effect** at a moment: the last row at
  or before it, not the row recorded AT that instant. With a deadband there is usually no row at
  the exact moment asked about (a door opened at 02:14 and hasn't moved since has no 03:00 row,
  but is still open at 03:00) — getting this wrong would quietly break every `delta`/`rate`/
  `stuck` rule condition, which all read through this.
- `Rollups(ctx, deviceId, key, span, fromSec, toSec)` — the downsampled buckets for a long
  window.
- `RunRollup(ctx, RetentionConfig)` — a `safego.Supervise`d hourly loop: builds `1m` and `1h`
  buckets (`rollupOnce`), then purges (`purge`). **Rollup runs before purge, on the same tick**
  — the reverse order would silently discard the raw data the rollup was meant to preserve
  before anyone asked for a chart of last month.
- `rollupOnce(ctx, span, width)` — only folds COMPLETE buckets (`cutoff = now - width`), so the
  still-filling current bucket is never summarized and then have to be corrected. Finds its
  starting point from the last existing rollup of that span (a cursor, not a full rescan), reads
  up to 20000 candidate readings, accumulates count/min/max/sum/last per (device, key, bucket)
  in memory, then batch-inserts.
- `purge(ctx, RetentionConfig)` — deletes raw readings older than `RawDays` and rollups older
  than `RollupDays`. Raw rows go first, rollups much later, "so the shape of the past survives
  long after its detail does."

## Key Type: RetentionConfig

`RawDays` (default 30), `RollupDays` (default 400 — over a year, so last summer is comparable
to this one), `Interval` (default 1h). Sourced from `apps/myiotsan/config`'s `telemetry_store`
block.

## Notes

- Isolated errors (`isNoResultErr`) are swallowed as empty results, not propagated — a device
  with no readings yet is not an error condition.
- Wired from `apps/myiotsan/app/app.go` via `telemetry.RunRollup(bgCtx, RetentionConfig{...})`.
