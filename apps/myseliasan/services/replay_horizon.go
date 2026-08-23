package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

// The replay horizon (W2-6, part of finding F-11).
//
// A node's events are delivered LIVE up the control channel. When the channel is down they
// are not queued anywhere — they are recovered on reconnect by replaying the node's stored
// notifications from the last `window`. That works, and it is the right design: no queue on
// the appliance, no new disk pressure, nothing to corrupt.
//
// But it is a promise with an expiry date, and nothing was watching the clock. Past the
// replay window a disconnect stops being recoverable: the reconnect asks for the last 72
// hours and everything older is simply never fetched. The fleet screens go on saying the
// node is "lost" in exactly the same tone at hour 2 and at hour 90, and the moment the
// guarantee lapsed passes unremarked.
//
// So this watches for it, and warns BEFORE rather than after. A warning that arrives once
// the events are already unrecoverable is an obituary, not an alert.

const (
	// replayWarnFraction is how far into the window a disconnect gets before the first
	// warning. Two-thirds leaves a real window to act in — on a 72h replay that is a full
	// day of notice — while being late enough that a node down over a weekend does not
	// page anyone on Friday evening.
	replayWarnFraction = 2.0 / 3.0
	// replaySweepInterval is how often the horizon is checked. Minutes matter nowhere
	// here: the thing being watched moves over days.
	replaySweepInterval = 15 * time.Minute
)

// ReplayHorizonState is what a node's disconnect looks like against the replay window.
const (
	// ReplayHorizonOk means the node is connected, or has been away for less than the
	// warn fraction of the window.
	ReplayHorizonOk = "ok"
	// ReplayHorizonApproaching means the disconnect is far enough into the window that
	// events will soon start falling out of reach.
	ReplayHorizonApproaching = "approaching"
	// ReplayHorizonLapsed means the disconnect is longer than the window: whatever the
	// node raised before the horizon can no longer be replayed, ever.
	ReplayHorizonLapsed = "lapsed"
)

// NodeReplayHorizon is one node's position against the window.
type NodeReplayHorizon struct {
	NodeId   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	SiteId   int64  `json:"siteId,omitempty"`
	State    string `json:"state"`
	// AwaySeconds is how long the node has been out of contact (0 when connected).
	AwaySeconds int64 `json:"awaySeconds"`
	// WindowSeconds is the replay window this is judged against, so a reader never has to
	// guess which number the state was derived from.
	WindowSeconds int64 `json:"windowSeconds"`
	// UnrecoverableBefore is the moment beyond which this node's events can no longer be
	// replayed — set only when lapsed. It is an absolute timestamp on purpose: "events
	// before 03:14 on Tuesday are gone" is actionable, "72 hours" is arithmetic homework.
	UnrecoverableBefore int64 `json:"unrecoverableBefore,omitempty"`
}

// ReplayHorizonReport is one sweep.
type ReplayHorizonReport struct {
	Nodes       []NodeReplayHorizon `json:"nodes"`
	CheckedAt   int64               `json:"checkedAt"`
	Approaching int                 `json:"approaching"`
	Lapsed      int                 `json:"lapsed"`
}

// replayHorizonNodeLister is the slice of the registry this needs.
type replayHorizonNodeLister interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

// ReplayHorizonMonitor warns as a node's disconnect approaches — and then passes — the
// point where its missed events can no longer be replayed.
type ReplayHorizonMonitor struct {
	nodes     replayHorizonNodeLister
	connected func(nodeID string) bool
	notify    func(nodeID, nodeName, state, detail string)
	window    time.Duration
	now       func() time.Time

	mu   sync.Mutex
	last ReplayHorizonReport
	// warned remembers the state each node was last warned about, so a node sitting in
	// "approaching" for a day produces ONE warning rather than one every sweep. A monitor
	// that repeats itself every fifteen minutes trains people to filter it out, and then
	// the escalation to "lapsed" is filtered out with it.
	warned map[string]string
}

// NewReplayHorizonMonitor builds the monitor. window is the control plane's reconnect
// replay window; notify may be nil (then the monitor only reports).
func NewReplayHorizonMonitor(nodes replayHorizonNodeLister, connected func(string) bool, window time.Duration, notify func(nodeID, nodeName, state, detail string)) *ReplayHorizonMonitor {
	return &ReplayHorizonMonitor{
		nodes:     nodes,
		connected: connected,
		notify:    notify,
		window:    window,
		now:       time.Now,
		warned:    map[string]string{},
	}
}

