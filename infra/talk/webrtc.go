package talk

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// SessionDescription is a minimal SDP offer/answer pair (mirrors the stream
// package's type so callers don't import pion directly).
type SessionDescription struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

const webRTCSetupTimeout = 15 * time.Second

// AnswerBrowserTalk negotiates a browser→server audio path forced to G.711
// A-law (PCMA) and pumps the received frames into the given camera talk session.
// The browser side must offer a sendonly/sendrecv audio track; we answer
// recvonly. The session is closed when the peer connection ends.
func AnswerBrowserTalk(offer SessionDescription, session Session, iceServers []ICEServer) (*SessionDescription, error) {
	if strings.TrimSpace(offer.SDP) == "" {
		return nil, errors.New("webrtc offer sdp is required")
	}

	// Register ONLY PCMA so the negotiated codec is A-law — exactly what the
	// camera backchannel consumes, with no server-side transcode.
	engine := &webrtc.MediaEngine{}
	if err := engine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA, ClockRate: 8000, Channels: 1},
		PayloadType:        8,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(engine))

	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: webRTCICEServers(iceServers)})
	if err != nil {
		return nil, err
	}

	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			_ = session.Close()
			_ = pc.Close()
		})
	}

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		closeAll()
		return nil, err
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		go pumpTrack(track, session, closeAll)
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected:
			closeAll()
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.NewSDPType(firstNonEmpty(offer.Type, "offer")),
		SDP:  offer.SDP,
	}); err != nil {
		closeAll()
		return nil, err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		closeAll()
		return nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		closeAll()
		return nil, err
	}
	select {
	case <-gatherComplete:
	case <-time.After(webRTCSetupTimeout):
		closeAll()
		return nil, errors.New("webrtc ICE gathering timed out")
	}

	local := pc.LocalDescription()
	if local == nil {
		closeAll()
		return nil, errors.New("webrtc local description was not created")
	}
	return &SessionDescription{Type: local.Type.String(), SDP: local.SDP}, nil
}

// pumpTrack reads PCMA RTP from the browser and forwards each frame to the
// camera talk session until the track ends.
func pumpTrack(track *webrtc.TrackRemote, session Session, done func()) {
	defer done()
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		if err := session.WritePCMA(pkt.Payload, pkt.Timestamp); err != nil {
			return
		}
	}
}

// ICEServer mirrors the stream package's ICE server config.
type ICEServer struct {
	URLs       []string
	Username   string
	Credential string
}

func webRTCICEServers(servers []ICEServer) []webrtc.ICEServer {
	if len(servers) == 0 {
		return nil
	}
	out := make([]webrtc.ICEServer, 0, len(servers))
	for _, s := range servers {
		if len(s.URLs) == 0 {
			continue
		}
		out = append(out, webrtc.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
