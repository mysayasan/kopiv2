package fleetnode

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/control"
	"github.com/mysayasan/kopiv2/infra/fleetca"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

const (
	defaultControlPort   = 49533
	controlMaxBackoff    = 30 * time.Second
	controlGateWait      = 5 * time.Second
	controlInitialBackff = time.Second
)

// ControlDispatcher serves a tunneled parent→node request against the node's own
// HTTP router (in-process, no network hop) and returns the response. It is supplied
// by the apis layer so it can inject the synthetic authenticated principal that
// carries the asserted role; see apis.NewControlDispatcher.
type ControlDispatcher func(ctx context.Context, req control.Request) control.Response

// ControlChannelManager runs the node side of the persistent control channel:
// once the node is paired AND enrolled (holds a fleet cert), it dials the parent's
// control listener over mTLS, keeps the connection alive, and reconnects with
// backoff. It reads all state live from the pairing service so adopt / unpair /
// renewal take effect without a restart — mirroring EnrollmentManager.
//
// Phase 1 establishes the channel and announces a Hello; the parent→node command
// dispatch (Phase 2) and node→parent event push (Phase 4) build on handleFrame.
type ControlChannelManager struct {
	svc         IPairingService
	controlPort int
	version     string
	dispatch    ControlDispatcher
	logf        func(format string, args ...any)

	connMu sync.Mutex
	// active is the live connection, for pushing events upstream. Held as the narrow
	// writer interface rather than *control.Conn so the forwarding paths — including the
	// two failure paths, which are the whole point of this file — can be exercised without
	// standing up a websocket.
	active frameWriter

	// dropMu guards the forwarding counters. ForwardEvent is called from notification
	// delivery, which runs on whatever goroutine published the notification.
	dropMu sync.Mutex
	// droppedSinceConnect counts events this node failed to forward since its last
	// successful hello. It is reported ON the next hello and then reset, so the control
	// plane learns what was lost during the gap it is about to replay.
	droppedSinceConnect int64
	// metrics is optional; nil means the node simply does not export these counters.
	metrics telemetry.Metrics
	// onContact is fired when the control plane is demonstrably reachable. Optional.
	onContact func()
}

// Metric names for control-channel event forwarding. kopiv2_* because this is shared
// infra: every node app that dials a control plane emits the same counters.
const (
	// MetricControlEventsForwarded counts events successfully pushed up the channel.
	// Only useful next to the drop counter — a drop count with no total is a number
	// nobody can size.
	MetricControlEventsForwarded = "kopiv2_control_events_forwarded_total"
	// MetricControlEventsDropped counts events that could NOT be pushed, labelled by
	// kind and reason (disconnected | write_failed).
	//
	// This is the whole point of the metric: ForwardEvent is a no-op when the channel is
	// down, and its write error was discarded, so a node whose events were vanishing and
	// a node with nothing to say produced identical telemetry — silence.
	MetricControlEventsDropped = "kopiv2_control_events_dropped_total"
)

// SetOnContact registers a callback fired whenever this node is in contact with its control
// plane — on every successful hello, and on every frame the parent sends afterwards.
//
// It exists for mypintusan's offline cache clock: a door controller decides whether to trust its
// cached access rules by how long it has been since anything entitled to change them could reach
// it, and a live control channel is exactly that. Optional and nil-safe; a node that does not care
// registers nothing. Called on the channel's own goroutines, so the callback must not block.
func (m *ControlChannelManager) SetOnContact(fn func()) {
	m.onContact = fn
}

func (m *ControlChannelManager) noteContact() {
	if m.onContact != nil {
		m.onContact()
	}
}

// SetMetrics attaches a metrics recorder. Optional and nil-safe: a node without one
// behaves exactly as before. Call once at startup, before Run.
func (m *ControlChannelManager) SetMetrics(metrics telemetry.Metrics) {
	m.metrics = metrics
	if metrics == nil {
		return
	}
	metrics.Describe(MetricControlEventsForwarded, "Node events successfully pushed up the control channel.")
	metrics.Describe(MetricControlEventsDropped, "Node events that could not be pushed up the control channel, by kind and reason.")

	// Publish both counters at ZERO for the kinds this node actually forwards, so a healthy
	// node exports "0 drops" rather than exporting nothing at all.
	//
	// That distinction is the entire subject of this file. A counter with no samples is
	// absent from the scrape, so a node that has never dropped an event and a node with no
	// instrumentation at all look identical — which is the exact confusion the drop counter
	// was added to end, reproduced one level up. It also means a dashboard reads "0"
	// instead of "no data", and an alert rule has a series to evaluate from boot.
	for _, kind := range []string{"notification", "going-offline"} {
		metrics.Add(MetricControlEventsForwarded, telemetry.Labels{"kind": kind}, 0)
		for _, reason := range []string{"disconnected", "write_failed"} {
			metrics.Add(MetricControlEventsDropped, telemetry.Labels{"kind": kind, "reason": reason}, 0)
		}
	}
}

