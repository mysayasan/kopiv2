package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/fleetca"
	"github.com/mysayasan/kopiv2/infra/mediarelay"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/pion/rtp"
)

const (
	defaultMediaPort   = 49534
	mediaMaxBackoff    = 30 * time.Second
	mediaGateWait      = 5 * time.Second
	mediaInitialBackff = time.Second
)

// MediaSubscriber opens a shared camera RTP subscription (implemented by stream.Manager).
type MediaSubscriber interface {
	Subscribe(source stream.Source) (*stream.Subscription, error)
}

// MediaSourceResolver maps a node-local camera id to its RTSP source (URI + creds).
type MediaSourceResolver func(ctx context.Context, cameraID uint64) (stream.Source, error)

// MediaChannelManager runs the node side of the media relay: once paired + enrolled,
// it dials the parent's media listener over fleet mTLS and, on the parent's request,
// subscribes a local camera and streams its RTP up the channel for the control plane
// to re-broadcast over WebRTC. State is read live from the pairing service (mirrors
// ControlChannelManager / EnrollmentManager); it reconnects with backoff. The media
// channel is bidirectional: the parent sends Start/Stop frames down it, RTP flows up.
type MediaChannelManager struct {
	svc        IPairingService
	mediaPort  int
	version    string
	subscriber MediaSubscriber
	resolve    MediaSourceResolver
	logf       func(format string, args ...any)

	mu      sync.Mutex
	active  *mediarelay.Conn
	streams map[uint64]*camEgress
}

type camEgress struct {
	cancel context.CancelFunc
	sub    *stream.Subscription
}

// NewMediaChannelManager builds the manager. mediaPort<=0 uses the default (49534).
// A nil subscriber/resolver answers Start with an error frame (the channel still
// establishes for liveness).
func NewMediaChannelManager(svc IPairingService, mediaPort int, version string, subscriber MediaSubscriber, resolve MediaSourceResolver, logf func(string, ...any)) *MediaChannelManager {
	if mediaPort <= 0 {
		mediaPort = defaultMediaPort
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &MediaChannelManager{
		svc:        svc,
		mediaPort:  mediaPort,
		version:    version,
		subscriber: subscriber,
		resolve:    resolve,
		logf:       logf,
		streams:    map[uint64]*camEgress{},
	}
}

// Run maintains the channel until ctx is cancelled. Gate mirrors the control channel:
// connect only while paired and holding a usable cert.
func (m *MediaChannelManager) Run(ctx context.Context) {
	backoff := mediaInitialBackff
	for {
		if ctx.Err() != nil {
			return
		}
		st, err := m.svc.Status(ctx)
		if err != nil || !st.Paired {
			if !sleepCtx(ctx, mediaGateWait) {
				return
			}
			continue
		}
		enr, err := m.svc.Enrollment(ctx)
		if err != nil || !enr.HasCert() {
			if !sleepCtx(ctx, mediaGateWait) {
				return
			}
			continue
		}
		if err := m.connectAndServe(ctx, st, enr); err != nil {
			m.logf("media channel: %v (retry in %s)", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, mediaMaxBackoff)
			continue
		}
		backoff = mediaInitialBackff
	}
}

func (m *MediaChannelManager) connectAndServe(ctx context.Context, st PairingStatus, enr Enrollment) error {
	wsURL, err := mediaWSURL(st.ParentBaseURL, m.mediaPort)
	if err != nil {
		return err
	}
	tlsCfg, err := fleetca.ClientTLSConfig(
		[]byte(enr.NodeCertPEM), []byte(enr.NodeKeyPEM), []byte(enr.CARootPEM), st.ParentID)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, err := mediarelay.Dial(dialCtx, wsURL, tlsCfg)
	cancel()
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	defer conn.Close()

	nodeID, _ := m.svc.NodeID(ctx)
	hello, _ := json.Marshal(mediarelay.HelloPayload{NodeID: nodeID, Version: m.version})
	if err := conn.WriteFrame(&mediarelay.Frame{Type: mediarelay.FrameHello, Body: hello}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	m.setActive(conn)
	m.logf("media channel connected to %s", wsURL)
	defer m.teardown(conn)

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()
	go m.pingLoop(loopCtx, conn)

	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			return nil // mid-session drop — reconnect (no backoff)
		}
		switch frame.Type {
		case mediarelay.FrameStart:
			var p mediarelay.StartPayload
			_ = json.Unmarshal(frame.Body, &p)
			m.handleStart(loopCtx, conn, frame.StreamID, p.CameraID)
		case mediarelay.FrameStop:
			m.handleStop(frame.StreamID)
		default:
			// Node side ignores other inbound frame types.
		}
	}
}

