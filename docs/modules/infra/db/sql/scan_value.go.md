# Module: infra/db/sql/scan_value.go

## Purpose

Answers the question the `postgres` and `mariadb` drivers used to answer wrongly, in
duplicate: what pointer do you hand `rows.Scan` for a struct field, and what do you do when
the column comes back `NULL`.

## The defect this closes

The auto-migrator is additive: a new entity field becomes an `ALTER TABLE ADD COLUMN` with no
default, which leaves every existing row `NULL`. Strings and bools already scanned through
`sql.NullString`/`sql.NullBool` for exactly that reason, but the numeric kinds (`int`/`int8`
.../`uint64`/`float32`/`float64`) were handed a raw `*int64`/`*float64`, and `database/sql`
cannot put a `NULL` in one. So the first upgrade that added a numeric field to a populated
table broke the whole `SELECT`, not just the row:

```
failover: listing nodes: select list failed: sql: Scan error on column index 8,
name "version_seen_at": converting NULL to int64 is unsupported
```

One un-backfilled column and the fleet had no nodes at all. It stayed hidden because the
sqlite driver scans into `interface{}` and maps `nil` to the zero value, so it is immune — and
every unit test in the suite runs on sqlite. The bug could only ever appear on a customer's
postgres or mariadb, on an upgrade, and only once there were rows to migrate.

## Behavior

```go
func ScanDestinationForField(fieldType reflect.Type) interface{}
func NormalizeScannedValue(raw interface{}, fieldType reflect.Type) interface{}
```

- `ScanDestinationForField` returns the pointer to hand `rows.Scan` for a struct field of the
  given reflected type: `*sql.NullString` for `string` (and for a field already declared
  `sql.NullString`), `*[]uint8` for a byte-slice field, `*sql.NullInt64` for every signed/
  unsigned int kind, `*sql.NullFloat64` for `float32`/`float64`, `*sql.NullBool` for `bool`,
  and `*interface{}` for anything else. Every destination that can receive a `NULL` is a
  `sql.NullXxx` — this is the fix: the old per-driver `scanDestinationForField` returned a raw
  `*int64`/`*float32`/etc. for the numeric kinds instead.
- `NormalizeScannedValue` converts what `ScanDestinationForField` produced back into a pointer
  to the field's own type, mapping a `NULL` to that type's zero value:
  - `*sql.NullString` → `*string` (`""` when `NULL`) — unless the entity field is itself typed
    `sql.NullString`, in which case the raw value is returned untouched so the caller keeps
    the `NULL`.
  - `*sql.NullBool` → `*bool` (`false` when `NULL`, via `value.Valid && value.Bool`).
  - `*sql.NullInt64` → a new pointer of the field's exact int/uint kind (`reflect.New` +
    `SetInt`/`SetUint`), `0` when `NULL`. A negative `Int64` is never assigned into an unsigned
    field (guards a would-be overflow rather than wrapping).
  - `*sql.NullFloat64` → a new pointer of the field's exact float kind, `0` when `NULL`.
  - Anything else (including a mismatched `fieldType.Kind()`) is passed through untouched.
- A `NULL` therefore reads as the field's **zero value**, which is what an un-backfilled column
  means: nobody has reported a version yet, nothing has been seen at, the count is none. A
  migration should still backfill the column where the zero value needs to mean something more
  specific than "unset" (see `apps/myseliasan/app/app.go.md`'s
  `20260901-01-managed-node-version-backfill`) — this is the seam that keeps a *missed*
  backfill from taking a screen down instead.

## Notes

- One pair of functions, two call sites (`postgres/db_crud_sel.go.md`,
  `mariadb/db_crud_sel.go.md`) — both drivers' `selectWithSQL` now call
  `dbsql.ScanDestinationForField`/`dbsql.NormalizeScannedValue` instead of their own duplicated,
  and divergent, `scanDestinationForField`/`normalizeScannedValue`. The sqlite driver is
  unaffected — it never needed this, since it scans into `interface{}`.
- Covered by `scan_value_test.go` (round-trips every numeric/string/bool kind through both
  functions, `NULL` and non-`NULL`, in-process) and by
  `postgres/null_numeric_smoke_test.go`'s `TestSmokeSelectSurvivesNullNumericColumn` — an
  opt-in smoke test (`KOPIV2_POSTGRES_SMOKE=1`, skipped otherwise) that runs `Select` against a
  real postgres `managed_node` table and asserts every row's `version`/`versionSeenAt` scan out
  as `*string`/`*int64` with no error, regardless of `NULL`. It has to run against a real
  postgres to mean anything, since sqlite's `interface{}` scan is immune to the bug it
  reproduces.