// DroppedSinceConnect reports how many events failed to forward since the last
// successful hello. Exposed for tests and for the node's own diagnostics.
func (m *ControlChannelManager) DroppedSinceConnect() int64 {
	m.dropMu.Lock()
	defer m.dropMu.Unlock()
	return m.droppedSinceConnect
}

// noteDrop records one un-forwardable event.
func (m *ControlChannelManager) noteDrop(kind, reason string) {
	m.dropMu.Lock()
	m.droppedSinceConnect++
	m.dropMu.Unlock()
	if m.metrics != nil {
		m.metrics.Inc(MetricControlEventsDropped, telemetry.Labels{"kind": kind, "reason": reason})
	}
}

// ForwardEvent pushes a node event (e.g. a notification or going-offline notice) up the
// control channel. Events are delivered LIVE, not queued: when the channel is down the
// event does not go up, and the control plane recovers it on reconnect by replaying the
// node's stored notifications.
//
// That recovery is sound, and it used to be entirely unmeasured. Both failure paths here
// returned silently — no counter, no log, and the write error discarded — so a node whose
// events were vanishing and a node with nothing to say looked exactly alike. Counting them
// is what turns "the replay covers it" from an assumption into something checkable: the
// count rides up on the next hello, and the control plane can compare it against what the
// replay actually recovered.
func (m *ControlChannelManager) ForwardEvent(kind string, body []byte) {
	m.connMu.Lock()
	conn := m.active
	m.connMu.Unlock()
	if conn == nil {
		m.noteDrop(kind, "disconnected")
		return
	}
	if err := conn.WriteFrame(&control.Frame{Type: control.FrameEvent, Kind: kind, Body: body, TS: time.Now().Unix()}); err != nil {
		// The channel looked up but the write failed — the connection is going away and
		// this event is lost with it. Counted separately from "disconnected" because it
		// means something different: the node believed it was connected.
		m.noteDrop(kind, "write_failed")
		return
	}
	if m.metrics != nil {
		m.metrics.Inc(MetricControlEventsForwarded, telemetry.Labels{"kind": kind})
	}
}

// frameWriter is the only thing ForwardEvent needs from a connection.
type frameWriter interface {
	WriteFrame(*control.Frame) error
}

func (m *ControlChannelManager) setActive(c frameWriter) {
	m.connMu.Lock()
	m.active = c
	m.connMu.Unlock()
}

func (m *ControlChannelManager) clearActive(c frameWriter) {
	m.connMu.Lock()
	if m.active == c {
		m.active = nil
	}
	m.connMu.Unlock()
}

