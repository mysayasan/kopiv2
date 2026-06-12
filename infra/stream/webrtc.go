package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
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
}

// NewManager creates a stream manager backed by shared RTSP sessions.
func NewManager() *Manager {
	return NewManagerWithOptions(Options{})
}

// NewManagerWithOptions creates a stream manager with runtime WebRTC options.
func NewManagerWithOptions(opts Options) *Manager {
	manager := NewManagerWithConnector(NewRTSPConnector())
	manager.ice = webRTCICEServers(opts.ICEServers)
	return manager
}

// NewManagerWithConnector creates a stream manager with an injectable connector.
func NewManagerWithConnector(connector Connector) *Manager {
	return &Manager{connector: connector}
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

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: webRTCICEServers(opts.ICEServers)})
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

	track, err := webrtc.NewTrackLocalStaticRTP(codecCapability(sub.Codec), "video", source.ID)
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
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed:
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

	go pumpRTP(sub, track, closePeer)
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

func codecCapability(codec Codec) webrtc.RTPCodecCapability {
	switch codec {
	case CodecH264:
		return webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
			RTCPFeedback: []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}},
		}
	default:
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}
	}
}

func pumpRTP(sub *Subscription, track *webrtc.TrackLocalStaticRTP, closePeer func()) {
	defer closePeer()
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
