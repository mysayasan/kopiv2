# Module: infra/db/sql/sqlite/db_close_test.go

## Purpose

Regression test for the sqlite `Close()` method added to unblock the factory reset's database drop on Windows.

## Coverage

- Opens a temporary file-backed sqlite database via `NewDbCrud`.
- Confirms the file cannot always be reliably removed while the connection is still open (logs the OS-dependent result rather than asserting it, since behavior varies by platform).
- Asserts `crud.(io.Closer)` succeeds and `Close()` returns no error.
- Asserts the database file **can** be removed after `Close()` on every OS — the actual reset guarantee: sqlite file handles are released so `infra/db/bootstrap.Reset`'s file removal (via `removeWithRetry`) no longer fails with "file in use by another process".
