# Module: apps/myidsan/main.go

## Purpose

Legacy app-local launcher for running the `myidsan` app directly.

## Behavior

- Instantiates `apps/myidsan/app.New`.
- Runs it through `infra/apphost.Run`.

## Notes

- The preferred local entrypoint is `go run . -app myidsan`.
- The compile-time entrypoint is `cmd/myidsan`.
