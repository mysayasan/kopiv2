package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mysayasan/kopiv2/infra/control"
)

// defaultControlPort is the parent's control-channel listener port (mirrors the
// node-side mTLS management port convention). Distinct from the node's 49532.
const defaultControlPort = 49533

// controlRequestTimeout bounds how long a tunneled command waits for the node's
// response before giving up (used when the caller's context has no deadline).
const controlRequestTimeout = 30 * time.Second

// ErrNodeOffline is returned when a command targets a node that has no live
// control-channel connection.
var ErrNodeOffline = errors.New("node is not connected to the control channel")

// ErrNodeDisconnected is returned when a node's control channel drops while a
// tunneled command is still awaiting its response — so the caller fails fast instead
// of hanging until controlRequestTimeout.
var ErrNodeDisconnected = errors.New("node control channel disconnected before response")

// ControlSender sends a tunneled request to a node and returns its response. The
// proxy API depends on this narrow interface (ControlServer implements it).
type ControlSender interface {
	SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error)
}

// ControlServer accepts node-initiated control-channel connections over fleet
// mTLS and tracks the live connection for each node. Phase 1 establishes and
// keeps the channel alive (presence = liveness) and ingests node frames; the
// parent→node command tunnel (Phase 2) will send request frames over the conns
// held here, and node→parent events (Phase 4) will be routed from dispatch.
// NodeEventHandler receives an event a node pushed up its control channel (kind is
// e.g. "notification" or "going-offline"; body is the kind-specific JSON payload).
type NodeEventHandler func(nodeID, kind string, body []byte)

type ControlServer struct {
	registry INodeRegistry
	port     int
	onEvent  NodeEventHandler
	logf     func(string, ...any)

	running atomic.Bool // true while the control listener's serve loop is active

	// onConnect, when set, is invoked (in its own goroutine) each time a node's control
	// connection is accepted — the moment to reconcile anything that could have drifted while
	// the node was gone (e.g. replay notifications published during the disconnect). Optional.
	onConnect func(nodeID string)

	// onDropReport, when set, is invoked (in its own goroutine) when a node's hello admits
	// it failed to forward events while it was disconnected. Optional; nil-safe.
	onDropReport func(nodeID string, dropped int64)

	// onDisconnect, when set, is invoked (in its own goroutine) once a node's control
	// connection has been torn down. Its counterpart to onConnect exists so ownership of
	// the node can be withdrawn promptly — behind a load balancer another instance is
	// waiting to take the node over, and without this it would have to wait out the
	// ownership lease instead. Optional.
	onDisconnect func(nodeID string)

	mu    sync.Mutex
	conns map[string]*control.Conn // nodeID -> current live connection

	pendingMu sync.Mutex
	pending   map[string]pendingReq // correlation id -> response waiter

	// rejected tracks nodes that dialed the control channel but were refused (unknown row or
	// revoked cert). These are otherwise INVISIBLE — the connection is closed and nothing is
	// persisted — so a stranded node (paired to us but with no managed record) would only ever
	// show up as a recurring log line. Surfacing them lets an operator see and block one.
	rejectedMu sync.Mutex
	rejected   map[string]*RejectedNode
}

// RejectedNode is a node that keeps connecting to the control channel but is refused. It
// holds a valid fleet-CA certificate (or the mTLS handshake would have failed) yet has no
// usable managed record — typically a node released here but never reset on its side, or one
// whose row was lost. Reason is human-facing ("unknown node" / "certificate revoked").
type RejectedNode struct {
	NodeID     string `json:"nodeId"`
	Reason     string `json:"reason"`
	RemoteAddr string `json:"remoteAddr"`
	FirstSeen  int64  `json:"firstSeen"`
	LastSeen   int64  `json:"lastSeen"`
	Count      int    `json:"count"`
}

// pendingReq is an in-flight tunneled request awaiting its node response. nodeID lets
// a control-channel drop fail exactly the waiters bound to that node. A nil frame
// delivered on ch signals the connection was lost.
type pendingReq struct {
	ch     chan *control.Frame
	nodeID string
}

