package apphost

import (
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
)

// This file answers the first question anyone has after starting the app: what do I
// type into a browser?
//
// It used to be answerable only by reading config.json, because the only thing printed
// was a structured log line per listener ("starting https server on :3002") — which
// names a PORT but no scheme and no host, and on a wildcard bind ":3002" is not an
// address a person can visit at all. The apps did print a friendly URL, but only on the
// very FIRST run, as a side effect of announcing the bootstrap password; every boot
// afterwards said nothing.
//
// So the host now prints one banner on every boot, saying where to browse — and opens it.

// anyTLS reports whether any listener serves HTTPS, which decides whether the
// certificate warning in the banner is relevant at all.
func anyTLS(listeners []listenerSpec) bool {
	for _, listener := range listeners {
		if listener.UseTLS {
			return true
		}
	}
	return false
}

// readyAddresses is the banner's content, grouped the way an operator reads it rather
// than as one flat list.
//
// The grouping is the point. A wildcard bind on a developer or appliance box expands to
// every interface — a VPN tunnel, a WSL bridge, a VirtualBox host-only adapter — and
// with two listeners that is ten URLs, nine of which are useless. Ten lines of noise
// hides the answer as effectively as printing nothing did. So the ONE address that
// reaches this app from another machine is named on its own, and the leftover
// interfaces are mentioned as bare addresses instead of being multiplied by ports.
type readyAddresses struct {
	// Primary is the URL to open on this machine, and the one handed to the browser.
	Primary string
	// Local is every URL that works on this machine, Primary first.
	Local []string
	// Network is every URL that works from another machine, via the interface that
	// carries this host's default route.
	Network []string
	// OtherIPs are the remaining local interface addresses, bare. They serve the same
	// listeners; they are just rarely the ones anybody wants.
	OtherIPs []string
}

// resolveReadyAddresses works out what to tell the operator, from the listeners that
// were actually started.
//
// HTTPS comes first because a listener configured for TLS is the one the operator meant
// to use, and localhost comes first because it is the only address guaranteed to work
// from the machine the banner is printed on.
func resolveReadyAddresses(listeners []listenerSpec) readyAddresses {
	primaryIP := primaryNetworkIP()
	otherIPs := map[string]bool{}

	var localTLS, localPlain, netTLS, netPlain []string
	seen := map[string]bool{}

	add := func(list *[]string, scheme, host, port string) {
		url := scheme + "://" + net.JoinHostPort(host, port)
		if seen[url] {
			return
		}
		seen[url] = true
		*list = append(*list, url)
	}

	for _, listener := range listeners {
		host, port, err := net.SplitHostPort(listener.Addr)
		if err != nil {
			continue
		}
		scheme := "http"
		localList, netList := &localPlain, &netPlain
		if listener.UseTLS {
			scheme = "https"
			localList, netList = &localTLS, &netTLS
		}

		switch host {
		case "", "0.0.0.0", "::", "[::]":
			// A wildcard bind answers everywhere, which is exactly the case where the
			// config cannot tell the operator what to type.
			add(localList, scheme, "localhost", port)
			for _, ip := range localIPv4s() {
				if ip == primaryIP {
					add(netList, scheme, ip, port)
					continue
				}
				otherIPs[ip] = true
			}
		case "127.0.0.1", "::1", "localhost":
			// Loopback-only: advertising a LAN address here would send the operator
			// to a port nothing is listening on.
			add(localList, scheme, "localhost", port)
		default:
			// An explicit hostname is what the operator meant; never expand it.
			add(netList, scheme, host, port)
		}
	}

	out := readyAddresses{
		Local:   append(append([]string{}, localTLS...), localPlain...),
		Network: append(append([]string{}, netTLS...), netPlain...),
	}
	for ip := range otherIPs {
		out.OtherIPs = append(out.OtherIPs, ip)
	}
	sort.Strings(out.OtherIPs)

	switch {
	case len(out.Local) > 0:
		out.Primary = out.Local[0]
	case len(out.Network) > 0:
		out.Primary = out.Network[0]
	}
	return out
}

// primaryNetworkIP returns the IPv4 address of the interface carrying this host's
// default route — the address another machine on the network would actually reach.
//
// The UDP "dial" sends nothing: for a connectionless socket the kernel only performs a
// route lookup and binds a local address, which is the whole point. The target is an
// RFC 5737 documentation address rather than a real public resolver, so nothing here
// depends on, or even names, an outside service — the air-gapped installs must stay
// air-gapped. With no default route (an isolated appliance) this returns "" and the
// banner simply lists the interfaces instead.
func primaryNetworkIP() string {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return ""
	}
	ip := addr.IP.To4()
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// localIPv4s lists this machine's routable IPv4 addresses, in a stable order. IPv6 is
// left out on purpose: a bracketed IPv6 literal is not something anyone types into a
// browser bar, and the IPv4 address reaches the same listener.
func localIPv4s() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	sort.Strings(out)
	return out
}

// announceReady prints the "here is where to browse" banner and, on an interactive
// desktop run, opens the primary URL in a browser. It returns the primary URL.
//
// The banner goes to stdout rather than through the logger for the same reason the
// first-run credential banner does: it must land verbatim in `docker logs`, in a
// journal, and in a terminal, not as one JSON line among hundreds.
func announceReady(appName string, listeners []listenerSpec, selfSignedTLS bool) string {
	addresses := resolveReadyAddresses(listeners)
	if addresses.Primary == "" {
		// Nothing we can describe. Say nothing rather than print an empty banner; the
		// per-listener log lines above still record what was started.
		return ""
	}

	const bar = "======================================================================"
	const indent = "                     "
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", bar)
	fmt.Fprintf(&b, "  %s is running.\n\n", appName)

	writeGroup := func(label string, urls []string) {
		for i, url := range urls {
			if i == 0 {
				fmt.Fprintf(&b, "  %-18s %s\n", label, url)
				continue
			}
			fmt.Fprintf(&b, "%s%s\n", indent, url)
		}
	}
	writeGroup("On this machine:", addresses.Local)
	if len(addresses.Network) > 0 {
		fmt.Fprintln(&b)
		writeGroup("From the network:", addresses.Network)
	}
	if len(addresses.OtherIPs) > 0 {
		fmt.Fprintf(&b, "\n  Also listening on %s\n", strings.Join(addresses.OtherIPs, ", "))
	}

	if selfSignedTLS {
		fmt.Fprint(&b, "\n  This install uses its own generated HTTPS certificate, so the browser\n")
		fmt.Fprint(&b, "  will warn that the connection is not private the first time. That is\n")
		fmt.Fprint(&b, "  expected here — continue past the warning, or install your own\n")
		fmt.Fprint(&b, "  certificate and point tls.certPath / tls.keyPath at it.\n")
	}

	// The browser is launched before the banner prints so the banner can state what
	// actually happened rather than what was merely intended.
	opened, reason := launchBrowser(addresses.Primary)
	switch {
	case opened:
		fmt.Fprint(&b, "\n  Opening it in your browser now.\n")
	case reason != "":
		fmt.Fprintf(&b, "\n  (Not opening a browser: %s.)\n", reason)
	}
	fmt.Fprintf(&b, "%s\n", bar)
	fmt.Print(b.String())
	// Also one structured line, so the URL is greppable in a log file long after the
	// banner has scrolled out of a terminal.
	log.Printf("%s ready at %s", appName, addresses.Primary)
	return addresses.Primary
}
