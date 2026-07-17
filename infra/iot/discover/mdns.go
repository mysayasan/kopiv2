package discover

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// mdnsServices is the set of DNS-SD service types worth browsing on a home/office LAN. Each is a
// family of devices that advertise themselves over multicast DNS.
var mdnsServices = []string{
	"_googlecast._tcp", // Chromecast / Google TV
	"_airplay._tcp",    // Apple TV / AirPlay speakers
	"_raop._tcp",       // AirPlay audio
	"_hap._tcp",        // HomeKit accessories
	"_sonos._tcp",      // Sonos
	"_spotify-connect._tcp",
	"_printer._tcp", "_ipp._tcp", // printers
	"_shelly._tcp",     // Shelly (also MQTT-capable)
	"_esphomelib._tcp", // ESPHome nodes
	"_http._tcp",       // generic web UIs (NAS, cameras, dashboards)
}

// ScanMDNS browses the LAN over multicast DNS for the common service types above and returns each
// responder as a Found. It is read-only. Many results (a TV, a Chromecast) are discoverable but not
// controllable without a per-vendor driver — the caller surfaces them honestly rather than implying
// they can be driven.
func ScanMDNS(ctx context.Context, timeout time.Duration) []Found {
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	per := timeout / time.Duration(len(mdnsServices))
	if per < 350*time.Millisecond {
		per = 350 * time.Millisecond
	}

	var mu sync.Mutex
	seen := map[string]Found{}

	for _, svc := range mdnsServices {
		if ctx.Err() != nil {
			break
		}
		entries := make(chan *mdns.ServiceEntry, 16)
		done := make(chan struct{})
		go func(service string) {
			defer close(done)
			for e := range entries {
				addr := ""
				if e.AddrV4 != nil {
					addr = e.AddrV4.String()
				}
				if addr == "" {
					continue
				}
				mu.Lock()
				key := addr + "|" + e.Name
				if _, ok := seen[key]; !ok {
					seen[key] = Found{
						Source:  "mdns",
						Address: addr,
						Name:    friendlyMDNS(e.Name, service),
						Detail:  strings.TrimSpace(service + "  " + strings.Join(e.InfoFields, " ")),
					}
				}
				mu.Unlock()
			}
		}(svc)

		// Query blocks for the timeout, streaming entries; close the channel once it returns so the
		// collector goroutine finishes before the next service type.
		_ = mdns.Query(&mdns.QueryParam{
			Service:     svc,
			Domain:      "local",
			Timeout:     per,
			Entries:     entries,
			DisableIPv6: true,
		})
		close(entries)
		<-done
	}

	out := make([]Found, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	return out
}

// friendlyMDNS turns "Living Room._googlecast._tcp.local." into "Living Room (_googlecast._tcp)".
func friendlyMDNS(name, service string) string {
	inst := name
	if i := strings.Index(name, "._"); i > 0 {
		inst = name[:i]
	}
	inst = strings.TrimSpace(inst)
	if inst == "" {
		return service
	}
	return inst + " (" + service + ")"
}
