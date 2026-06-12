# Module: infra/coordination/locker.go

## Purpose

Defines transaction coordination contracts shared by memory and Redis lock providers.

## Responsibilities

- Defines `Locker` for FIFO resource lock acquisition.
- Defines `Lock` for owner-token release.
- Defines `Ping` lifecycle checks so startup can fail fast when the configured provider is unavailable.
- Defines shared timing/config values for wait timeout, lease, stuck timeout, and Redis connection settings.
- Emits shared coordination telemetry observations through the telemetry recorder interface.

## Notes

- The coordinator serializes critical app-level work; it does not replace request-scoped DB transactions.
- Resource labels must stay low-cardinality for metrics.
