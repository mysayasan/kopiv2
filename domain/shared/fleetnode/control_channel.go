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
	active *control.Conn // the live connection, for pushing events upstream
}

// ForwardEvent pushes a node event (e.g. a notification or going-offline notice)
// up the control channel to the control plane. It is a no-op when the channel is
// not currently connected — events are delivered live, not queued.
func (m *ControlChannelManager) ForwardEvent(kind string, body []byte) {
	m.connMu.Lock()
	conn := m.active
	m.connMu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.WriteFrame(&control.Frame{Type: control.FrameEvent, Kind: kind, Body: body, TS: time.Now().Unix()})
}

func (m *ControlChannelManager) setActive(c *control.Conn) {
	m.connMu.Lock()
	m.active = c
	m.connMu.Unlock()
}

func (m *ControlChannelManager) clearActive(c *control.Conn) {
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
	if err := conn.WriteFrame(&control.Frame{
		Type: control.FrameHello, NodeID: nodeID, Name: st.Name, Version: m.version, TS: time.Now().Unix(),
	}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	m.logf("control channel connected to %s", wsURL)
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
