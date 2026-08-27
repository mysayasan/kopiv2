package services

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// myiotsan's runtime metrics.
//
// The rule for what belongs here: instrument what FAILS SILENTLY. A metric that duplicates a
// log line an operator would read anyway is noise. A metric for a failure that has no other
// symptom is the whole point — and a sensor hub has several of those.
//
// The ingest pipeline is deliberately arranged so a publish never touches the database, so its
// failures never raise an error a human sees. A reading that was DROPPED because the disk could
// not keep up, a payload that could not be DECODED because a device sent malformed JSON, a
// deadband that has stopped suppressing — none of these logs anything at a rate anyone watches.
// They are exactly the things a scrape should show.
const (
	// MetricIngestReceived counts payloads accepted from devices. The denominator for
	// everything else.
	MetricIngestReceived = "myiotsan_ingest_received_total"
	// MetricIngestStored counts readings actually written to the database.
	MetricIngestStored = "myiotsan_ingest_stored_total"
	// MetricIngestSuppressed counts readings the deadband dropped as unchanged. The ratio of
	// suppressed to (stored+suppressed) IS the storage design working — it should sit high (90%+
	// on real building sensors). If it falls toward zero, a deadband has been mistuned and the
	// database is about to be in trouble.
	MetricIngestSuppressed = "myiotsan_ingest_suppressed_total"
	// MetricIngestDropped counts readings SHED because the write queue was full. This is the one
	// that matters most: a non-zero, rising value means ingest has outrun the disk and telemetry
	// is being lost. It has no other symptom — the broker keeps accepting publishes, the UI keeps
	// rendering — so without this counter, silent data loss.
	MetricIngestDropped = "myiotsan_ingest_dropped_total"
	// MetricIngestQueueDepth is the current write-queue depth. A queue that is climbing toward its
	// cap is the leading indicator of drops; watching it lets an operator act before readings are
	// lost rather than after.
	MetricIngestQueueDepth = "myiotsan_ingest_queue_depth"
	// MetricIngestSeries is how many (device, key) series the deadband gate is tracking — the one
	// unbounded structure in the ingest path.
	MetricIngestSeries = "myiotsan_ingest_series"

	// MetricDevicesOnline / MetricDevicesOffline is the fleet's health at a glance. Offline is the
	// one to alert on: a sensor that has gone silent is a monitoring blind spot, and a smoke
	// detector that has gone silent is worse.
	MetricDevicesOnline  = "myiotsan_devices_online"
	MetricDevicesOffline = "myiotsan_devices_offline"

	// MetricCommandsTotal counts actuation commands by outcome (confirmed | failed | refused).
	// A rising `failed` is devices not acting; a rising `refused` is somebody repeatedly trying
	// something they are not allowed to — both are worth seeing.
	MetricCommandsTotal = "myiotsan_commands_total"

	// MetricFlowEventsDropped counts telemetry events the flow runtime SHED because its queue
	// overflowed. The same argument as MetricIngestDropped, one layer up and even quieter: when
	// the flow worker cannot keep up, the readings still arrive and the charts still draw, and the
	// only thing missing is the automation on top of them. Nothing else anywhere goes wrong.
	MetricFlowEventsDropped = "myiotsan_flow_events_dropped_total"
	// MetricFlowQueueDepth is the runtime's current backlog — the leading indicator of the above.
	MetricFlowQueueDepth = "myiotsan_flow_queue_depth"
	// MetricFlowsStopped counts enabled flows that are NOT running: one that would not compile, or
	// one quarantined for running away. An operator who drew a flow believes it is armed.
	MetricFlowsStopped = "myiotsan_flows_stopped"
)

