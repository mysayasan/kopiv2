# Module: infra/telemetry/prometheus/prometheus.go

## Purpose

Provides the first telemetry backend: a Prometheus text exporter.

## Metrics

- `kopiv2_api_requests_total`
- `kopiv2_api_request_duration_ms`
- `kopiv2_api_slow_requests_total`
- `kopiv2_api_slow_request_duration_ms`

## Labels

- `app`
- `method`
- `path`
- `status`

## Notes

- The `path` label uses Gorilla Mux route templates when available to avoid high-cardinality raw IDs.
- Slow request metrics use the configured `apiDurationThresholdMs`.
- The exporter is implemented with the Go standard library and does not require an external Prometheus client dependency.
