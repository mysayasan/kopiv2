# Module: infra/db/bootstrap/mariadb_integration_test.go

## Purpose

Provides an opt-in integration test that runs the bootstrap engine against a real MariaDB container.

## Behavior

- Skips by default unless `RUN_MARIADB_IT=1` is set.
- Starts a temporary `mariadb:latest` container using Docker.
- Waits for DB readiness using `mariadb-admin ping`.
- Calls `bootstrap.Ensure` with `db.engine=mariadb`.
- Verifies first run and second run both return `ready=true`.
- Removes the temporary container after test completion.

## Why It Exists

- Validates MariaDB bootstrap behavior against a real database.
- Prevents regressions in dialect-specific bootstrap logic.
