package pairing

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

// ResponderConfig configures the node-side UDP listener that answers authenticated
// discovery probes while the node is discoverable.
type ResponderConfig struct {
	// FleetKey returns the shared HMAC secret, read live on every probe so a key set
	// or rotated through the API takes effect without restarting the listener. An
	// empty key makes the node undiscoverable (no signature can be produced/verified).
	FleetKey func() []byte
	// MulticastAddr is the group+port to join. Defaults to DefaultMulticastAddr.
	MulticastAddr string
	// ReplayWindow bounds packet freshness. Defaults to DefaultReplayWindow.
	ReplayWindow time.Duration
	// Discoverable reports whether the node should answer right now. It returns
	// false once the node is paired (or has no fleet key), making the node silent —
	// which is exactly how "no longer discoverable after adoption" is enforced.
	Discoverable func() bool
	// AnnounceInfo supplies the live node-identity fields stamped into each reply.
	AnnounceInfo func() AnnounceInfo
	// Logf, if set, receives best-effort diagnostics (never the fleet key).
	Logf func(format string, args ...any)
}

// Responder listens for discovery probes and replies with a signed announce when
// the node is discoverable. Every rejection (bad key, stale, replayed, paired) is
// deliberately silent so a scanner without the fleet key learns nothing.
type Responder struct {
	cfg   ResponderConfig
	cache *nonceCache
}

// NewResponder builds a responder. Defaults are filled for the multicast address
// and replay window.
func NewResponder(cfg ResponderConfig) *Responder {
	if cfg.MulticastAddr == "" {
		cfg.MulticastAddr = DefaultMulticastAddr
	}
	if cfg.ReplayWindow <= 0 {
		cfg.ReplayWindow = DefaultReplayWindow
	}
	return &Responder{cfg: cfg, cache: newNonceCache(cfg.ReplayWindow)}
}

// Run binds the discovery socket and serves probes until ctx is cancelled. It
// joins the multicast group on every multicast-capable interface (so a multi-homed
// host — common on Windows with Hyper-V/WSL/VPN adapters — still receives probes
// regardless of which interface the control plane egressed) and binds to
// 0.0.0.0:port so unicast announce replies carry a valid source address. It is a
// blocking call meant to run in its own goroutine.
func (r *Responder) Run(ctx context.Context) error {
	gaddr, err := net.ResolveUDPAddr("udp4", r.cfg.MulticastAddr)
	if err != nil {
		return fmt.Errorf("pairing: resolve multicast addr: %w", err)
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: gaddr.Port})
	if err != nil {
		return fmt.Errorf("pairing: listen udp :%d: %w", gaddr.Port, err)
	}
	defer conn.Close()

	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true) // so a control plane on the same host is heard
	joined := 0
	for _, ifi := range multicastInterfaces() {
		ifi := ifi
		if err := pc.JoinGroup(&ifi, &net.UDPAddr{IP: gaddr.IP}); err == nil {
			joined++
		}
	}
	r.logf("pairing responder listening on :%d (group %s, joined %d interface(s))", gaddr.Port, gaddr.IP, joined)

	// Closing the socket from a watcher goroutine unblocks ReadFromUDP on shutdown.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 4096)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		announce, ok := r.reply(append([]byte(nil), buf[:n]...))
		if !ok {
			continue
		}
		if _, err := conn.WriteToUDP(announce, src); err != nil {
			r.logf("pairing responder reply to %s failed: %v", src, err)
		} else {
			r.logf("pairing responder answered probe from %s", src)
		}
	}
}

// reply authenticates a raw probe and, if the node should answer, returns the
// signed announce bytes. ok=false means stay silent. Pure and side-effect-light
// (only the replay cache), so it is unit-tested directly without sockets.
func (r *Responder) reply(probeData []byte) (announce []byte, ok bool) {
	if r.cfg.Discoverable != nil && !r.cfg.Discoverable() {
		return nil, false // paired or disabled → invisible
	}
	var key []byte
	if r.cfg.FleetKey != nil {
		key = r.cfg.FleetKey()
	}
	if len(key) == 0 {
		return nil, false // no key → cannot authenticate anyone
	}
	probe, err := ParseProbe(probeData, key, r.cfg.ReplayWindow)
	if err != nil {
		return nil, false // wrong key, malformed, or stale → silent
	}
	if r.cache.seenBefore(probe.Nonce) {
		return nil, false // replayed probe → silent
	}
	var info AnnounceInfo
	if r.cfg.AnnounceInfo != nil {
		info = r.cfg.AnnounceInfo()
	}
	ann, err := NewAnnounce(key, probe.Nonce, info)
	if err != nil {
		return nil, false
	}
	return ann.Marshal(), true
}

func (r *Responder) logf(format string, args ...any) {
	if r.cfg.Logf != nil {
		r.cfg.Logf(format, args...)
	}
}

// multicastInterfaces returns the up, multicast-capable interfaces to join/send on.
func multicastInterfaces() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]net.Interface, 0, len(ifaces))
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		out = append(out, ifi)
	}
	return out
}
