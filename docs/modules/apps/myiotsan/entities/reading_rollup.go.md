# Module: apps/myiotsan/entities/reading_rollup.go

## Purpose

Defines a downsampled bucket: count/min/max/sum/last of one key on one device over one span. It
is what a chart of a month reads instead of a million raw rows, and it is what SURVIVES when
retention purges the raw readings underneath it. The pattern — and the cursor that drives it —
deliberately mirrors mymatasan's notification rollup: the shape is identical and that one is
proven.

## Fields

- `Id`, `DeviceId`, `Key`, `Span` (`"1m"` or `"1h"`), `Bucket` (the bucket's start time, unix
  seconds, floored to the span).
- `Count`, `Min`, `Max`, `Sum`.
- `Last` — the final value in the bucket; the only meaningful summary for a STATE-like key (a
  door's last position), where an average is nonsense.

## Indexes

`idx:"dev_key_span"` spans `DeviceId`, `Key`, `Span`, `Bucket` — the exact shape both the chart
read (`services.TelemetryService.Rollups`) and the rollup cursor (`rollupOnce`, latest bucket of
a span) query against.

## Notes

- Built by `services.TelemetryService.RunRollup` (`apps/myiotsan/services/telemetry.go.md`),
  which runs the rollup pass BEFORE the raw-row purge in the same tick — reversing that order
  would silently throw away data the rollup was supposed to preserve.
- Retention is longer for rollups than for raw readings (`rollupRetentionDays` default 400 vs.
  `rawRetentionDays` default 30) — last summer stays comparable to this one, at a fraction of
  the size, long after the raw rows are gone.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
