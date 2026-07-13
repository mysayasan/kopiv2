# Module: infra/telemetry/prometheus/metrics.go

## Purpose

Implements `telemetry.Metrics` (the app-defined metric registry) on the existing Prometheus `Recorder`, so any app-defined counter, gauge, or histogram is emitted in the same scrape as the shared `kopiv2_api_*`/`kopiv2_tx_*` series, alongside arbitrary label sets.

## Responsibilities

- `Describe(name, help)` — attaches help text to a metric name. Safe to call before or after the metric is first observed; a described-but-never-observed metric is not emitted at all (no placeholder lines).
- `Inc`/`Add` — counter operations.
- `Set` — gauge operation.
- `Observe` — histogram operation; the value is expected in milliseconds, reusing the recorder's existing bucket set (`10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000`).
- `withSeries` resolves (creating if needed) the series for `name`+`labels` and applies the mutation under `r.mu`.
- `collectCustom(b *strings.Builder)` — appends every app-defined metric to the scrape in Prometheus text format, called from `Recorder.Collect()`. Metric names and series keys are sorted so scrape output is deterministic.

## Key design points

- **Kind binding.** A metric name is bound to the kind (`counter`/`gauge`/`histogram`) it was first observed with. A later observation of the same name with a different kind is silently dropped rather than corrupting the exposition — a name cannot be both a counter and a gauge in Prometheus.
- **Cardinality cap** (`maxSeriesPerMetric = 500`). Every series lives in memory for the life of the process, so an accidentally unbounded label (an error string, a file path) would be a slow memory leak on a box meant to run for months. Once a metric name hits the cap, new label combinations are dropped and a `<name>_series_truncated 1` gauge is emitted alongside it — truncation is made visible in the scrape, since a silently short metric would be worse than a loud one.
- **Labels are copied on first observation** (`cloneLabels`) — callers are expected to reuse label maps across calls, so the recorder must never alias one.
- **Series identity is order-independent** — `seriesKey` sorts label names before hashing, so `{a:1,b:2}` and `{b:2,a:1}` resolve to the same series.
- Histogram series track `buckets`, `count`, and `sum`, and are rendered with `_bucket{...,le="..."}`, `_sum`, and `_count` lines, matching standard Prometheus histogram exposition.

## Notes

- This is the backend for `infra/telemetry.Metrics` — see `infra/telemetry/telemetry.go.md` for the interface contract and label-cardinality guidance callers must follow.
- Naming convention observed by callers (not enforced here): `kopiv2_*` for metrics emitted by shared infra (app-neutral — `infra/recording`, `infra/notification`), app-specific prefixes (e.g. `mymatasan_*`) for metrics owned by one app (see `apps/mymatasan/services/metrics.go.md`).
- All exported methods are nil-receiver-safe (`if r == nil { return }`), so a caller holding a possibly-unset `*Recorder` doesn't need to guard every call site.
- Covered by `infra/telemetry/prometheus/metrics_test.go`: counter/gauge/histogram exposition, kind-conflict dropping, cardinality cap + truncation visibility, label-order independence, label copy-on-write, and concurrent-observation safety.
