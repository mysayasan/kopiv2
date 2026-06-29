package mediarelay

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/gorilla/websocket"
)

// Dial opens a media-channel connection to the parent's WebSocket endpoint
// (wss://host:port/media), presenting the node's mTLS material via tlsCfg and
// verifying the parent's server cert through tlsCfg's VerifyPeerCertificate.
func Dial(ctx context.Context, url string, tlsCfg *tls.Config) (*Conn, error) {
	dialer := &websocket.Dialer{
		TLSClientConfig:  tlsCfg,
		HandshakeTimeout: 15 * time.Second,
		ReadBufferSize:   64 << 10,
		WriteBufferSize:  64 << 10,
	}
	ws, resp, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	return newConn(ws), nil
}
