package control

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeWait bounds a single frame/control write.
	writeWait = 10 * time.Second
	// pongWait is how long a read may go without any traffic (data or pong) before
	// the connection is considered dead.
	pongWait = 60 * time.Second
	// PingPeriod is how often each side sends a keepalive ping. It must be shorter
	// than pongWait so a pong arrives before the peer's read deadline elapses.
	PingPeriod = (pongWait * 9) / 10
	// maxFrameBytes caps an inbound frame. Tunneled responses may carry a snapshot
	// image, so this is generous; bulk media is never sent over the control channel.
	maxFrameBytes = 16 << 20
)

// Conn wraps a WebSocket with frame-oriented read/write and keepalive deadline
// management. A single goroutine must own ReadFrame; writes are serialized
// internally so any goroutine may call WriteFrame/Ping concurrently.
type Conn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
}

// newConn configures keepalive deadlines on a freshly upgraded/dialed socket.
func newConn(ws *websocket.Conn) *Conn {
	ws.SetReadLimit(maxFrameBytes)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(pongWait))
	})
	return &Conn{ws: ws}
}

// ReadFrame blocks for the next frame. A successful read also extends the read
// deadline, so active traffic keeps the connection alive between pings.
func (c *Conn) ReadFrame() (*Frame, error) {
	var f Frame
	if err := c.ws.ReadJSON(&f); err != nil {
		return nil, err
	}
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	return &f, nil
}

// WriteFrame serializes one frame to the peer.
func (c *Conn) WriteFrame(f *Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteJSON(f)
}

// Ping sends a WebSocket-level keepalive ping.
func (c *Conn) Ping() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
}

// RemoteAddr returns the peer's network address (host:port), for diagnostics/audit. Empty
// when the socket has no addressable peer.
func (c *Conn) RemoteAddr() string {
	if c.ws == nil || c.ws.RemoteAddr() == nil {
		return ""
	}
	return c.ws.RemoteAddr().String()
}

// Close tears down the underlying socket.
// Close is safe on a nil or zero-valued Conn. Teardown paths close whatever they were
// handed, often in a defer and often more than once, and a Close that panics on an
// unstarted connection turns an ordinary cleanup into a lost goroutine.
func (c *Conn) Close() error {
	if c == nil || c.ws == nil {
		return nil
	}
	return c.ws.Close()
}