// NewControlServer builds the control server. port<=0 uses defaultControlPort.
// onEvent (optional) is invoked for each node-pushed event frame.
func NewControlServer(registry INodeRegistry, port int, onEvent NodeEventHandler, logf func(string, ...any)) *ControlServer {
	if port <= 0 {
		port = defaultControlPort
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ControlServer{
		registry: registry,
		port:     port,
		onEvent:  onEvent,
		logf:     logf,
		conns:    map[string]*control.Conn{},
		pending:  map[string]pendingReq{},
		rejected: map[string]*RejectedNode{},
	}
}

// recordRejected notes a refused control-channel connection so an operator can see and act on
// a stranded node. Deduped by nodeID; bumps the count + LastSeen on repeats (the node retries
// in a tight loop). Bounded so a flood of distinct ids can't grow it without limit.
func (cs *ControlServer) recordRejected(nodeID, remoteAddr, reason string) {
	cs.rejectedMu.Lock()
	defer cs.rejectedMu.Unlock()
	now := time.Now().Unix()
	if r, ok := cs.rejected[nodeID]; ok {
		r.LastSeen = now
		r.Count++
		if remoteAddr != "" {
			r.RemoteAddr = remoteAddr
		}
		r.Reason = reason
		return
	}
	const maxRejected = 200
	if len(cs.rejected) >= maxRejected {
		// Evict the stalest entry so the map stays bounded.
		var oldestID string
		var oldest int64
		for id, r := range cs.rejected {
			if oldestID == "" || r.LastSeen < oldest {
				oldestID, oldest = id, r.LastSeen
			}
		}
		delete(cs.rejected, oldestID)
	}
	cs.rejected[nodeID] = &RejectedNode{NodeID: nodeID, Reason: reason, RemoteAddr: remoteAddr, FirstSeen: now, LastSeen: now, Count: 1}
}

// Unrecognized returns the currently-tracked refused nodes, most-recent first.
func (cs *ControlServer) Unrecognized() []RejectedNode {
	cs.rejectedMu.Lock()
	defer cs.rejectedMu.Unlock()
	out := make([]RejectedNode, 0, len(cs.rejected))
	for _, r := range cs.rejected {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	return out
}

// ForgetRejected drops a node from the rejected list (operator dismissed it, or it was
// blocked/handled). It reappears only if it dials and is refused again.
func (cs *ControlServer) ForgetRejected(nodeID string) {
	cs.rejectedMu.Lock()
	delete(cs.rejected, nodeID)
	cs.rejectedMu.Unlock()
}

// SendRequest tunnels a request to nodeID over its live control connection and
// waits for the correlated response. Returns ErrNodeOffline when the node has no
// connection. Honors ctx cancellation; falls back to controlRequestTimeout when ctx
// has no deadline of its own.
func (cs *ControlServer) SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error) {
	cs.mu.Lock()
	conn := cs.conns[nodeID]
	cs.mu.Unlock()
	if conn == nil {
		return control.Response{}, ErrNodeOffline
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, controlRequestTimeout)
		defer cancel()
	}

	id := newCorrelationID()
	waiter := make(chan *control.Frame, 1)
	cs.pendingMu.Lock()
	cs.pending[id] = pendingReq{ch: waiter, nodeID: nodeID}
	cs.pendingMu.Unlock()
	defer func() {
		cs.pendingMu.Lock()
		delete(cs.pending, id)
		cs.pendingMu.Unlock()
	}()

	if err := conn.WriteFrame(req.ToFrame(id)); err != nil {
		return control.Response{}, fmt.Errorf("send to node %s: %w", nodeID, err)
	}

	select {
	case <-ctx.Done():
		return control.Response{}, ctx.Err()
	case f := <-waiter:
		if f == nil {
			// The control channel dropped before the node answered — fail fast rather
			// than hang. The write may have applied on the node, so the caller must treat
			// the outcome as unknown (no automatic retry for non-idempotent commands).
			return control.Response{}, ErrNodeDisconnected
		}
		return control.ResponseFromFrame(f), nil
	}
}

