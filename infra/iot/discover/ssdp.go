package discover

import (
	"context"
	"net"
	"strings"
	"time"
)

// ScanSSDP finds UPnP/SSDP devices — smart TVs, Sonos, Roku, DLNA servers, some plugs — by sending
// an M-SEARCH to the SSDP multicast group and collecting the replies for a short window. It is
// read-only: a discovery probe, never a control action.
func ScanSSDP(ctx context.Context, timeout time.Duration) []Found {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	group, err := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if err != nil {
		return nil
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()

	msearch := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 2\r\n" +
		"ST: ssdp:all\r\n\r\n"
	if _, err := conn.WriteToUDP([]byte(msearch), group); err != nil {
		return nil
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	seen := map[string]Found{}
	buf := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			break
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // deadline reached
		}
		h := parseSSDPHeaders(string(buf[:n]))
		key := src.IP.String() + "|" + h["usn"]
		if _, ok := seen[key]; ok {
			continue
		}
		name := firstNonEmpty(h["server"], h["st"], "UPnP device")
		detail := strings.TrimSpace("ST=" + h["st"])
		if loc := h["location"]; loc != "" {
			detail += "  LOCATION=" + loc
		}
		seen[key] = Found{Source: "ssdp", Address: src.IP.String(), Name: name, Detail: detail}
	}

	out := make([]Found, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	return out
}

func parseSSDPHeaders(resp string) map[string]string {
	h := map[string]string{}
	for _, line := range strings.Split(resp, "\r\n") {
		if i := strings.Index(line, ":"); i > 0 {
			h[strings.ToLower(strings.TrimSpace(line[:i]))] = strings.TrimSpace(line[i+1:])
		}
	}
	return h
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
