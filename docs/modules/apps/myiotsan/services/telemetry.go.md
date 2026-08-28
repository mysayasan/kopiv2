# Module: apps/myiotsan/services/telemetry.go

## Purpose

Reads the telemetry history back out — charts, latest values — and runs the rollup and
retention workers that keep the hot `device_reading` table from growing without bound.

## Key Type: SeriesPage

```go
type SeriesPage struct {
	Items     []*entities.DeviceReading `json:"items"`
	Span      string                    `json:"span"`      // "raw" | "1m" | "1h"
	Truncated bool                      `json:"truncated"`
}
```

What `Series` actually returns: the points AND what resolution they carry. A chart that quietly
swaps raw samples for hourly summaries without saying so is a chart that lies. `Truncated` is
true only when the window held more raw points than the cap and no rollups existed yet to
summarize them — the caller got the most recent slice of the window, not the whole of it.

## Key Type: TelemetryService

```go
func NewTelemetryService(db dbsql.IDbCrud, logf func(string, ...any)) *TelemetryService
```

- `Series(ctx, deviceId, key, fromSec, toSec, maxPoints)` — a device's readings for one key over
  a window, oldest first, returned as a `SeriesPage`. `maxPoints` (default 2000) caps what is
  returned — a month of a busy key can be tens of thousands of rows and a 900px-wide chart
  cannot draw them. **Which end gets discarded matters**: the window is read newest-first
  (`rawWindow`, DESC + reverse in memory) so the cap bites the OLD end, not the recent one — the
  old behavior (ASC with a limit) drew the oldest slice of a wide window and stopped, which looks
  identical to a device that died days ago. Over the cap it picks the coarsest span that still
  fits (`1m` if the window's minute-count is within `maxPoints`, else `1h`) and reads it from
  `rollupSeries`; only when no rollup bucket covers the window at all does it fall back to the
  most recent `maxPoints` raw rows with `Truncated: true`.
- `rawWindow(ctx, deviceId, key, fromSec, toSec, limit)` — the DESC-then-reverse read `Series`
  and `rollupSeries`'s tail-topup both use.
- `rollupSeries(ctx, deviceId, key, span, width, fromSec, toSec, maxPoints)` — renders a window
  from `Rollups` buckets (using each bucket's `Last`, not its mean — a state-like key's average
  is nonsense, and `Last` is a value the sensor actually reported), then tops the result up with
  the raw tail the rollup worker has not folded yet (`rawWindow` from the last bucket's end to
  `toSec`) so the chart still reaches the present. Returns `(nil, false)` when no bucket covers
  the window, so `Series` can fall back to raw rather than draw an empty chart over data that
  exists.
- `Latest(ctx, deviceId, keys)` — the most recent reading per key, what the device page shows at
  the top. Takes the profile's **declared key list** and does one indexed `(device, key, time)`
  seek per key (`LIMIT 1 DESC`), rather than reading a fixed tail and folding it. This is a fix
  for a real outage: the old version read the device's newest 500 rows and folded them down, on
  the theory that "the tail is small because the deadband keeps it small" — true PER KEY, not
  for the device as a whole. One key publishing every second fills 500 rows in eight minutes and
  pushes every other key on the device off the tail; measured live, a seven-key device showed
  exactly one key after one of them wrote 520 rows. `keys` is bounded and small (the profile's
  own key count), so per-key seeks are also cheaper than the 500-row scan they replaced. A device
  with no profile (`len(keys) == 0`) falls back to `latestByTail` — the old tail-fold — since
  there is no declared key list to seek by and, for an undecodable device, no volume of rows to
  crowd it anyway.
- `latestByTail(ctx, deviceId)` — the fallback above: reads the newest 500 rows across the whole
  device and folds them down in memory, first-seen-per-key.
- `ValueAt(ctx, deviceId, key, atSec)` — the reading **in effect** at a moment: the last row at
  or before it, not the row recorded AT that instant. With a deadband there is usually no row at
  the exact moment asked about (a door opened at 02:14 and hasn't moved since has no 03:00 row,
  but is still open at 03:00) — getting this wrong would quietly break every `delta`/`rate`/
  `stuck` rule condition, which all read through this.
- `Rollups(ctx, deviceId, key, span, fromSec, toSec)` — the downsampled buckets for a long
  window. Now has a real caller (`rollupSeries`, above) — before this change it had ZERO callers
  anywhere in the app, so the rollup table was write-only: built every hour and never read back.
- `RunRollup(ctx, RetentionConfig)` — a `safego.Supervise`d loop on `RetentionConfig.Interval`:
  builds `1m` and `1h` buckets (`rollupOnce`), then purges (`purge`). **Rollup runs before purge,
  on the same tick** — the reverse order would silently discard the raw data the rollup was meant
  to preserve before anyone asked for a chart of last month.
- `rollupOnce(ctx, span, width)` — only folds COMPLETE buckets. The cutoff is now floored to a
  bucket boundary (`now.Unix() / secs * secs`), not `now - width`: the old cutoff landed
  MID-BUCKET (at 10:01:30 it cut at 10:00:30, the middle of the 10:00 bucket), wrote that bucket
  from half a minute of data, then advanced the cursor past it — the readings from 10:00:30 to
  10:00:59 were folded by nothing, permanently. Finds its starting point from the last existing
  rollup of that span (a cursor, not a full rescan), reads up to `rollupBatch` (20000) candidate
  readings, drops an unproven-complete trailing bucket (`dropIncompleteTail`), accumulates
  count/min/max/sum/last per (device, key, bucket) in memory, then batch-inserts.
- `dropIncompleteTail(rows, secs, truncated)` — when a pass fills its `rollupBatch`, the last rows
  read very likely belong to a bucket the read cut in half; folding that bucket would have the
  same permanent-undercount consequence as the old mid-bucket cutoff, because the cursor advances
  past whatever gets folded. Trims those trailing rows so the pass stops at the last bucket it can
  PROVE is whole — except when a single bucket is wider than the whole batch, in which case
  dropping it would leave nothing to fold, the cursor would never advance, and every future pass
  would read and drop the same rows forever; that one case folds the partial bucket rather than
  stall the rollup permanently.
- `purge(ctx, RetentionConfig)` — deletes raw readings older than `RawDays` and rollups older
  than `RollupDays`. Raw rows go first, rollups much later, "so the shape of the past survives
  long after its detail does."

## Key Type: RetentionConfig

`RawDays` (default 30), `RollupDays` (default 400 — over a year, so last summer is comparable
to this one), `Interval` (default 1h, now genuinely settable — see `config.go.md`'s
`RollupIntervalMs` — because a background job with no way to make it run is a job nobody has ever
watched do its work: on every bench of this app before this change, the rollup worker had never
run once). Sourced from `apps/myiotsan/config`'s `telemetry_store` block (`Interval` specifically
from raw `Config.Telemetry.RollupIntervalMs`, not the runtime-editable telemetry settings store —
see `app/app.go.md`).

## Notes

- Isolated errors (`isNoResultErr`) are swallowed as empty results, not propagated — a device
  with no readings yet is not an error condition.
- Wired from `apps/myiotsan/app/app.go` via `telemetry.RunRollup(bgCtx, RetentionConfig{...})`.
- `GET /api/devices/{id}/readings` (`apis/devices.go.md`) serves `Series`'s `SeriesPage` as
  `{items, span, truncated}`; `GET /api/devices/{id}/latest` serves `Latest` fed the calling
  device's profile-declared key list.
- Live-benched by `tools/fleetbench/bench_iotsan_telemetry.py` (50/50 after the fixes above,
  33/44 against the unfixed binary) — see `docs/MYIOTSAN_PLAN.md`'s telemetry read-back
  hardening entry and `tools/fleetbench/README.md`.