// Last returns the most recent sweep, for the API to serve without re-walking the fleet.
func (m *ReplayHorizonMonitor) Last() ReplayHorizonReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

// Sweep evaluates every node once and raises a notification for each node that has newly
// entered a worse state.
func (m *ReplayHorizonMonitor) Sweep(ctx context.Context) (ReplayHorizonReport, error) {
	nodes, err := m.nodes.List(ctx)
	if err != nil {
		return ReplayHorizonReport{}, err
	}
	now := m.now().UTC().Unix()
	windowSeconds := int64(m.window / time.Second)
	warnAfter := int64(float64(windowSeconds) * replayWarnFraction)

	report := ReplayHorizonReport{CheckedAt: now, Nodes: []NodeReplayHorizon{}}
	seen := map[string]bool{}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		seen[node.NodeId] = true
		row := NodeReplayHorizon{
			NodeId:        node.NodeId,
			NodeName:      nodeLabel(node),
			SiteId:        node.SiteId,
			State:         ReplayHorizonOk,
			WindowSeconds: windowSeconds,
		}

		// A connected node has nothing to recover: it is forwarding live right now.
		connected := m.connected == nil || m.connected(node.NodeId)
		switch {
		case connected:
		case node.LastSeenAt <= 0:
			// Never seen. There is no clock to measure against, and inventing one would
			// either cry wolf on a node adopted a minute ago or stay silent forever.
			// Left ok deliberately; a node that never connects is the liveness monitor's
			// business, not this one's.
		default:
			row.AwaySeconds = now - node.LastSeenAt
			if row.AwaySeconds < 0 {
				row.AwaySeconds = 0
			}
			switch {
			case row.AwaySeconds >= windowSeconds:
				row.State = ReplayHorizonLapsed
				// Everything the node raised before this instant is out of reach: the
				// reconnect will ask for the last window only.
				row.UnrecoverableBefore = now - windowSeconds
			case row.AwaySeconds >= warnAfter:
				row.State = ReplayHorizonApproaching
			}
		}

		switch row.State {
		case ReplayHorizonApproaching:
			report.Approaching++
		case ReplayHorizonLapsed:
			report.Lapsed++
		}
		report.Nodes = append(report.Nodes, row)
		m.raise(row)
	}

	// Forget nodes that are gone (released, or recovered), so a node that comes back and
	// later goes away again is warned about afresh rather than silently.
	m.mu.Lock()
	for id := range m.warned {
		if !seen[id] {
			delete(m.warned, id)
		}
	}
	m.last = report
	m.mu.Unlock()
	return report, nil
}

// raise notifies once per state TRANSITION, never once per sweep.
func (m *ReplayHorizonMonitor) raise(row NodeReplayHorizon) {
	m.mu.Lock()
	previous := m.warned[row.NodeId]
	if row.State == ReplayHorizonOk {
		delete(m.warned, row.NodeId)
	} else {
		m.warned[row.NodeId] = row.State
	}
	m.mu.Unlock()

	if row.State == ReplayHorizonOk || row.State == previous || m.notify == nil {
		return
	}
	hours := row.AwaySeconds / 3600
	windowHours := row.WindowSeconds / 3600
	detail := fmt.Sprintf("%s has been out of contact for %dh. Events it raises are recovered on reconnect from the last %dh only.",
		row.NodeName, hours, windowHours)
	if row.State == ReplayHorizonLapsed {
		detail = fmt.Sprintf("%s has been out of contact for %dh, longer than the %dh replay window. Anything it raised before %s can no longer be recovered.",
			row.NodeName, hours, windowHours,
			time.Unix(row.UnrecoverableBefore, 0).UTC().Format("2006-01-02 15:04 UTC"))
	}
	m.notify(row.NodeId, row.NodeName, row.State, detail)
}

// RegisterScheduler starts the periodic sweep.
func (m *ReplayHorizonMonitor) RegisterScheduler(start func(name string, interval time.Duration, run func(context.Context) error)) {
	if start == nil {
		return
	}
	start("replay-horizon", replaySweepInterval, func(ctx context.Context) error {
		_, err := m.Sweep(ctx)
		return err
	})
}
