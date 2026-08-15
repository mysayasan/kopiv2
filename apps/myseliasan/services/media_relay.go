package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mysayasan/kopiv2/infra/mediarelay"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/pion/rtp"
)

const (
	// relayMetaTimeout bounds how long a browser subscription waits for the node to
	// describe the stream before giving up.
	relayMetaTimeout = 15 * time.Second
	// relayPacketBuffer is the per-subscription RTP backlog. On overflow (a stalled
	// browser peer) packets are dropped rather than blocking the node read loop —
	// standard for live media.
	relayPacketBuffer = 1024
)

// MediaRelayHub is the parent side of the camera media relay. It accepts node-dialed
// media connections (fleet mTLS) and, per browser subscription, allocates a streamID,
// asks the node to start that camera, and feeds the relayed RTP into a
// stream.Subscription that the WebRTC layer consumes via Connector(nodeID). Each
// subscription is its own independent node stream (the node sends it a fresh keyframe
// backlog), so the parent needs no GOP cache or fan-out.
type MediaRelayHub struct {
	logf func(string, ...any)

	// onConnect/onDisconnect publish and withdraw this instance's claim on a node's MEDIA
	// channel. Behind a load balancer a browser's video request lands on an arbitrary
	// instance, and only the one holding this connection can answer it. Optional.
	onConnect    func(nodeID string)
	onDisconnect func(nodeID string)

	mu           sync.Mutex
	nodes        map[string]*relayNodeConn
	nextStreamID uint64
}

// SetOwnershipHooks registers callbacks fired (each in its own goroutine) when a node's
// media connection is accepted and when it is torn down. Set once at startup.
func (h *MediaRelayHub) SetOwnershipHooks(onConnect, onDisconnect func(nodeID string)) {
	h.onConnect = onConnect
	h.onDisconnect = onDisconnect
}

type relayNodeConn struct {
	conn *mediarelay.Conn
	mu   sync.Mutex
	subs map[uint64]*relaySub
}

// relaySub carries one browser subscription's relayed media. The node read loop feeds
// packets/audio; Close (or a node disconnect) shuts it down exactly once.
type relaySub struct {
	streamID uint64
	meta     chan mediarelay.MetaPayload
	packets  chan *rtp.Packet
	audio    chan *rtp.Packet
	errc     chan error

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// NewMediaRelayHub builds the hub.
func NewMediaRelayHub(logf func(string, ...any)) *MediaRelayHub {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &MediaRelayHub{logf: logf, nodes: map[string]*relayNodeConn{}}
}

// HandleConn is the mediarelay.Server ConnectHandler: it runs for the lifetime of a
// node's media connection, routing inbound frames to subscriptions. The TLS layer has
// already proven the caller holds nodeID's fleet cert.
func (h *MediaRelayHub) HandleConn(nodeID string, conn *mediarelay.Conn) {
	nc := &relayNodeConn{conn: conn, subs: map[uint64]*relaySub{}}
	h.mu.Lock()
	old := h.nodes[nodeID]
	h.nodes[nodeID] = nc
	h.mu.Unlock()
	if old != nil {
		old.closeAll()
		_ = old.conn.Close()
	}
	h.logf("media: node %s connected", nodeID)
	if h.onConnect != nil {
		go h.onConnect(nodeID)
	}

	pingCtx, stopPing := context.WithCancel(context.Background())
	go relayPingLoop(pingCtx, conn)

	defer func() {
		stopPing()
		h.mu.Lock()
		// Was this still the CURRENT connection? A node that reconnected has already
		// replaced the entry, and this teardown belongs to the old one.
		wasCurrent := h.nodes[nodeID] == nc
		if wasCurrent {
			delete(h.nodes, nodeID)
		}
		h.mu.Unlock()
		nc.closeAll()
		_ = conn.Close()
		if !wasCurrent {
			h.logf("media: node %s stale connection closed (already reconnected)", nodeID)
			return
		}
		h.logf("media: node %s disconnected", nodeID)
		// After the connection is removed, so a peer reacting to this can never observe it
		// as still held here. Guarded by wasCurrent: announcing a stale teardown would
		// withdraw the ownership claim of a LIVE media channel, and every other instance
		// would then report "media channel is not connected" for a streaming camera.
		if h.onDisconnect != nil {
			go h.onDisconnect(nodeID)
		}
	}()

	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			return
		}
		h.dispatch(nc, frame)
	}
}

// IsConnected reports whether a node currently holds a live media connection.
func (h *MediaRelayHub) IsConnected(nodeID string) bool {
	h.mu.Lock()
	_, ok := h.nodes[nodeID]
	h.mu.Unlock()
	return ok
}

// Connector returns a stream.Connector bound to nodeID for plugging into a
// stream.Manager (NewManagerWithConnector). Subscribe parses the camera id from
// source.ID ("camera-N").
func (h *MediaRelayHub) Connector(nodeID string) stream.Connector {
	return &relayConnector{hub: h, nodeID: nodeID}
}

