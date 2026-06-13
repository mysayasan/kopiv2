package stream

import "github.com/pion/rtp"

// Codec is a browser stream codec supported by the WebRTC bridge.
type Codec string

const (
	// CodecH264 is the first supported camera codec for browser live view.
	CodecH264 Codec = "h264"
	// CodecPCMA is G.711 A-law audio (RTP PT=8), natively supported by all browsers.
	CodecPCMA Codec = "pcma"
	// CodecPCMU is G.711 µ-law audio (RTP PT=0), natively supported by all browsers.
	CodecPCMU Codec = "pcmu"
)

// Source identifies one camera stream.
type Source struct {
	ID  string
	URI string
}

// Options controls WebRTC session setup.
type Options struct {
	ICEServers []ICEServer
}

// ICEServer describes one STUN/TURN server exposed to WebRTC peers.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// SessionDescription is the JSON shape exchanged with browser WebRTC clients.
type SessionDescription struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// Subscription carries RTP packets from a camera session to one browser peer.
// AudioPackets is nil when the camera RTSP stream exposes no G.711 audio track.
// H264ProfileLevelID is the 6-hex-digit SDP fmtp value derived from the camera's
// H264 SPS (e.g. "640028" for High 4.0). Empty string means the camera SPS was
// absent from the RTSP DESCRIBE response; the WebRTC layer falls back to "42e01f".
type Subscription struct {
	Codec              Codec
	Packets            <-chan *rtp.Packet
	AudioCodec         Codec
	AudioPackets       <-chan *rtp.Packet
	H264ProfileLevelID string
	Close              func()
}

// Connector opens or shares camera stream subscriptions.
type Connector interface {
	Subscribe(source Source) (*Subscription, error)
	Close() error
}