// DescribeMetrics attaches help text. Call once at startup.
func DescribeMetrics(m telemetry.Metrics) {
	if m == nil {
		return
	}
	m.Describe(MetricIngestReceived, "Payloads accepted from devices.")
	m.Describe(MetricIngestStored, "Readings written to the database (passed the deadband).")
	m.Describe(MetricIngestSuppressed, "Readings dropped by the deadband as unchanged. A high ratio is the storage design working.")
	m.Describe(MetricIngestDropped, "Readings SHED because the write queue was full — ingest outran the disk. Silent data loss; alert on any increase.")
	m.Describe(MetricIngestQueueDepth, "Current write-queue depth. Climbing toward the cap is the leading indicator of drops.")
	m.Describe(MetricIngestSeries, "Distinct (device,key) series the deadband gate is tracking.")
	m.Describe(MetricDevicesOnline, "Devices whose last reading is recent.")
	m.Describe(MetricDevicesOffline, "Devices that have gone silent — a monitoring blind spot.")
	m.Describe(MetricCommandsTotal, "Actuation commands by outcome (confirmed|failed|refused).")
	m.Describe(MetricFlowEventsDropped, "Telemetry events SHED by the flow runtime — automation silently stopped firing. Alert on any increase.")
	m.Describe(MetricFlowQueueDepth, "Current flow-runtime backlog. Climbing toward the cap is the leading indicator of shed events.")
	m.Describe(MetricFlowsStopped, "Enabled flows that are not running (would not compile, or quarantined for running away).")
}

// statSource is the narrow slice of Ingest the sampler needs — one method. A consumer-defined
// interface so the sampler does not drag in the whole ingest pipeline.
type statSource interface {
	Stats() IngestStats
}

// flowStatSource is the same narrow shape for the flow runtime. Nil is allowed: an install can be
// wired without flows, and a sampler that panicked on that would be worse than a missing gauge.
type flowStatSource interface {
	Stats() FlowStats
}

// RunMetricsSampler periodically reads the ingest counters and device health into Prometheus.
//
// It SAMPLES rather than instrumenting the hot path directly, and that is deliberate. A publish
// arrives thousands of times a second; taking a metrics lock on each one would put contention on
// the exact path the whole app is arranged to keep fast. Ingest already keeps its own cheap
// atomic counters (IngestStats), so the sampler just copies them out on a ticker — the cost is
// one read per interval, not one write per reading.
//
// Counters are exposed as gauges of the running total, which Prometheus turns back into a rate
// with `rate()`. That loses nothing and keeps the sampler a plain copy.
func RunMetricsSampler(ctx context.Context, m telemetry.Metrics, stats statSource, flows flowStatSource, devices *DeviceService, interval time.Duration) {
	if m == nil || stats == nil {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	safego.Supervise(ctx, "myiotsan.metrics-sampler", func(ctx context.Context) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sampleOnce(ctx, m, stats, flows, devices)
			}
		}
	})
}

func sampleOnce(ctx context.Context, m telemetry.Metrics, stats statSource, flows flowStatSource, devices *DeviceService) {
	s := stats.Stats()
	m.Set(MetricIngestReceived, nil, float64(s.Received))
	m.Set(MetricIngestStored, nil, float64(s.Stored))
	m.Set(MetricIngestSuppressed, nil, float64(s.Suppressed))
	m.Set(MetricIngestDropped, nil, float64(s.Dropped))
	m.Set(MetricIngestQueueDepth, nil, float64(s.Queued))
	m.Set(MetricIngestSeries, nil, float64(s.Series))

	if flows != nil {
		f := flows.Stats()
		m.Set(MetricFlowEventsDropped, nil, float64(f.Dropped))
		m.Set(MetricFlowQueueDepth, nil, float64(f.Queued))
		m.Set(MetricFlowsStopped, nil, float64(f.Broken+f.Quarantined))
	}

	if devices == nil {
		return
	}
	rows, _, err := devices.List(ctx, 1000, 0)
	if err != nil {
		return
	}
	online, offline := 0, 0
	for _, d := range rows {
		if !d.Enabled {
			continue
		}
		if d.Health == "offline" {
			offline++
		} else {
			online++
		}
	}
	m.Set(MetricDevicesOnline, nil, float64(online))
	m.Set(MetricDevicesOffline, nil, float64(offline))
}
