# Module: infra/db/bootstrap/types.go

## Purpose

Defines the shared bootstrap configuration, runtime options, manifest status, and seeder contract.

## Key Types

- `BootstrapConfig`
  - controls whether bootstrap runs and what it is allowed to create/update
- `Options`
  - app name, DB config, entity registry, and optional seeders
- `Status`
  - result object returned after bootstrap completes
- `Seeder`
  - interface for app-level or shared initial-data providers

## Notes

- This file is intentionally small and contract-focused.
- Apps should only pass entity values and optional seeders into the shared engine.
