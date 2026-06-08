# Module: main.go

## Purpose

Root launcher for selecting which app module to run.

## Behavior

- Reads `-app` flag.
- Resolves app module from an in-process registry map.
- Runs selected module via `infra/apphost.Run`.
- Prints available app names and exits with non-zero code when app name is unknown.
- Currently registers `mymatasan`, `myidsan`, and `myseliasan`.

## Notes

- Runtime selection is convenient for local development.
- Compile-time selection remains available through per-app `cmd/<app>` targets.
