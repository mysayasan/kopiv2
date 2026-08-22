package entities

// Node liveness states, as recorded in history. These are the SAME strings
// ManagedNode.Status carries — history is a log of that field's transitions, not a
// parallel vocabulary, so a state that appears on the Nodes page and a state that
// appears in an SLA report can never mean two different things.
const (
	// NodeStateOnline — the node had a live control channel or answered an mTLS probe.
	NodeStateOnline = "online"
	// NodeStateLost — no contact on either path for longer than the grace window.
	NodeStateLost = "lost"
	// NodeStateSelfDropped — the node unpaired itself. It is no longer part of the
	// fleet, so this is neither uptime nor downtime; see NodeStateEvent.
	NodeStateSelfDropped = "self-dropped"
)

// Reasons a state was recorded. Carried for diagnosis only — availability maths never
// branches on them — so a reason this build does not know is harmless.
const (
	// NodeStateReasonBaseline is the first observation of a node that has no history
	// yet: an upgrade of an existing fleet, or the moment right after adoption.
	// Availability is only computed from a node's first recorded event onward, so this
	// row is what starts the clock — it never back-dates a claim about the past.
	NodeStateReasonBaseline = "baseline"
	// NodeStateReasonHeartbeat is the periodic liveness reconciliation.
	NodeStateReasonHeartbeat = "heartbeat"
	// NodeStateReasonControlChannel is a node dialing in on the control channel, which
	// proves liveness between two heartbeat sweeps.
	NodeStateReasonControlChannel = "control-channel"
	// NodeStateReasonEnroll is a certificate enrollment or renewal.
	NodeStateReasonEnroll = "enroll"
	// NodeStateReasonAdopt is the adoption that put the node in the fleet.
	NodeStateReasonAdopt = "adopt"
	// NodeStateReasonSelfDrop is the node's authenticated self-drop notice.
	NodeStateReasonSelfDrop = "self-drop"
)

// NodeStateEvent is one recorded TRANSITION of a node's liveness — the fleet's memory
// of what happened, as opposed to ManagedNode.Status, which only remembers what is
// happening now.
//
// Why this table exists: the control plane could always answer "is this node up?" and
// could never answer "was it up last month?". A customer with an SLA does not ask the
// first question. `services/reports.go` said so in its own footnote ("historical uptime
// is not yet tracked"), and the only other record of an outage was a notification —
// which is retained on a rolling window, deduplicated, and edge-triggered, so it is a
// record of ALERTS, not of STATE, and cannot be summed into an availability figure.
//
// Append-only, one row per CHANGE. A node that is up for a year writes one row, so the
// table stays small on a healthy fleet and only grows where there is something to
// report. Rows are written by the heartbeat reconciler (the single leader-elected
// writer of node status) and by the three paths that legitimately change status between
// sweeps: adoption, control-channel accept, and the self-drop notice.
//
// NodeId, not the numeric Id, is the key — it is what survives a backup/restore, and it
// is what the node itself asserts on the wire. The corollary is that RELEASING a node
// must delete its history (see INodeStateHistory.Forget): the same appliance re-adopted
// later reuses its NodeId, and a stale "lost" span would otherwise flow straight into
// the new adoption and report an outage that never happened.
type NodeStateEvent struct {
	Id     int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	NodeId string `json:"nodeId" form:"nodeId" query:"nodeId" validate:"required"`
	// State is one of the NodeState* values — what the node became at At.
	State string `json:"state" form:"state" query:"state"`
	// PrevState is what it was before, empty for a baseline row. Recorded so a reader
	// can see a transition without having to fetch the neighbouring row.
	PrevState string `json:"prevState" form:"prevState" query:"prevState"`
	// At is the unix second the state BEGAN, which is not always the second it was
	// noticed.
	//
	// It matters most for "lost". The grace window means the sweep that declares a node
	// lost runs up to three heartbeat intervals (at least 90 seconds) after the node
	// actually went quiet, so stamping the transition with the sweep clock would discard
	// that interval from every single outage — a published availability figure
	// under-stating downtime by a fixed amount per incident, always in the vendor's
	// favour. The reconciler therefore dates a lost transition to ManagedNode.LastSeenAt,
	// the last moment there WAS contact. That is safe because LastSeenAt is stamped by
	// the control plane's own clock on every path that sets it — never by the node — so
	// there is no remote skew to import. Every other transition is dated as observed,
	// because for those the observation IS the event.
	At int64 `json:"at" form:"at" query:"at"`
	// Reason is one of the NodeStateReason* values: which path observed this.
	Reason string `json:"reason" form:"reason" query:"reason"`
}
