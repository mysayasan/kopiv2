package mediarelay

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mysayasan/kopiv2/infra/fleetca"
)

// ConnectHandler is invoked for each accepted node media connection, with the node
// identity from the verified client-cert CN. It must block for the connection's
// lifetime (run the read loop); returning closes it.
type ConnectHandler func(nodeID string, c *Conn)

// Server is the parent-side media-channel listener: a dedicated fleet-mTLS endpoint
// accepting node-initiated WebSocket media connections. mTLS is the authentication
// (only a fleet-CA-signed node can connect), so the WebSocket Origin check is off.
type Server struct {
	addr     string
	tlsCfg   *tls.Config
	onConn   ConnectHandler
	logf     func(string, ...any)
	upgrader websocket.Upgrader
}

// NewServer builds a media server bound to addr (":port") presenting tlsCfg (which
// must require + verify the node client cert against the fleet CA).
func NewServer(addr string, tlsCfg *tls.Config, onConn ConnectHandler, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{
		addr:   addr,
		tlsCfg: tlsCfg,
		onConn: onConn,
		logf:   logf,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 15 * time.Second,
			ReadBufferSize:   64 << 10,
			WriteBufferSize:  64 << 10,
			CheckOrigin:      func(*http.Request) bool { return true },
		},
	}
}

// Run serves until ctx is cancelled. Returns nil on a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/media", s.handle)
	srv := &http.Server{Addr: s.addr, Handler: mux, TLSConfig: s.tlsCfg}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	nodeID := fleetca.PeerCommonName(r.TLS)
	if nodeID == "" {
		http.Error(w, "client certificate required", http.StatusUnauthorized)
		return
	}
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logf("media: websocket upgrade failed: %v", err)
		return
	}
	s.onConn(nodeID, newConn(ws))
}
