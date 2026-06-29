package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const webRTCSetupTimeout = 15 * time.Second

// Manager creates browser WebRTC sessions from camera RTP subscriptions.
type Manager struct {
	connector Connector
	ice       []webrtc.ICEServer
	// api, when set, is a shared pion API configured with a SettingEngine (NAT 1:1
	// public-IP advertisement + a fixed UDP mux) for the parent relay. nil uses pion's
	// default per-peer ephemeral ports — fine for same-LAN.
	api *webrtc.API
}

// WebRTCEngine holds a shared pion API for the parent media relay so a browser on a
// different network can reach the control plane: it advertises the parent's external
// IP(s) as host candidates (NAT 1:1) and binds a single shared UDP port (one firewall
// rule for all peers). STUN/TURN are passed per session via Options.ICEServers.
type WebRTCEngine struct {
	api *webrtc.API
}

// NewWebRTCEngine builds the shared engine. With no publicIPs and udpPort<=0 it
// returns (nil, nil) — callers then use the default per-peer behavior (host
// candidates, ephemeral ports), which is correct for same-LAN/local dev.
func NewWebRTCEngine(publicIPs []string, udpPort int) (*WebRTCEngine, error) {
	if len(publicIPs) == 0 && udpPort <= 0 {
		return nil, nil
	}
	se := webrtc.SettingEngine{}
	if len(publicIPs) > 0 {
		se.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)
	}
	if udpPort > 0 {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: udpPort})
		if err != nil {
			return nil, fmt.Errorf("bind webrtc udp mux on :%d: %w", udpPort, err)
		}
		se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udp))
	}
	return &WebRTCEngine{api: webrtc.NewAPI(webrtc.WithSettingEngine(se))}, nil
}

// NewManager creates a stream manager backed by shared RTSP sessions.
func NewManager() *Manager {
	return NewManagerWithOptions(Options{})
}

// NewManagerWithOptions creates a stream manager with runtime WebRTC options.
func NewManagerWithOptions(opts Options) *Manager {
	manager := NewManagerWithConnector(NewRTSPConnector(opts.FFmpegPath))
	manager.ice = webRTCICEServers(opts.ICEServers)
	return manager
}

// NewManagerWithConnector creates a stream manager with an injectable connector.
func NewManagerWithConnector(connector Connector) *Manager {
	return &Manager{connector: connector}
}

// NewManagerWithConnectorEngine is like NewManagerWithConnector but uses a shared
// WebRTCEngine (parent-relay public-IP/UDP-mux config). A nil engine falls back to
// pion defaults.
func NewManagerWithConnectorEngine(connector Connector, engine *WebRTCEngine) *Manager {
	m := NewManagerWithConnector(connector)
	if engine != nil {
		m.api = engine.api
	}
	return m
}

// newPeerConnection builds a peer using the manager's configured pion API (shared
// SettingEngine) when present, else the package default.
func (m *Manager) newPeerConnection(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
	if m.api != nil {
		return m.api.NewPeerConnection(config)
	}
	return webrtc.NewPeerConnection(config)
}