// deliverResponse routes a FrameRes to its waiting SendRequest, if still pending.
func (cs *ControlServer) deliverResponse(f *control.Frame) {
	cs.pendingMu.Lock()
	req, ok := cs.pending[f.ID]
	cs.pendingMu.Unlock()
	if ok {
		select {
		case req.ch <- f:
		default:
		}
	}
}

// failPending signals every in-flight request bound to nodeID that its connection is
// gone (nil frame), so their SendRequest calls return ErrNodeDisconnected immediately
// instead of blocking until the request timeout. Called when a node disconnects.
func (cs *ControlServer) failPending(nodeID string) {
	cs.pendingMu.Lock()
	for _, req := range cs.pending {
		if req.nodeID == nodeID {
			select {
			case req.ch <- nil:
			default:
			}
		}
	}
	cs.pendingMu.Unlock()
}

// newCorrelationID returns a short random id for request/response matching.
func newCorrelationID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Run serves the control listener until ctx is cancelled.
func (cs *ControlServer) Run(ctx context.Context) {
	tlsCfg, err := cs.registry.ParentServerTLS(ctx)
	if err != nil {
		cs.logf("control server: TLS config unavailable: %v", err)
		return
	}
	srv := control.NewServer(fmt.Sprintf(":%d", cs.port), tlsCfg, cs.handleConn, cs.logf)
	cs.logf("control channel listening on :%d", cs.port)
	cs.running.Store(true)
	defer cs.running.Store(false)
	if err := srv.Run(ctx); err != nil {
		cs.logf("control server stopped: %v", err)
	}
}

// IsListening reports whether the control-channel serve loop is currently active. It
// feeds the app's readiness probe so an operator can see the fleet listener is up
// (advisory — it does not gate the process's db/cache readiness).
func (cs *ControlServer) IsListening() bool { return cs.running.Load() }

// SetOnConnect registers a callback invoked (in its own goroutine) whenever a node's control
// connection is accepted. Set once at startup, before Run. See the onConnect field.
func (cs *ControlServer) SetOnConnect(fn func(nodeID string)) { cs.onConnect = fn }

// SetOnDisconnect registers a callback invoked (in its own goroutine) once a node's control
// connection has been torn down. Set once at startup, before Run. See the onDisconnect field.
func (cs *ControlServer) SetOnDisconnect(fn func(nodeID string)) { cs.onDisconnect = fn }

// ConnectedCount returns the number of nodes currently holding a live control channel.
func (cs *ControlServer) ConnectedCount() int {
	cs.mu.Lock()
	n := len(cs.conns)
	cs.mu.Unlock()
	return n
}

// handleConn runs for the lifetime of one node connection.
func (cs *ControlServer) handleConn(nodeID string, conn *control.Conn) {
	node, err := cs.registry.AcceptControlConn(context.Background(), nodeID)
	if err != nil {
		cs.logf("control: rejecting node %s: %v", nodeID, err)
		cs.recordRejected(nodeID, conn.RemoteAddr(), err.Error())
		_ = conn.Close()
		return
	}
	// A node that now connects cleanly is no longer a problem — drop any stale rejection.
	cs.ForgetRejected(nodeID)
	cs.add(nodeID, conn)
	cs.logf("control: node %s (%s) connected", nodeID, node.Name)
	// Reconcile drift from the disconnect (e.g. catch up on notifications the node published
	// while the channel was down). Off the read loop so a slow reconcile never stalls frames.
	if cs.onConnect != nil {
		go cs.onConnect(nodeID)
	}
	defer func() {
		// Only announce a disconnect for the connection that was still CURRENT. A node
		// that reconnected — to this same instance — has already replaced the entry, and
		// the old goroutine's teardown arrives afterwards. Announcing it unconditionally
		// withdrew the ownership claim of a live connection, after which nothing held the
		// node: the heartbeat reads presence from that registry, so a perfectly healthy
		// node was marked lost and alerted on, on a SINGLE instance as much as a cluster.
		wasCurrent := cs.remove(nodeID, conn)
		if !wasCurrent {
			cs.logf("control: node %s stale connection closed (already reconnected)", nodeID)
			return
		}
		cs.logf("control: node %s disconnected", nodeID)
		// After remove, so a peer that reacts to this can never observe the connection as
		// still held here. Off the read loop for the same reason as onConnect.
		if cs.onDisconnect != nil {
			go cs.onDisconnect(nodeID)
		}
	}()

	pingCtx, stopPing := context.WithCancel(context.Background())
	defer stopPing()
	go pingLoop(pingCtx, conn)

	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			return
		}
		cs.dispatch(nodeID, frame)
	}
}

