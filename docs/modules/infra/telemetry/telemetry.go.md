# Module: infra/telemetry/telemetry.go

## Purpose

Defines shared telemetry contracts used by runtime modules.

## Responsibilities

- Defines `APIRequestMetric` for completed API request observations.
- Defines `CoordinationMetric` for transaction lock/queue observations.
- Defines `APIRecorder` so telemetry implementations stay interchangeable.
- Defines `CoordinationRecorder` and combined `Recorder` for shared runtime telemetry.
- Provides a no-op recorder for disabled telemetry.

## Notes

- Shared middleware and transaction coordination depend on these interfaces, not on a specific telemetry backend.
