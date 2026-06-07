# Module: infra/telemetry/prometheus/prometheus.go

## Purpose

Provides the first telemetry backend: a Prometheus text exporter.

## Metrics

- `kopiv2_api_requests_total`
- `kopiv2_api_request_duration_ms`
- `kopiv2_api_slow_requests_total`
- `kopiv2_api_slow_request_duration_ms`
- `kopiv2_tx_lock_events_total`
- `kopiv2_tx_lock_wait_ms`
- `kopiv2_tx_lock_stuck_total`

## Labels

- `app`
- `method`
- `path`
- `status`
- transaction lock labels: `app`, `provider`, `resource`, `outcome`

## Notes

- The `path` label uses Gorilla Mux route templates when available to avoid high-cardinality raw IDs.
- Slow request metrics use the configured `apiDurationThresholdMs`.
- Transaction lock metrics use low-cardinality resource labels such as `file-storage`.
- Stuck lock metrics increment when a lock is held longer than the configured transaction stuck timeout.
- The exporter is implemented with the Go standard library and does not require an external Prometheus client dependency.
