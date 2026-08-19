# kopiv2 Documentation

This folder is the technical documentation set for the mini framework inside this repository.

## Documents

- `TECHNICAL_SPEC.md`: system-level specification and constraints.
- `REQUEST_FLOW.md`: request lifecycle and runtime flow.
- `HOWTO.md`: runbooks and operational procedures (includes MyMataSan live audio, PTZ, and recording stream selection).
- `DB_BOOTSTRAP_SPEC.md`: shared code-first database bootstrap design.
- `MYIOTSAN_PLAN.md`: implementation plan for MyIotSan, the planned fourth app (IoT sensor hub).
- `MYMATASAN_TIER2_PLAN.md`: MyMataSan Tier 2 architectural-debt plan (composition-root decomposition, config seam, migrations, RBAC).
- `FLAGSHIP_HARDENING_PLAN.md`: phased plan to close the 24 findings from the 2026-08-19 MyMataSan + MySeliaSan condition register (evidence integrity, control-plane backup, fleet-scale operations). Its status board is the resume point — update it in the commit that lands the work.
- `modules/`: module documentation separated by source filename.

Integration coverage note:

- Bootstrap has an opt-in Docker-backed MariaDB integration test in `infra/db/bootstrap/mariadb_integration_test.go` and a regular SQLite bootstrap test in `infra/db/bootstrap/sqlite_test.go`.
- Shared OpenAPI/Swagger runtime module is in `infra/apidocs/openapi.go`.

## Module Docs Convention

- Every documented module maps to one source file.
- File naming pattern:
  - source file `path/to/file.go`
  - doc file `docs/modules/path/to/file.go.md`
- Keep function names and endpoint paths in sync with code.

## Update Rule

Any code change that modifies behavior, config, API routes, middleware, infra, or runtime flow must also update:

1. The matching file in `docs/modules/...`.
2. Supporting docs in this folder when affected (`TECHNICAL_SPEC.md`, `REQUEST_FLOW.md`, `HOWTO.md`).
3. Root `README.md` when usage/architecture/operations are affected.
