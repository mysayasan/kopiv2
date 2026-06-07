# Module: infra/telemetry/telemetry.go

## Purpose

Defines shared telemetry contracts used by runtime modules.

## Responsibilities

- Defines `APIRequestMetric` for completed API request observations.
- Defines `APIRecorder` so telemetry implementations stay interchangeable.
- Provides a no-op recorder for disabled telemetry.

## Notes

- Shared middleware depends on this interface, not on a specific telemetry backend.