// dispatch handles an inbound frame from a node. Phase 1 understands Hello and
// logs events; the command-response and event-routing paths land in later phases.
func (cs *ControlServer) dispatch(nodeID string, frame *control.Frame) {
	switch frame.Type {
	case control.FrameHello:
		cs.logf("control: node %s hello (version %s)", nodeID, frame.Version)
		// Keep what the node said it is running. This used to be logged and thrown away,
		// which left the control plane unable to answer the first question any fleet
		// upgrade asks — what is each appliance on right now. Best-effort and off the
		// read loop: a database hiccup must not cost us the connection.
		if cs.registry != nil && strings.TrimSpace(frame.Version) != "" {
			version := frame.Version
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := cs.registry.RecordVersion(ctx, nodeID, version); err != nil {
					cs.logf("control: record version for node %s: %v", nodeID, err)
				}
			}()
		}
		// The node is telling us how many events it could not forward while it was away.
		// Those are exactly what the reconnect replay is about to go and fetch, so this is
		// the number that makes the replay checkable instead of merely believed.
		if frame.Dropped > 0 {
			cs.logf("control: node %s reports %d event(s) dropped while disconnected", nodeID, frame.Dropped)
			if cs.onDropReport != nil {
				dropped := frame.Dropped
				go cs.onDropReport(nodeID, dropped)
			}
		}
	case control.FrameEvent:
		cs.logf("control: node %s event %q", nodeID, frame.Kind)
		if cs.onEvent != nil {
			cs.onEvent(nodeID, frame.Kind, frame.Body)
		}
	case control.FrameRes:
		cs.deliverResponse(frame)
	default:
		// Unknown frame types are ignored.
	}
}

// SetDropReportHandler installs the callback for a node admitting it lost events while
// disconnected. Set once at startup.
func (cs *ControlServer) SetDropReportHandler(fn func(nodeID string, dropped int64)) {
	cs.onDropReport = fn
}

// IsConnected reports whether nodeID currently holds a live control-channel
// connection. It is the authoritative liveness signal the heartbeat reconciler
// consults before falling back to the mTLS poll.
func (cs *ControlServer) IsConnected(nodeID string) bool {
	cs.mu.Lock()
	_, ok := cs.conns[nodeID]
	cs.mu.Unlock()
	return ok
}

// add registers a node's live connection, replacing (and closing) any stale one.
func (cs *ControlServer) add(nodeID string, conn *control.Conn) {
	cs.mu.Lock()
	old := cs.conns[nodeID]
	cs.conns[nodeID] = conn
	cs.mu.Unlock()
	if old != nil && old != conn {
		_ = old.Close()
	}
}

// remove clears a node's connection if it is still the current one, and fails any
// in-flight tunneled requests waiting on it so callers don't hang to the timeout.
// It reports whether this connection was still the current one. A node that reconnected
// meanwhile has already had its entry replaced, and the OLD connection's teardown must not
// be mistaken for the node going away — see the caller.
func (cs *ControlServer) remove(nodeID string, conn *control.Conn) bool {
	cs.mu.Lock()
	current := cs.conns[nodeID] == conn
	if current {
		delete(cs.conns, nodeID)
	}
	cs.mu.Unlock()
	if current {
		cs.failPending(nodeID)
	}
	_ = conn.Close()
	return current
}

// pingLoop sends periodic keepalive pings until ctx is cancelled or a write fails.
func pingLoop(ctx context.Context, conn *control.Conn) {
	t := time.NewTicker(control.PingPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := conn.Ping(); err != nil {
				return
			}
		}
	}
}
