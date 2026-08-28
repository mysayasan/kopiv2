// Package discover holds the active network scanners that find IoT/industrial devices already on a
// LAN, as opposed to the enrollment window (where a device announces itself over MQTT). Each scanner
// is a distinct protocol — there is no single "scan for all IoT" — but they all normalise to Found,
// so the app can turn any of them into the same quarantined adoption candidate.
//
// SAFETY POSTURE. Every scan here is LAN-local, READ-ONLY (nothing is ever written to a discovered
// device), bounded (host cap, per-operation timeout, concurrency cap) and cancellable. A scan on an
// industrial network can perturb fragile gear, so the callers make it opt-in and gentle; this
// package never broadcasts a write or a control command.
//
// LAN-local is ENFORCED, not merely intended — see Hosts. The multicast scanners cannot leave the
// link by construction, but the Modbus sweep takes its target from a string an admin types, and for
// a long time nothing checked it: a live appliance would accept and really run a sweep of public
// address space, or of its own loopback. Hosts now refuses anything outside private/link-local
// ranges.
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
// fat mask can never launch millions of probes, and refusing any target outside LAN address space.
//
// THE SECOND REFUSAL IS THE IMPORTANT ONE, and it was missing. The package doc has always said
// "every scan here is LAN-local", and for the multicast scanners that is true by construction —
// they cannot leave the link. The Modbus sweep, though, takes its target from a string an admin
// types, and nothing checked it. Measured on a running appliance: a sweep of 192.0.2.0/24 (public
// address space) was accepted and really ran, and so was 127.0.0.1/32.
//
// What that makes the endpoint is the problem. An appliance sits in a plant room with routes a
// laptop does not have, and an unconstrained sweep turns it into a general-purpose port scanner
// pointed anywhere it can reach — outward at whatever is beyond the plant network, or inward at
// its own loopback, where things bind that were never meant to face a network at all. The device
// being scanned never consented to any of it, which is the whole reason the LAN-local claim was
// written down in the first place.
//
// Discovery is not the only way to add a device: anything outside these ranges can still be entered
// by hand, deliberately, with its address typed by somebody who knows what it is.
func Hosts(cidrs []string, max int) ([]string, error) {
	var out []string
	add := func(ip string) error {
		if err := checkLANLocal(ip); err != nil {
			return err
		}
		out = append(out, ip)
		if len(out) > max {
			return fmt.Errorf("scan range too large (> %d hosts)", max)
		}
		return nil
	}
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if err := add(c); err != nil { // a bare host/IP
				return nil, err
			}
			continue
		}
		ip, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("bad CIDR %q: %w", c, err)
		}
		// Check the range's bounds BEFORE expanding it: a /16 of public space should be refused
		// on sight, not after a million strings have been built and thrown away.
		if err := checkLANLocal(ip.Mask(ipnet.Mask).String()); err != nil {
			return nil, err
		}
		for cur := ip.Mask(ipnet.Mask); ipnet.Contains(cur); incIP(cur) {
			if err := add(cur.String()); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// lanRanges is the address space a scan may sweep: the private allocations a site actually numbers
// its equipment out of, plus link-local for gear that came up without DHCP.
//
// Deliberately absent: loopback (the appliance's own interior — nothing to discover there, and
// plenty to reach), and all public space. Modbus has no authentication; equipment reachable on a
// public address is a problem in itself, not a discovery target.
var lanRanges = []string{
	"10.0.0.0/8",     // RFC 1918
	"172.16.0.0/12",  // RFC 1918
	"192.168.0.0/16", // RFC 1918
	"100.64.0.0/10",  // RFC 6598 carrier-grade NAT, used by some site networks
	"169.254.0.0/16", // RFC 3927 link-local: a device that came up with no DHCP server
	"fc00::/7",       // IPv6 unique-local
	"fe80::/10",      // IPv6 link-local
}

var lanNets = func() []*net.IPNet {
	out := make([]*net.IPNet, 0, len(lanRanges))
	for _, r := range lanRanges {
		if _, n, err := net.ParseCIDR(r); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// checkLANLocal refuses a scan target that is not on a local network.
//
// A bare hostname is RESOLVED and every address it yields must qualify, so a name is not a way
// around the rule. Resolution failure is refusal: a target that cannot be checked is not scanned.
func checkLANLocal(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("empty scan target")
	}
	ips := []net.IP{}
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil || len(resolved) == 0 {
			return fmt.Errorf("cannot resolve scan target %q — a target that cannot be checked is not scanned", host)
		}
		ips = resolved
	}
	for _, ip := range ips {
		if !isLANLocal(ip) {
			return fmt.Errorf("refusing to scan %s: a scan is LAN-local, and that address is not on a "+
				"private network (add such a device by hand instead)", ip)
		}
	}
	return nil
}

func isLANLocal(ip net.IP) bool {
	// Loopback is excluded on purpose — see lanRanges. It is neither a LAN nor a place devices live.
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, n := range lanNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
