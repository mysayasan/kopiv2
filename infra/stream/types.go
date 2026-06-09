package stream

import "github.com/pion/rtp"

// Codec is a browser stream codec supported by the WebRTC bridge.
type Codec string

const (
	// CodecH264 is the first supported camera codec for browser live view.
	CodecH264 Codec = "h264"
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
type Subscription struct {
	Codec   Codec
	Packets <-chan *rtp.Packet
	Close   func()
}

// Connector opens or shares camera stream subscriptions.
type Connector interface {
	Subscribe(source Source) (*Subscription, error)
	Close() error
}
