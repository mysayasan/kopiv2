# kopiv2 Documentation

This folder is the technical documentation set for the mini framework inside this repository.

## Documents

- `TECHNICAL_SPEC.md`: system-level specification and constraints.
- `REQUEST_FLOW.md`: request lifecycle and runtime flow.
- `HOWTO.md`: runbooks and operational procedures.
- `DB_BOOTSTRAP_SPEC.md`: shared code-first database bootstrap design.
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
