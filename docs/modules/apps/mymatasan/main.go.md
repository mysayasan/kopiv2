# Module: apps/mymatasan/main.go

## Purpose

Thin app-specific entrypoint that delegates startup to the shared runtime host.

## Key Functions

- `main`: runs `infra/apphost.Run` with the `mymatasan` app module.
- `HealthCheckHandler`: retained only for existing integration tests.
- `ReadinessCheckHandler`: retained only for existing integration tests.

## Notes

- Runtime orchestration now lives in `infra/apphost`.
- App-specific route wiring and entity/seed registration now live in `apps/mymatasan/app/app.go`.
