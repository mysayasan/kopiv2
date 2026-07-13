# Module: infra/db/bootstrap/types.go

## Purpose

Defines the shared bootstrap configuration, runtime options, manifest status, and seeder contract.

## Key Types

- `BootstrapConfig`
  - controls whether bootstrap runs and what it is allowed to create/update
- `Options`
  - app name, DB config, entity registry, optional versioned `Migrations` (`migration.go.md`),
    and optional seeders
- `Status`
  - result object returned after bootstrap completes; now also reports
    `MigrationsApplied []string` (the migration IDs this run executed — empty on a fresh
    database, which baselines them instead) and `SchemaDrift []SchemaDrift` (differences the
    additive auto-migrator cannot fix — changed column types, columns the entities no longer
    declare; see `drift.go.md`). `SchemaDrift` is distinct from the pre-existing
    `DriftDetected` bool, which just means the manifest hash changed and additive updates
    were applied.
- `Seeder`
  - interface for app-level or shared initial-data providers

## Notes

- This file is intentionally small and contract-focused.
- Apps should only pass entity values, optional migrations, and optional seeders into the
  shared engine.