func (h *MediaRelayHub) dispatch(nc *relayNodeConn, f *mediarelay.Frame) {
	switch f.Type {
	case mediarelay.FrameHello:
		// Identity is proven by mTLS; the hello is advisory.
	case mediarelay.FrameMeta:
		var p mediarelay.MetaPayload
		_ = json.Unmarshal(f.Body, &p)
		if sub := nc.get(f.StreamID); sub != nil {
			select {
			case sub.meta <- p:
			default:
			}
		}
	case mediarelay.FrameBacklog, mediarelay.FrameVideoRTP:
		if sub := nc.get(f.StreamID); sub != nil {
			sub.feed(sub.packets, f.Body)
		}
	case mediarelay.FrameAudioRTP:
		if sub := nc.get(f.StreamID); sub != nil {
			sub.feed(sub.audio, f.Body)
		}
	case mediarelay.FrameError:
		var p mediarelay.ErrorPayload
		_ = json.Unmarshal(f.Body, &p)
		if sub := nc.get(f.StreamID); sub != nil {
			select {
			case sub.errc <- fmt.Errorf("node stream error: %s", p.Message):
			default:
			}
			sub.close()
		}
	}
}

func (h *MediaRelayHub) subscribe(nodeID string, camID uint64) (*stream.Subscription, error) {
	h.mu.Lock()
	nc := h.nodes[nodeID]
	streamID := atomic.AddUint64(&h.nextStreamID, 1)
	h.mu.Unlock()
	if nc == nil {
		return nil, fmt.Errorf("node %s media channel is not connected", nodeID)
	}

	sub := &relaySub{
		streamID: streamID,
		meta:     make(chan mediarelay.MetaPayload, 1),
		packets:  make(chan *rtp.Packet, relayPacketBuffer),
		audio:    make(chan *rtp.Packet, relayPacketBuffer),
		errc:     make(chan error, 1),
		done:     make(chan struct{}),
	}
	nc.add(sub)

	start, _ := json.Marshal(mediarelay.StartPayload{CameraID: camID})
	if err := nc.conn.WriteFrame(&mediarelay.Frame{Type: mediarelay.FrameStart, StreamID: streamID, Body: start}); err != nil {
		nc.remove(streamID)
		return nil, fmt.Errorf("request node camera stream: %w", err)
	}

	select {
	case meta := <-sub.meta:
		return h.buildSubscription(nc, sub, meta), nil
	case err := <-sub.errc:
		h.stopStream(nc, streamID)
		return nil, err
	case <-time.After(relayMetaTimeout):
		h.stopStream(nc, streamID)
		return nil, fmt.Errorf("timed out waiting for node camera %d stream", camID)
	}
}

func (h *MediaRelayHub) buildSubscription(nc *relayNodeConn, sub *relaySub, meta mediarelay.MetaPayload) *stream.Subscription {
	s := &stream.Subscription{
		Codec:              stream.Codec(meta.VideoCodec),
		Packets:            sub.packets,
		H264ProfileLevelID: meta.H264ProfileLevelID,
		Close:              func() { h.stopStream(nc, sub.streamID) },
	}
	if strings.TrimSpace(meta.AudioCodec) != "" {
		s.AudioCodec = stream.Codec(meta.AudioCodec)
		s.AudioPackets = sub.audio
	}
	return s
}

// stopStream tells the node to stop the stream and closes the local subscription.
func (h *MediaRelayHub) stopStream(nc *relayNodeConn, streamID uint64) {
	sub := nc.get(streamID)
	nc.remove(streamID)
	if sub != nil {
		sub.close()
	}
	_ = nc.conn.WriteFrame(&mediarelay.Frame{Type: mediarelay.FrameStop, StreamID: streamID})
}

// --- relayNodeConn helpers ---

func (nc *relayNodeConn) get(streamID uint64) *relaySub {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return nc.subs[streamID]
}

func (nc *relayNodeConn) add(s *relaySub) {
	nc.mu.Lock()
	nc.subs[s.streamID] = s
	nc.mu.Unlock()
}

func (nc *relayNodeConn) remove(streamID uint64) {
	nc.mu.Lock()
	delete(nc.subs, streamID)
	nc.mu.Unlock()
}

func (nc *relayNodeConn) closeAll() {
	nc.mu.Lock()
	subs := nc.subs
	nc.subs = map[uint64]*relaySub{}
	nc.mu.Unlock()
	for _, s := range subs {
		s.close()
	}
}

// --- relaySub helpers ---

// feed unmarshals one RTP packet and delivers it without blocking the node read loop;
// it drops on a full buffer (stalled peer) and never writes after close.
func (s *relaySub) feed(ch chan *rtp.Packet, raw []byte) {
	pkt := &rtp.Packet{}
	if err := pkt.Unmarshal(raw); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case ch <- pkt:
	default:
	}
}

// close shuts the subscription down once; closing packets/audio lets the WebRTC
// pumpRTP loops exit cleanly.
func (s *relaySub) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.packets)
	close(s.audio)
	close(s.done)
}

// --- connector ---

type relayConnector struct {
	hub    *MediaRelayHub
	nodeID string
}

func (c *relayConnector) Subscribe(source stream.Source) (*stream.Subscription, error) {
	camID := parseRelayCameraID(source.ID)
	if camID == 0 {
		return nil, fmt.Errorf("invalid camera source %q", source.ID)
	}
	return c.hub.subscribe(c.nodeID, camID)
}

func (c *relayConnector) Close() error { return nil }

func parseRelayCameraID(id string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(id), "camera-"), 10, 64)
	return n
}

func relayPingLoop(ctx context.Context, conn *mediarelay.Conn) {
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
