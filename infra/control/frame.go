// Package control implements the persistent, node-initiated control channel
// between the myseliasan control plane (parent) and an adopted mymatasan node.
// It is a thin transport: a single long-lived WebSocket carried over the fleet-CA
// mTLS trust established at adoption, multiplexing JSON frames in both directions.
//
// Direction is deliberately node→parent for the dial: a node sits behind NAT /
// firewalls and may be re-IP'd, so a node-dialed connection has one consistent
// direction to secure, survives address changes, and gives the parent an instant
// liveness signal (socket drop = node lost) on top of the existing heartbeat poll.
//
// The frame vocabulary already carries the Phase-2 request/response tunnel fields
// (Method/Path/Role/Actor/Status) so adding the command tunnel needs no wire
// change — Phase 1 uses only Hello + Event plus WebSocket ping/pong keepalive.
package control

// FrameType discriminates the multiplexed messages on the channel.
type FrameType string

const (
	// FrameHello is the node's first frame after connecting (identity + version).
	FrameHello FrameType = "hello"
	// FrameEvent is an unsolicited node→parent push (alert / health / status).
	FrameEvent FrameType = "event"
	// FrameReq is a parent→node tunneled HTTP request (Phase 2).
	FrameReq FrameType = "req"
	// FrameRes is the node's reply to a FrameReq, correlated by ID (Phase 2).
	FrameRes FrameType = "res"
)

// Frame is one message on the control channel. Fields are grouped by frame type;
// unused fields are omitted on the wire.
type Frame struct {
	Type FrameType `json:"type"`
	// ID correlates a FrameReq with its FrameRes.
	ID string `json:"id,omitempty"`

	// Hello.
	NodeID  string `json:"nodeId,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	// Dropped is how many events the node failed to forward while it was DISCONNECTED —
	// counted since its last successful hello, not since boot, because "what did I lose
	// while I was away" is the number the control plane can act on. It is the node's own
	// admission of loss: the reconnect replay is expected to recover these from the node's
	// stored notifications, and this is what makes that expectation checkable rather than
	// assumed. Zero (and so omitted) on a normal reconnect that lost nothing.
	Dropped int64 `json:"dropped,omitempty"`

	// Event (node→parent).
	Kind string `json:"kind,omitempty"`

	// Req (parent→node tunneled HTTP) — Phase 2. Role is the per-frame asserted
	// effective role (viewer|admin); Actor is the parent operator identity, carried
	// so node-side audit attributes writes to a real person.
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Role    string            `json:"role,omitempty"`
	Actor   string            `json:"actor,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Res (node→parent) — Phase 2.
	Status int `json:"status,omitempty"`

	// Body carries the request/response/event payload. It is []byte so encoding/json
	// base64-encodes it on the wire — meaning any payload (JSON, an image snapshot,
	// form data, or empty) tunnels intact, not just valid JSON.
	Body []byte `json:"body,omitempty"`
	// TS is the sender's unix timestamp (seconds), advisory.
	TS int64 `json:"ts,omitempty"`
}
