# Module: infra/db/sql/sqlite/db_crud_test.go

## Purpose

Validates the SQLite runtime adapter against the shared repository contract.

## Coverage

- Creates a temporary file-backed SQLite database.
- Exercises create, read, update, delete, filtering, sorting, and total-count behavior through `GenericRepo`.
- Verifies request-scoped transaction rollback behavior.
- Regression test for the `SelectByUnique` fail-closed fix: asserts that a `GetByUnique` call whose key group matches no field returns `(nil, nil)` rather than the first row, confirming the auth-escalation bug is closed.
- Verifies the `In` compare operator builds a correct `col IN (...)` clause (multi-value filter matches any listed value) and that an empty `In` list drops the constraint entirely rather than matching nothing.
- `TestSQLiteSelectReturnsTheLimitAsked` regression-tests the cross-driver scan-limit fix (`infra/db/sql/scan_limit.go.md`): writes 250 rows and asserts `Get(limit=250)` returns all 250 (checking the tail row's value, not just the slice length) and reports `totalCnt = 250`; asserts `Get(limit=10)` still pages to exactly 10 against the same 250-row total; and asserts `Get(limit=0)` — no limit requested — is capped at `dbsql.DefaultScanRowLimit` (100) rather than reading everything unbounded.
