package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
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

	mu    sync.Mutex
	conns map[string]*control.Conn // nodeID -> current live connection

	pendingMu sync.Mutex
	pending   map[string]pendingReq // correlation id -> response waiter
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
	}
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
	if err := srv.Run(ctx); err != nil {
		cs.logf("control server stopped: %v", err)
	}
}

// handleConn runs for the lifetime of one node connection.
func (cs *ControlServer) handleConn(nodeID string, conn *control.Conn) {
	node, err := cs.registry.AcceptControlConn(context.Background(), nodeID)
	if err != nil {
		cs.logf("control: rejecting node %s: %v", nodeID, err)
		_ = conn.Close()
		return
	}
	cs.add(nodeID, conn)
	cs.logf("control: node %s (%s) connected", nodeID, node.Name)
	defer func() {
		cs.remove(nodeID, conn)
		cs.logf("control: node %s disconnected", nodeID)
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
func (cs *ControlServer) remove(nodeID string, conn *control.Conn) {
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
