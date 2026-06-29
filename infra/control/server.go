package control

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mysayasan/kopiv2/infra/fleetca"
)

// ConnectHandler is invoked for each accepted node connection. It is called with
// the node identity taken from the verified client certificate (CN) and must block
// for the lifetime of the connection (run the read loop); returning closes it.
type ConnectHandler func(nodeID string, c *Conn)

// Server is the parent-side control-channel listener: a dedicated fleet-mTLS
// endpoint that accepts node-initiated WebSocket connections. The mTLS layer (via
// tlsCfg) is the authentication — only a node holding a fleet-CA-signed cert can
// connect — so the WebSocket Origin check is disabled.
type Server struct {
	addr     string
	tlsCfg   *tls.Config
	onConn   ConnectHandler
	logf     func(string, ...any)
	upgrader websocket.Upgrader
}

// NewServer builds a control server bound to addr (":port") presenting tlsCfg
// (which must require + verify the node client cert against the fleet CA).
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
			// mTLS authenticates the peer; browser Origin is irrelevant here.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// Run serves until ctx is cancelled. Returns nil on a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/control", s.handle)
	srv := &http.Server{Addr: s.addr, Handler: mux, TLSConfig: s.tlsCfg}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	// Cert/key are already in TLSConfig.Certificates, so the file args are empty.
	if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// The TLS layer already required + verified the client cert against the fleet
	// CA; the CN is the node identity.
	nodeID := fleetca.PeerCommonName(r.TLS)
	if nodeID == "" {
		http.Error(w, "client certificate required", http.StatusUnauthorized)
		return
	}
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logf("control: websocket upgrade failed: %v", err)
		return
	}
	s.onConn(nodeID, newConn(ws))
}
