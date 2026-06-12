# Module: cmd/myidsan/main.go

## Purpose

Compile-time entrypoint for building only the `myidsan` app binary.

## Behavior

- Instantiates `apps/myidsan/app.New`.
- Runs the app through `infra/apphost.Run`.

## Notes

- Used by `go build ./cmd/myidsan` and Docker builds with `--build-arg APP=myidsan`.
