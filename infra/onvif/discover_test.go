package onvif

import (
	"context"
	"net"
	"testing"
	"time"
)

// A workstation typically has adapters that cannot carry a multicast probe (VirtualBox
// host-only, WSL/Hyper-V vEthernet, a link-local 169.254.x fallback). Letting one of
// those decide the scan's outcome is what turned "no cameras found" into a 500 on the
// wizard's "Add your first camera" step and on the Discover screen.
func TestShouldFailDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		found, started, failed int
		want                   bool
	}{
		{"nothing found, every interface refused the probe", 0, 3, 3, true},
		{"nothing found, one dead adapter among three", 0, 3, 1, false},
		{"nothing found, everything sent fine — an empty subnet", 0, 3, 0, false},
		{"a camera answered even though an adapter failed", 1, 3, 2, false},
		{"a camera answered and every send failed (cannot happen, but is not an error)", 2, 2, 2, false},
		{"no interface was usable at all", 0, 0, 0, false},
	} {
		if got := shouldFailDiscovery(tc.found, tc.started, tc.failed); got != tc.want {
			t.Errorf("%s: shouldFailDiscovery(%d,%d,%d) = %v; want %v",
				tc.name, tc.found, tc.started, tc.failed, got, tc.want)
		}
	}
}

// An empty subnet must answer "nothing found", not an error: this is the first thing a
// fresh install does, before any camera is plugged in.
func TestDiscoverFindsNothingWithoutErroring(t *testing.T) {
	// Point discovery at a loopback port nothing answers on, so every send succeeds and
	// no ProbeMatch ever comes back.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Skipf("no loopback UDP available: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	c := NewClient()
	c.DiscoveryAddress = addr
	devices, err := c.Discover(context.Background(), 300*time.Millisecond)
	if err != nil {
		t.Fatalf("a scan that finds nothing must not be an error, got: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected no devices, got %d", len(devices))
	}
}
