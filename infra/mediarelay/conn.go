package mediarelay

import (
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeWait bounds a single frame/control write.
	writeWait = 10 * time.Second
	// pongWait is how long a read may go without any traffic before the connection is
	// considered dead.
	pongWait = 60 * time.Second
	// PingPeriod is how often each side sends a keepalive ping (< pongWait).
	PingPeriod = (pongWait * 9) / 10
	// maxFrameBytes caps an inbound frame. RTP packets are small (a few KB), but allow
	// generous headroom for fragmented NALs / large audio frames.
	maxFrameBytes = 4 << 20
)

// Conn wraps a WebSocket carrying binary media frames. One goroutine must own
// ReadFrame; writes are serialized so any goroutine may WriteFrame/Ping concurrently.
type Conn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
}

func newConn(ws *websocket.Conn) *Conn {
	ws.SetReadLimit(maxFrameBytes)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(pongWait))
	})
	return &Conn{ws: ws}
}

// ReadFrame blocks for the next binary frame and extends the read deadline.
func (c *Conn) ReadFrame() (*Frame, error) {
	msgType, msg, err := c.ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	if msgType != websocket.BinaryMessage {
		return nil, fmt.Errorf("mediarelay: unexpected websocket message type %d", msgType)
	}
	return parseFrame(msg)
}

// WriteFrame serializes one frame to the peer as a binary message.
func (c *Conn) WriteFrame(f *Frame) error {
	data := f.Marshal()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(websocket.BinaryMessage, data)
}

// Ping sends a WebSocket-level keepalive ping.
func (c *Conn) Ping() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
}

// Close tears down the underlying socket.
func (c *Conn) Close() error {
	return c.ws.Close()
}