func webRTCICEServers(servers []ICEServer) []webrtc.ICEServer {
	if len(servers) == 0 {
		return nil
	}
	result := make([]webrtc.ICEServer, 0, len(servers))
	for _, server := range servers {
		if len(server.URLs) == 0 {
			continue
		}
		result = append(result, webrtc.ICEServer{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return result
}

// Subscribe opens (or shares) a camera RTP subscription directly, for callers that
// relay the RTP elsewhere — e.g. the node→parent media channel — rather than building
// a browser peer here. The returned Subscription is shared across subscribers and
// must be Closed by the caller. Mirrors what CreateWebRTCAnswer does internally.
func (m *Manager) Subscribe(source Source) (*Subscription, error) {
	if m == nil || m.connector == nil {
		return nil, errors.New("stream manager is not configured")
	}
	return m.connector.Subscribe(source)
}

// Close stops active camera stream sessions.
func (m *Manager) Close() error {
	if m == nil || m.connector == nil {
		return nil
	}
	return m.connector.Close()
}

// CreateWebRTCAnswer subscribes to a camera source and answers a browser offer.
func (m *Manager) CreateWebRTCAnswer(ctx context.Context, source Source, offer SessionDescription) (*SessionDescription, error) {
	return m.CreateWebRTCAnswerWithOptions(ctx, source, offer, Options{ICEServers: m.configuredICEServers()})
}

// CreateWebRTCAnswerWithOptions subscribes to a camera source and answers a browser offer with per-session options.
func (m *Manager) CreateWebRTCAnswerWithOptions(ctx context.Context, source Source, offer SessionDescription, opts Options) (*SessionDescription, error) {
	if m == nil || m.connector == nil {
		return nil, errors.New("stream manager is not configured")
	}
	if strings.TrimSpace(offer.SDP) == "" {
		return nil, errors.New("webrtc offer sdp is required")
	}

	setupCtx, cancelSetup := context.WithTimeout(ctx, webRTCSetupTimeout)
	defer cancelSetup()

	sub, err := m.subscribeWithContext(setupCtx, source)
	if err != nil {
		return nil, err
	}

	pc, err := m.newPeerConnection(webrtc.Configuration{ICEServers: webRTCICEServers(opts.ICEServers)})
	if err != nil {
		sub.Close()
		return nil, fmt.Errorf("create peer connection failed: %w", err)
	}

	var closeOnce sync.Once
	closePeer := func() {
		closeOnce.Do(func() {
			sub.Close()
			_ = pc.Close()
		})
	}

	track, err := webrtc.NewTrackLocalStaticRTP(codecCapability(sub), "video", source.ID)
	if err != nil {
		closePeer()
		return nil, fmt.Errorf("create video track failed: %w", err)
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		closePeer()
		return nil, fmt.Errorf("add video track failed: %w", err)
	}
	go drainRTCP(sender)

	var audioTrack *webrtc.TrackLocalStaticRTP
	if sub.AudioPackets != nil && sub.AudioCodec != "" {
		audioTrack, err = webrtc.NewTrackLocalStaticRTP(audioCodecCapability(sub.AudioCodec), "audio", source.ID+"-audio")
		if err != nil {
			closePeer()
			return nil, fmt.Errorf("create audio track failed: %w", err)
		}
		audioSender, err := pc.AddTrack(audioTrack)
		if err != nil {
			closePeer()
			return nil, fmt.Errorf("add audio track failed: %w", err)
		}
		go drainRTCP(audioSender)
	}
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		// Disconnected is transient — ICE may recover without tearing down the
		// subscription. Only close on permanent terminal states.
		switch state {
		case webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateFailed:
			closePeer()
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.NewSDPType(firstNonEmpty(offer.Type, "offer")),
		SDP:  offer.SDP,
	}); err != nil {
		closePeer()
		return nil, fmt.Errorf("set remote description failed: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		closePeer()
		return nil, fmt.Errorf("create webrtc answer failed: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		closePeer()
		return nil, fmt.Errorf("set local description failed: %w", err)
	}

	select {
	case <-gatherComplete:
	case <-setupCtx.Done():
		closePeer()
		return nil, setupCtx.Err()
	}

	go pumpRTP(sub, track, audioTrack, closePeer)
	local := pc.LocalDescription()
	if local == nil {
		closePeer()
		return nil, errors.New("webrtc local description was not created")
	}

	return &SessionDescription{Type: local.Type.String(), SDP: local.SDP}, nil
}

func (m *Manager) configuredICEServers() []ICEServer {
	if len(m.ice) == 0 {
		return nil
	}
	result := make([]ICEServer, 0, len(m.ice))
	for _, server := range m.ice {
		iceServer := ICEServer{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: "",
		}
		if server.Credential != nil {
			iceServer.Credential = fmt.Sprint(server.Credential)
		}
		result = append(result, iceServer)
	}
	return result
}

func (m *Manager) subscribeWithContext(ctx context.Context, source Source) (*Subscription, error) {
	type result struct {
		sub *Subscription
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		sub, err := m.connector.Subscribe(source)
		resultCh <- result{sub: sub, err: err}
	}()
	select {
	case res := <-resultCh:
		return res.sub, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// codecCapability builds the H264 RTP codec capability using the actual
// profile-level-id and packetization-mode from the camera's RTSP SPS so the
// browser hardware decoder is initialised with a profile that matches the
// bitstream it will receive. A mismatch (e.g. advertising Baseline 3.1 while
// the camera sends High 4.0) can force the decoder to reinitialise mid-stream,
// which on Windows triggers a GPU driver TDR (monitor blackout).
func codecCapability(sub *Subscription) webrtc.RTPCodecCapability {
	profileLevelID := sub.H264ProfileLevelID
	if profileLevelID == "" {
		profileLevelID = "42e01f"
	}
	// packetization-mode=1 (non-interleaved: STAP-A + FU-A) is the WebRTC
	// standard and must be fixed regardless of what the camera RTSP SDP declares.
	// Mode 0 ("single NAL unit only") would cause FU-A fragments for large frames
	// to be dropped by the browser decoder, producing garbled / sliding-frame
	// decode artifacts.
	fmtp := fmt.Sprintf(
		"level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=%s",
		profileLevelID,
	)
	return webrtc.RTPCodecCapability{
		MimeType:     webrtc.MimeTypeH264,
		ClockRate:    90000,
		SDPFmtpLine:  fmtp,
		RTCPFeedback: []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}},
	}
}

func pumpRTP(sub *Subscription, track *webrtc.TrackLocalStaticRTP, audioTrack *webrtc.TrackLocalStaticRTP, closePeer func()) {
	defer closePeer()
	if audioTrack != nil && sub.AudioPackets != nil {
		go func() {
			for pkt := range sub.AudioPackets {
				if pkt != nil {
					_ = audioTrack.WriteRTP(pkt)
				}
			}
		}()
	}
	// Replay the cached GOP first so the browser decoder initialises on a
	// keyframe before the live stream resumes. Packets are session-owned and
	// shared across subscribers, so clone before writing (WriteRTP rewrites the
	// header SSRC/payload type per peer).
	for _, pkt := range sub.Backlog {
		if pkt == nil {
			continue
		}
		if err := track.WriteRTP(pkt.Clone()); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				return
			}
			return
		}
	}
	for pkt := range sub.Packets {
		if pkt == nil {
			continue
		}
		if err := track.WriteRTP(pkt); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				return
			}
			return
		}
	}
}

func audioCodecCapability(codec Codec) webrtc.RTPCodecCapability {
	switch codec {
	case CodecPCMU:
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1}
	case CodecOpus:
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}
	default:
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA, ClockRate: 8000, Channels: 1}
	}
}

func drainRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
