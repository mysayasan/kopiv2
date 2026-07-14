package pairing

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"golang.org/x/net/ipv4"
)

// ProbeResult is one discovered, signature-verified node.
type ProbeResult struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	IP        string `json:"ip"`
	HTTPSPort int    `json:"httpsPort"`
	// Kind is the node's ADVISORY, UNSIGNED claim about what it is — see Announce.Kind. It is
	// safe to render and unsafe to trust: a hostile host on the LAN can put anything here. The
	// authoritative kind comes from the adopt reply.
	Kind string `json:"kind,omitempty"`
}

// Discover broadcasts a signed discovery probe to the fleet multicast group on
// every multicast-capable interface (so a multi-homed control-plane host reaches
// nodes regardless of which adapter routes to them, and same-host dev works via
// multicast loopback) and collects authenticated announces until timeout (or ctx)
// elapses. Results are deduplicated by node id and sorted. Only announces that echo
// this probe's nonce and carry a valid HMAC for fleetKey are accepted.
func Discover(ctx context.Context, fleetKey []byte, multicastAddr string, timeout time.Duration) ([]ProbeResult, error) {
	if len(fleetKey) == 0 {
		return nil, fmt.Errorf("pairing: discovery requires a fleet key")
	}
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	if multicastAddr == "" {
		multicastAddr = DefaultMulticastAddr
	}

	gaddr, err := net.ResolveUDPAddr("udp4", multicastAddr)
	if err != nil {
		return nil, fmt.Errorf("pairing: resolve multicast addr: %w", err)
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, fmt.Errorf("pairing: open socket: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	probe, err := NewProbe(fleetKey)
	if err != nil {
		return nil, fmt.Errorf("pairing: build probe: %w", err)
	}
	data := probe.Marshal()
	dst := &net.UDPAddr{IP: gaddr.IP, Port: gaddr.Port}

	// Send the probe out every multicast-capable interface, with loopback on so a
	// node on the same host hears it. Duplicates are harmless — the responder
	// de-duplicates by nonce and we de-duplicate announces by node id.
	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true)
	sent := 0
	for _, ifi := range multicastInterfaces() {
		ifi := ifi
		if err := pc.SetMulticastInterface(&ifi); err != nil {
			continue
		}
		if _, err := pc.WriteTo(data, nil, dst); err == nil {
			sent++
		}
	}
	if sent == 0 {
		// Fallback: let the OS pick the default route.
		if _, err := conn.WriteToUDP(data, dst); err != nil {
			return nil, fmt.Errorf("pairing: send probe: %w", err)
		}
	}

	found := make(map[string]ProbeResult)
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			goto done
		default:
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // deadline reached or socket closed
		}
		ann, err := ParseAnnounce(buf[:n], fleetKey, DefaultReplayWindow, probe.Nonce)
		if err != nil || ann.NodeID == "" {
			continue
		}
		if _, ok := found[ann.NodeID]; ok {
			continue
		}
		found[ann.NodeID] = ProbeResult{
			NodeID:    ann.NodeID,
			Name:      ann.Name,
			Version:   ann.Version,
			IP:        src.IP.String(),
			HTTPSPort: ann.HTTPSPort,
			Kind:      ann.Kind,
		}
	}

done:
	out := make([]ProbeResult, 0, len(found))
	for _, r := range found {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}
