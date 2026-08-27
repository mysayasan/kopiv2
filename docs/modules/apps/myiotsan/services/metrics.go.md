# Module: apps/myiotsan/services/metrics.go

## Purpose

myiotsan's runtime metrics catalog and sampler. The rule for what belongs here: **instrument
what FAILS SILENTLY.** A metric that duplicates a log line an operator would read anyway is
noise; a metric for a failure mode with no other symptom is the whole point — and a sensor hub
has several of those, because the ingest pipeline is deliberately arranged so a publish never
touches the database and so never raises an error a human sees.

## Metrics

- `MetricIngestReceived` = `myiotsan_ingest_received_total` — payloads accepted from devices; the
  denominator for everything else.
- `MetricIngestStored` = `myiotsan_ingest_stored_total` — readings actually written (passed the
  deadband).
- `MetricIngestSuppressed` = `myiotsan_ingest_suppressed_total` — readings the deadband dropped as
  unchanged. Suppressed:(stored+suppressed) is the storage design working and should sit high
  (90%+ on real building sensors); falling toward zero means a deadband has been mistuned and the
  database is about to be in trouble.
- `MetricIngestDropped` = `myiotsan_ingest_dropped_total` — **the headline metric.** Readings shed
  because the write queue was full — ingest outran the disk. Silent data loss: the broker keeps
  accepting publishes and the UI keeps rendering, so without this counter a torrent that outran
  the disk leaves no other trace. Verified live: a torrent that outran the disk produced
  `dropped_total 86`. Alert on any increase.
- `MetricIngestQueueDepth` = `myiotsan_ingest_queue_depth` — current write-queue depth; the
  leading indicator of drops, letting an operator act before readings are actually lost.
- `MetricIngestSeries` = `myiotsan_ingest_series` — distinct `(device, key)` series the deadband
  gate is tracking, the one unbounded structure in the ingest path.
- `MetricDevicesOnline` / `MetricDevicesOffline` = `myiotsan_devices_online` /
  `myiotsan_devices_offline` — fleet health at a glance. Offline is the one to alert on: a sensor
  gone silent is a monitoring blind spot, and a smoke detector gone silent is worse.
- `MetricCommandsTotal` = `myiotsan_commands_total` ({outcome=confirmed|failed|refused}) —
  actuation command outcomes, incremented directly by `services/commands.go`'s `countCommand` (not
  sampled — commands are rare). A rising `failed` is devices not acting; a rising `refused` is
  somebody repeatedly trying something they are not allowed to.
- `MetricFlowEventsDropped` = `myiotsan_flow_events_dropped_total` — telemetry events the flow
  runtime SHED because its worker queue overflowed. The same silent-failure shape as
  `MetricIngestDropped`, one layer up: the reading still lands and the chart still draws, and only
  the automation on top of it quietly stops firing. Alert on any increase.
- `MetricFlowQueueDepth` = `myiotsan_flow_queue_depth` — the flow runtime's current backlog, the
  leading indicator of the above.
- `MetricFlowsStopped` = `myiotsan_flows_stopped` — enabled flows that are NOT running: one that
  would not compile, plus one quarantined for running away (`services/flow_runtime.go.md`'s
  `FlowStats.Broken + FlowStats.Quarantined`). An operator who drew a flow believes it is armed.
- `DescribeMetrics(m telemetry.Metrics)` registers help text for all twelve; called once at startup
  (`app.go`).

## Sampling, not instrumenting, the ingest and flow gauges

`RunMetricsSampler(ctx, m, stats statSource, flows flowStatSource, devices *DeviceService,
interval)` reads `ingest`'s own atomic counters (`IngestStats`), the flow runtime's own counters
(`FlowStats`), and device health off a `10s` ticker (`safego.Supervise`d as
`myiotsan.metrics-sampler`) and copies them into gauges — it does **not** instrument the publish
path directly. That path arrives thousands of times a second; taking a metrics lock on each one
would put contention on the exact path the whole app is arranged to keep fast. The counters are
exposed as gauges of the running total, which Prometheus's `rate()` turns back into a rate — this
loses nothing and keeps the sampler a plain copy, one read per interval instead of one write per
reading.

`statSource` is a one-method consumer-defined interface (`Stats() IngestStats`) so the sampler
does not have to import the whole ingest pipeline; `flowStatSource` is the same shape for the flow
runtime (`Stats() FlowStats`) and is explicitly allowed to be `nil` — an install can be wired
without the flow engine, and a sampler that panicked on a nil dependency would be worse than a
missing gauge. Device online/offline counts are derived from `DeviceService.List` filtered to
enabled devices, bucketed on `Health == "offline"`.

## Notes

- Wired from `app.go`'s `RegisterAppRoutes`: `services.DescribeMetrics(deps.Metrics)` then
  `services.RunMetricsSampler(bgCtx, deps.Metrics, ingest, flowRuntime, deviceService,
  10*time.Second)`. This call now happens right after the Flow Engine is wired (step 10e), not
  immediately after the ingest spine as before, because the flow gauges need `flowRuntime` to
  exist first. See `app/app.go.md`.
- `deps.Metrics` is never nil (apphost falls back to a no-op recorder when telemetry is
  disabled — `infra/apphost/run.go.md`), but `DescribeMetrics`/`RunMetricsSampler` both nil-guard
  anyway so the package works when constructed directly (tests).
- Before this file, myiotsan exposed zero series on `/metrics`; a live scrape confirmed it.
