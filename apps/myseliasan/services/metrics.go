package services

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// myseliasan's runtime metrics.
//
// A control plane is a RELAY: it holds no cameras and no sensors of its own, so its failures are
// failures of the FLEET, and they are the quietest kind. A node drops off the control channel and
// the UI simply shows one fewer node — which looks identical to a node an operator released on
// purpose. A node's certificate creeps toward expiry with no symptom at all until the day it
// expires and the node cannot re-enroll. These are exactly the things a scrape should surface,
// because the app itself keeps answering perfectly while the fleet quietly degrades.
const (
	// MetricNodesConnected is how many nodes currently hold a live control channel. The number an
	// operator cares about most: it is the fleet that is actually reachable RIGHT NOW, as distinct
	// from the fleet that has been adopted.
	MetricNodesConnected = "myseliasan_nodes_connected"
	// MetricNodesAdopted is how many nodes have been adopted in total. The gap between adopted and
	// connected is the fleet that is supposed to be there and is not.
	MetricNodesAdopted = "myseliasan_nodes_adopted"
	// MetricControlChannelUp is 1 while the control-channel listener is serving. If this is 0, NO
	// node can reach the control plane, and every node in the fleet is about to look lost — so
	// this is the first thing to check when the whole fleet appears to vanish at once.
	MetricControlChannelUp = "myseliasan_control_channel_up"
	// MetricFleetEventsTotal counts fleet-health transitions by kind (node_lost | node_recovered |
	// cert_expiring). A burst of node_lost is a network partition or the control channel dying; a
	// steady trickle of cert_expiring is enrollment quietly failing across the fleet.
	MetricFleetEventsTotal = "myseliasan_fleet_events_total"
	// MetricFleetRuleFiredTotal counts cross-domain correlation rules firing. Low-volume by
	// nature (an intrusion is rare); a SPIKE is either a real incident or a rule mistuned into
	// crying wolf, and both are worth seeing.
	MetricFleetRuleFiredTotal = "myseliasan_fleet_rule_fired_total"
)

// DescribeMyseliasanMetrics attaches help text. Call once at startup.
func DescribeMyseliasanMetrics(m telemetry.Metrics) {
	if m == nil {
		return
	}
	m.Describe(MetricNodesConnected, "Nodes currently holding a live control channel — the fleet reachable right now.")
	m.Describe(MetricNodesAdopted, "Nodes adopted in total. The gap to connected is the fleet that should be here and is not.")
	m.Describe(MetricControlChannelUp, "1 while the control-channel listener is serving. 0 means no node can reach the control plane.")
	m.Describe(MetricFleetEventsTotal, "Fleet-health transitions by kind (node_lost|node_recovered|cert_expiring).")
	m.Describe(MetricFleetRuleFiredTotal, "Cross-domain correlation rules firing. A spike is a real incident or a mistuned rule.")
}

// connectionSource is the narrow slice of the control server the sampler reads.
type connectionSource interface {
	IsListening() bool
	ConnectedCount() int
}

// RunFleetMetricsSampler periodically reads fleet health into Prometheus.
//
// Sampled, not instrumented per-connection: connections come and go, but a scrape every ten
// seconds is plenty to watch a fleet, and it keeps the control-channel accept path free of a
// metrics lock. adoptedCount is a func rather than an interface so the caller can pass the
// registry's List directly without a shim.
func RunFleetMetricsSampler(
	ctx context.Context,
	m telemetry.Metrics,
	conns connectionSource,
	adoptedCount func(ctx context.Context) int,
	interval time.Duration,
) {
	if m == nil || conns == nil {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	safego.Supervise(ctx, "myseliasan.metrics-sampler", func(ctx context.Context) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				up := 0.0
				if conns.IsListening() {
					up = 1
				}
				m.Set(MetricControlChannelUp, nil, up)
				m.Set(MetricNodesConnected, nil, float64(conns.ConnectedCount()))
				if adoptedCount != nil {
					m.Set(MetricNodesAdopted, nil, float64(adoptedCount(ctx)))
				}
			}
		}
	})
}
