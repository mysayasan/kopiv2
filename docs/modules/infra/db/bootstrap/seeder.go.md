# Module: infra/db/bootstrap/seeder.go

## Purpose

Provides a config-driven SQL seeder implementation for initial data.

## Behavior

- Executes a list of SQL statements sequentially.
- Trims blank statements.
- Returns the first execution error with context.

## Why It Exists

- Lets new apps seed bootstrap data through config instead of per-app bespoke services.
- Keeps initial data provisioning inside the shared bootstrap package.
