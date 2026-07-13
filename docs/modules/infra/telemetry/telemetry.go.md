# Module: infra/telemetry/telemetry.go

## Purpose

Defines shared telemetry contracts used by runtime modules.

## Responsibilities

- Defines `APIRequestMetric` for completed API request observations.
- Defines `CoordinationMetric` for transaction lock/queue observations.
- Defines `APIRecorder` so telemetry implementations stay interchangeable.
- Defines `CoordinationRecorder` and combined `Recorder` for shared runtime telemetry.
- Defines `Labels` (`map[string]string`) and the `Metrics` interface (`Describe`/`Inc`/`Add`/`Set`/`Observe`) so any app — not just the shared API/coordination middleware — can record its own counters, gauges, and millisecond histograms without depending on the Prometheus backend directly. `Recorder` embeds `Metrics`.
- Provides a no-op recorder for disabled telemetry; `noopRecorder` also implements `Metrics`, so instrumentation call sites never need a nil check when telemetry is off.

## Notes

- Shared middleware and transaction coordination depend on these interfaces, not on a specific telemetry backend.
- `Metrics` implementations must be safe for concurrent use and must never block — call sites include per-frame, per-segment, and per-delivery hot paths.
- `Labels` values must stay bounded (camera ids, small outcome enums) — an error string, file path, or timestamp used as a label value is a slow memory leak, since every distinct combination is a series held for the life of the process. See `infra/telemetry/prometheus/metrics.go.md` for the cardinality cap enforced by the Prometheus backend.