// handleStart begins streaming a camera under the parent-allocated streamID
// (idempotent per streamID — a duplicate Start is a no-op).
func (m *MediaChannelManager) handleStart(ctx context.Context, conn *mediarelay.Conn, streamID, camID uint64) {
	m.mu.Lock()
	if _, ok := m.streams[streamID]; ok {
		m.mu.Unlock()
		return
	}
	egCtx, cancel := context.WithCancel(ctx)
	eg := &camEgress{cancel: cancel}
	m.streams[streamID] = eg
	m.mu.Unlock()

	go func() {
		if err := m.runEgress(egCtx, conn, streamID, camID, eg); err != nil && egCtx.Err() == nil {
			m.logf("media channel: stream %d (camera %d) egress stopped: %v", streamID, camID, err)
			body, _ := json.Marshal(mediarelay.ErrorPayload{Message: err.Error()})
			_ = conn.WriteFrame(&mediarelay.Frame{Type: mediarelay.FrameError, StreamID: streamID, Body: body})
		}
		m.handleStop(streamID)
	}()
}

// handleStop stops a stream's egress (idempotent).
func (m *MediaChannelManager) handleStop(streamID uint64) {
	m.mu.Lock()
	eg := m.streams[streamID]
	delete(m.streams, streamID)
	m.mu.Unlock()
	if eg != nil {
		eg.cancel()
	}
}

// runEgress subscribes the camera, announces its codecs, replays the GOP backlog, then
// pumps live RTP (video on this goroutine, audio on a child) until ctx ends or the
// connection drops. Frames are tagged with streamID (the per-subscription id).
func (m *MediaChannelManager) runEgress(ctx context.Context, conn *mediarelay.Conn, streamID, camID uint64, eg *camEgress) error {
	if m.subscriber == nil || m.resolve == nil {
		return fmt.Errorf("media relay is not configured on this node")
	}
	source, err := m.resolve(ctx, camID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(source.URI) == "" {
		return fmt.Errorf("camera %d has no resolved live stream (resolve live view first)", camID)
	}
	sub, err := m.subscriber.Subscribe(source)
	if err != nil {
		return err
	}
	m.mu.Lock()
	eg.sub = sub
	m.mu.Unlock()
	defer sub.Close()

	meta, _ := json.Marshal(mediarelay.MetaPayload{
		VideoCodec:         string(sub.Codec),
		H264ProfileLevelID: sub.H264ProfileLevelID,
		AudioCodec:         string(sub.AudioCodec),
	})
	if err := conn.WriteFrame(&mediarelay.Frame{Type: mediarelay.FrameMeta, StreamID: streamID, Body: meta}); err != nil {
		return err
	}
	// Replay the cached GOP first so a late-joining browser starts on a keyframe.
	for _, pkt := range sub.Backlog {
		if pkt == nil {
			continue
		}
		raw, mErr := pkt.Marshal()
		if mErr != nil {
			continue
		}
		if err := conn.WriteFrame(&mediarelay.Frame{Type: mediarelay.FrameBacklog, StreamID: streamID, Body: raw}); err != nil {
			return err
		}
	}
	if sub.AudioPackets != nil && sub.AudioCodec != "" {
		go m.pumpRTP(ctx, conn, streamID, mediarelay.FrameAudioRTP, sub.AudioPackets)
	}
	return m.pumpRTP(ctx, conn, streamID, mediarelay.FrameVideoRTP, sub.Packets)
}

// pumpRTP marshals each RTP packet from ch and writes it as a media frame until the
// channel closes, ctx ends, or a write fails.
func (m *MediaChannelManager) pumpRTP(ctx context.Context, conn *mediarelay.Conn, camID uint64, t mediarelay.FrameType, ch <-chan *rtp.Packet) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case pkt, ok := <-ch:
			if !ok {
				return nil
			}
			if pkt == nil {
				continue
			}
			raw, err := pkt.Marshal()
			if err != nil {
				continue
			}
			if err := conn.WriteFrame(&mediarelay.Frame{Type: t, StreamID: camID, Body: raw}); err != nil {
				return err
			}
		}
	}
}

func (m *MediaChannelManager) setActive(conn *mediarelay.Conn) {
	m.mu.Lock()
	m.active = conn
	m.mu.Unlock()
}

// teardown drops the active connection reference and cancels every camera egress so
// nothing keeps pulling RTSP after the channel is gone. The parent re-sends Start for
// active viewers when the node reconnects.
func (m *MediaChannelManager) teardown(conn *mediarelay.Conn) {
	m.mu.Lock()
	if m.active == conn {
		m.active = nil
	}
	streams := m.streams
	m.streams = map[uint64]*camEgress{}
	m.mu.Unlock()
	for _, eg := range streams {
		eg.cancel()
	}
}

func (m *MediaChannelManager) pingLoop(ctx context.Context, conn *mediarelay.Conn) {
	t := time.NewTicker(mediarelay.PingPeriod)
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

// mediaWSURL derives wss://host:port/media from the stored parent base URL, replacing
// its port with the media port (mirrors controlWSURL).
func mediaWSURL(parentBaseURL string, port int) (string, error) {
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
	return fmt.Sprintf("wss://%s:%d/media", host, port), nil
}