// NewControlChannelManager builds the manager. controlPort<=0 uses the default; it
// is the shared parent listener port the node dials (host derived from ParentBaseURL).
// dispatch serves parent→node commands against the node's own router; a nil
// dispatch answers commands with 501 (channel still establishes for liveness).
func NewControlChannelManager(svc IPairingService, controlPort int, version string, dispatch ControlDispatcher, logf func(string, ...any)) *ControlChannelManager {
	if controlPort <= 0 {
		controlPort = defaultControlPort
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ControlChannelManager{svc: svc, controlPort: controlPort, version: version, dispatch: dispatch, logf: logf}
}

// Run maintains the channel until ctx is cancelled.
func (m *ControlChannelManager) Run(ctx context.Context) {
	backoff := controlInitialBackff
	for {
		if ctx.Err() != nil {
			return
		}
		// Gate: only connect while paired and holding a usable cert. Until then,
		// wait quietly — EnrollmentManager is bringing the cert up.
		st, err := m.svc.Status(ctx)
		if err != nil || !st.Paired {
			if !sleepCtx(ctx, controlGateWait) {
				return
			}
			continue
		}
		enr, err := m.svc.Enrollment(ctx)
		if err != nil || !enr.HasCert() {
			if !sleepCtx(ctx, controlGateWait) {
				return
			}
			continue
		}

		if err := m.connectAndServe(ctx, st, enr); err != nil {
			m.logf("control channel: %v (retry in %s)", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, controlMaxBackoff)
			continue
		}
		// Clean disconnect (e.g. parent closed, or we unpaired): reconnect promptly,
		// but the gate above re-checks paired/cert first.
		backoff = controlInitialBackff
	}
}

// connectAndServe dials the parent, announces Hello, and serves the read loop until
// the connection drops or the node unpairs. Returns an error only for a failed dial
// (so Run backs off); a mid-session drop returns nil (reconnect promptly).
func (m *ControlChannelManager) connectAndServe(ctx context.Context, st PairingStatus, enr Enrollment) error {
	wsURL, err := controlWSURL(st.ParentBaseURL, m.controlPort)
	if err != nil {
		return err
	}
	// Verify the parent's server cert chains to the fleet CA and its CN == the
	// parent id recorded at adoption (identity, not hostname).
	tlsCfg, err := fleetca.ClientTLSConfig(
		[]byte(enr.NodeCertPEM), []byte(enr.NodeKeyPEM), []byte(enr.CARootPEM), st.ParentID)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, err := control.Dial(dialCtx, wsURL, tlsCfg)
	cancel()
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	defer conn.Close()

	nodeID, _ := m.svc.NodeID(ctx)
	// Tell the control plane what was lost during the gap it is about to replay. Read
	// before the write and cleared only after it succeeds, so a failed hello does not
	// discard the very number that says events went missing.
	dropped := m.DroppedSinceConnect()
	if err := conn.WriteFrame(&control.Frame{
		Type: control.FrameHello, NodeID: nodeID, Name: st.Name, Version: m.version,
		Dropped: dropped, TS: time.Now().Unix(),
	}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	if dropped > 0 {
		m.logf("control channel: %d event(s) could not be forwarded while disconnected", dropped)
		m.dropMu.Lock()
		m.droppedSinceConnect -= dropped
		m.dropMu.Unlock()
	}
	m.logf("control channel connected to %s", wsURL)
	m.noteContact()
	m.setActive(conn)
	defer m.clearActive(conn)

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()
	go m.pingLoop(loopCtx, conn)
	go m.watchUnpair(loopCtx, conn)

	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			return nil // mid-session drop — reconnect (no backoff)
		}
		// Every frame the parent sends is fresh evidence the control plane can reach this node —
		// including the pong that answers the keepalive, which is the only traffic on an idle
		// channel. A session that established hours ago and has said nothing since is not the same
		// as one that is still up, and a cache clock that could not tell them apart would be
		// measuring the handshake rather than the connection.
		m.noteContact()
		m.handleFrame(loopCtx, conn, frame)
	}
}

// handleFrame processes an inbound parent frame. A command (FrameReq) is served
// concurrently so a slow handler doesn't stall the read loop; the reply rides back
// over the same connection, correlated by frame ID.
func (m *ControlChannelManager) handleFrame(ctx context.Context, conn *control.Conn, f *control.Frame) {
	switch f.Type {
	case control.FrameReq:
		go m.serveReq(ctx, conn, f)
	default:
		// Hello/Event/Res are not expected from the parent on the node side.
	}
}

// serveReq dispatches one tunneled request against the node's router and writes the
// correlated response frame back to the parent.
func (m *ControlChannelManager) serveReq(ctx context.Context, conn *control.Conn, f *control.Frame) {
	var resp control.Response
	if m.dispatch == nil {
		resp = control.Response{Status: http.StatusNotImplemented}
	} else {
		resp = m.dispatch(ctx, control.RequestFromFrame(f))
	}
	if err := conn.WriteFrame(resp.ToFrame(f.ID)); err != nil {
		m.logf("control channel: write response for %s failed: %v", f.ID, err)
	}
}

// pingLoop sends keepalive pings until the connection drops or ctx is cancelled.
func (m *ControlChannelManager) pingLoop(ctx context.Context, conn *control.Conn) {
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

// watchUnpair closes the connection promptly if the node is unpaired while
// connected, so the channel doesn't linger until a ping fails.
func (m *ControlChannelManager) watchUnpair(ctx context.Context, conn *control.Conn) {
	t := time.NewTicker(controlGateWait)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if st, err := m.svc.Status(ctx); err == nil && !st.Paired {
				// Tell the control plane we're leaving before dropping the channel.
				m.ForwardEvent("going-offline", nil)
				_ = conn.Close()
				return
			}
		}
	}
}

// controlWSURL derives the parent control endpoint (wss://host:port/control) from
// the stored parent base URL, replacing its port with the control port.
func controlWSURL(parentBaseURL string, port int) (string, error) {
	parentBaseURL = strings.TrimSpace(parentBaseURL)
	if parentBaseURL == "" {
		return "", fmt.Errorf("no parent base URL")
	}
	u, err := url.Parse(parentBaseURL)
	if err != nil {
		return "", fmt.Errorf("parse parent URL: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("parent URL has no host")
	}
	return fmt.Sprintf("wss://%s:%d/control", host, port), nil
}

// sleepCtx sleeps for d or until ctx is cancelled; returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
