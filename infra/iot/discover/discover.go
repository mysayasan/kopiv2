// Package discover holds the active network scanners that find IoT/industrial devices already on a
// LAN, as opposed to the enrollment window (where a device announces itself over MQTT). Each scanner
// is a distinct protocol — there is no single "scan for all IoT" — but they all normalise to Found,
// so the app can turn any of them into the same quarantined adoption candidate.
//
// SAFETY POSTURE. Every scan here is LAN-local (subnet sweep or link-local multicast), READ-ONLY
// (nothing is ever written to a discovered device), bounded (host cap, per-operation timeout,
// concurrency cap) and cancellable. A scan on an industrial network can perturb fragile gear, so the
// callers make it opt-in and gentle; this package never broadcasts a write or a control command.
package discover

import (
	"fmt"
	"net"
	"strings"
)

// Found is one discovered device, normalised across every scanner.
type Found struct {
	Source  string // "modbus" | "mdns" | "ssdp" | "ethernetip" | "bacnet"
	Address string // ip, or ip:port
	Name    string // best friendly label (mDNS instance, SSDP server, vendor+model)
	Vendor  string
	Model   string
	Detail  string // free-form evidence shown to the admin
	// Modbus-only: the connection to pre-fill on adoption.
	Endpoint  string
	Unit      int
	Transport string // "tcp" | "rtutcp"
}

// Hosts expands CIDRs (and bare host entries) to a flat IP list, refusing a range wider than max so a
// fat mask can never launch millions of probes.
func Hosts(cidrs []string, max int) ([]string, error) {
	var out []string
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			out = append(out, c) // a bare host/IP
			if len(out) > max {
				return nil, fmt.Errorf("scan range too large (> %d hosts)", max)
			}
			continue
		}
		ip, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("bad CIDR %q: %w", c, err)
		}
		for cur := ip.Mask(ipnet.Mask); ipnet.Contains(cur); incIP(cur) {
			out = append(out, cur.String())
			if len(out) > max {
				return nil, fmt.Errorf("scan range too large (> %d hosts)", max)
			}
		}
	}
	return out, nil
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
