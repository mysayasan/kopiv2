# Module: infra/db/sql/scan_limit.go

## Purpose

Answers the one question all three drivers (`sqlite`, `postgres`, `mariadb`) used to answer
wrongly, in triplicate: how many rows may a `selectWithSQL` scan loop materialise from a result
set.

## The defect this closes

All three drivers hardcoded a scan-loop ceiling of 100 rows and applied it to EVERY query,
regardless of the `limit` the caller passed. The generated SQL still said `LIMIT 2000`; the
database still found and returned that many rows; the scan loop stopped at 100 and discarded the
rest. Worse, `x_rows_cnt` is computed by the database over the whole result set, so the reported
total came back TRUE — a caller had a page of 100 rows and a total of 2000 with no way to tell the
read had been truncated rather than legitimately paged. Measured live: a telemetry chart asking
for 2000 samples drew 100; a device page folding the newest 500 readings into "the current value
of every key" saw 100, so a busy key crowded the others off the page entirely; and the myiotsan
flow runtime, asking for its 500 enabled flows, compiled 100 — an install's hundred-and-first flow
was listed nowhere and never ran, with no error at any layer. Roughly seventy call sites across the
suite ask for more than a hundred rows.

## Behavior

```go
const DefaultScanRowLimit = uint64(100)
func ScanRowLimit(limit uint64) uint64
```

- `ScanRowLimit(limit)` returns `limit` unchanged when it is non-zero — the SQL already bounds the
  result to that many rows, so the scan loop simply takes what came back.
- It returns `DefaultScanRowLimit` (100, the historical value, kept deliberately) only when
  `limit == 0` — the caller asked for no `LIMIT` clause at all, so without a ceiling here a single
  call could materialise the largest table in the schema (the hot tables in an appliance install,
  e.g. `device_reading`/`recording_segment`, grow forever). A caller that means to read more than
  100 rows must say so by passing a limit.
- Each driver's `selectWithSQL` was widened to take `limit uint64` as a parameter (from `Select`
  and `SelectJoin`) and now sets `maxRowCnt := dbsql.ScanRowLimit(limit)` instead of the old
  hardcoded `100`.

## Notes

- One function, three call sites (`sqlite/db_crud_sel.go.md`, `postgres/db_crud_sel.go.md`,
  `mariadb/db_crud_sel.go.md`) — a single place to change the ceiling policy rather than three
  copies that can drift.
- Pinned by `TestSQLiteSelectReturnsTheLimitAsked` (`sqlite/db_crud_test.go.md`): writes 250 rows,
  asserts `limit=250` returns all 250 with the tail row proving it (not just a long-enough slice),
  asserts `limit=10` still pages correctly, and asserts `limit=0` is capped at
  `DefaultScanRowLimit`.
