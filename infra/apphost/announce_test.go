package apphost

import (
	"strings"
	"testing"
)

// The whole point of the banner is that ":3002" is not an address anyone can type. A
// wildcard bind must be expanded into something browsable, with the scheme attached.
func TestResolveReadyAddressesExpandsAWildcardBind(t *testing.T) {
	got := resolveReadyAddresses([]listenerSpec{{Addr: ":3002", UseTLS: true}})
	if got.Primary != "https://localhost:3002" {
		t.Fatalf("Primary = %q, want the localhost HTTPS URL", got.Primary)
	}
	for _, url := range append(append([]string{}, got.Local...), got.Network...) {
		if strings.HasPrefix(url, "https://:") || strings.Contains(url, "//0.0.0.0:") {
			t.Fatalf("an unbrowsable address reached the banner: %q", url)
		}
	}
}

// HTTPS is what the operator configured when a TLS listener exists, so that is the URL
// to hand them first — handing them the http one would look like a broken redirect.
func TestResolveReadyAddressesPrefersTLSAndLocalhost(t *testing.T) {
	got := resolveReadyAddresses([]listenerSpec{
		{Addr: "0.0.0.0:8080", UseTLS: false},
		{Addr: "0.0.0.0:8443", UseTLS: true},
	})
	if got.Primary != "https://localhost:8443" {
		t.Fatalf("Primary = %q", got.Primary)
	}
	// The plain listener must still be listed — an operator whose browser refuses the
	// self-signed certificate needs to know the http port exists.
	plain := false
	for _, url := range got.Local {
		if strings.HasPrefix(url, "http://") {
			plain = true
		}
	}
	if !plain {
		t.Fatalf("the non-TLS listener was dropped from %v", got.Local)
	}
}

// A configured hostname is what the operator meant; it must not be replaced by this
// machine's interface addresses, and it is reachable from elsewhere, not from localhost.
func TestResolveReadyAddressesKeepsAnExplicitHostname(t *testing.T) {
	got := resolveReadyAddresses([]listenerSpec{{Addr: "fleet.internal:3002", UseTLS: true}})
	if got.Primary != "https://fleet.internal:3002" {
		t.Fatalf("Primary = %q", got.Primary)
	}
	if len(got.Network) != 1 || got.Network[0] != "https://fleet.internal:3002" {
		t.Fatalf("an explicit hostname must not be expanded: %v", got.Network)
	}
	if len(got.Local) != 0 {
		t.Fatalf("an explicit hostname is not a localhost address: %v", got.Local)
	}
}

func TestResolveReadyAddressesWithNoListeners(t *testing.T) {
	got := resolveReadyAddresses(nil)
	if got.Primary != "" || len(got.Local) != 0 || len(got.Network) != 0 {
		t.Fatalf("got %+v, want nothing to announce", got)
	}
}

// A loopback-only bind is reachable at localhost and nowhere else; listing a LAN address
// there would send the operator to a port nothing is listening on.
func TestResolveReadyAddressesDoesNotAdvertiseLANForALoopbackBind(t *testing.T) {
	got := resolveReadyAddresses([]listenerSpec{{Addr: "127.0.0.1:3002", UseTLS: false}})
	if len(got.Local) != 1 || got.Local[0] != "http://localhost:3002" {
		t.Fatalf("loopback bind announced as %v", got.Local)
	}
	if len(got.Network) != 0 || len(got.OtherIPs) != 0 {
		t.Fatalf("a loopback bind must advertise no network address: %+v", got)
	}
}

// The grouping exists so the useful address is not buried: a box with a VPN, a WSL
// bridge and a VirtualBox adapter has four IPv4s, and two listeners would otherwise
// print ten URLs. Every interface still appears — the spare ones as bare addresses, once
// each, rather than multiplied by every port.
func TestResolveReadyAddressesDoesNotMultiplySpareInterfacesByPorts(t *testing.T) {
	got := resolveReadyAddresses([]listenerSpec{
		{Addr: ":8443", UseTLS: true},
		{Addr: ":8080", UseTLS: false},
	})
	if len(got.Local) != 2 {
		t.Fatalf("both listeners should be reachable on this machine: %v", got.Local)
	}
	if len(got.Network) > 2 {
		t.Fatalf("the network group must name one address per listener, got %v", got.Network)
	}
	for _, ip := range got.OtherIPs {
		if strings.Contains(ip, ":") || strings.Contains(ip, "/") {
			t.Fatalf("spare interfaces must be bare addresses, got %q", ip)
		}
	}
	// A spare interface must never also appear as a full URL, or the noise is back.
	for _, url := range got.Network {
		for _, ip := range got.OtherIPs {
			if strings.Contains(url, "//"+ip+":") {
				t.Fatalf("%s is listed both as a URL and as a spare interface", ip)
			}
		}
	}
}

func TestAnyTLS(t *testing.T) {
	if anyTLS([]listenerSpec{{Addr: ":80", UseTLS: false}}) {
		t.Fatal("no TLS listener, but anyTLS said yes")
	}
	if !anyTLS([]listenerSpec{{Addr: ":80", UseTLS: false}, {Addr: ":443", UseTLS: true}}) {
		t.Fatal("a TLS listener was missed")
	}
}

/* ---------- when a browser may be opened ---------- */

// Every guard here exists because opening a browser in that situation is wrong, not
// merely useless: a service has no desktop, a container has no browser, and a restart
// after a settings change would pop a new tab every single save.
func TestBrowserIsSuppressedWhereItWouldBeWrong(t *testing.T) {
	t.Run("explicitly off", func(t *testing.T) {
		withCleanBrowserEnv(t)
		t.Setenv("KOPIV2_OPEN_BROWSER", "0")
		if reason := browserSuppressedBecause(); reason == "" {
			t.Fatal("KOPIV2_OPEN_BROWSER=0 must suppress the browser")
		}
	})
	t.Run("no-browser flag", func(t *testing.T) {
		withCleanBrowserEnv(t)
		t.Setenv("KOPIV2_NO_BROWSER", "1")
		if reason := browserSuppressedBecause(); reason == "" {
			t.Fatal("KOPIV2_NO_BROWSER must suppress the browser")
		}
	})
	t.Run("under a supervisor", func(t *testing.T) {
		withCleanBrowserEnv(t)
		t.Setenv("KOPIV2_SUPERVISED", "1")
		if reason := browserSuppressedBecause(); reason == "" {
			t.Fatal("a supervised install must not open a browser")
		}
	})
	t.Run("a self-restart", func(t *testing.T) {
		withCleanBrowserEnv(t)
		original := relaunchedByRestart
		relaunchedByRestart = true
		t.Cleanup(func() { relaunchedByRestart = original })
		if reason := browserSuppressedBecause(); reason == "" {
			t.Fatal("a restart must not reopen a browser tab")
		}
	})
}

// An operator in an unusual setup (X forwarding, a remote desktop) knows better than
// these heuristics, so an explicit yes has to win.
func TestExplicitOptInOverridesTheHeuristics(t *testing.T) {
	withCleanBrowserEnv(t)
	t.Setenv("KOPIV2_SUPERVISED", "1")
	t.Setenv("KOPIV2_OPEN_BROWSER", "1")
	if reason := browserSuppressedBecause(); reason != "" {
		t.Fatalf("explicit opt-in was overruled: %s", reason)
	}
}

// launchBrowser must report a launcher failure rather than claim success, so the banner
// never says "opening it in your browser now" when nothing opened.
func TestLaunchBrowserReportsWhatHappened(t *testing.T) {
	withCleanBrowserEnv(t)
	t.Setenv("KOPIV2_OPEN_BROWSER", "1")

	var got string
	original := browserOpener
	t.Cleanup(func() { browserOpener = original })

	browserOpener = func(url string) error { got = url; return nil }
	opened, reason := launchBrowser("https://localhost:3002")
	if !opened || reason != "" {
		t.Fatalf("opened=%v reason=%q", opened, reason)
	}
	if got != "https://localhost:3002" {
		t.Fatalf("launcher got %q", got)
	}

	browserOpener = func(string) error { return errTestLauncher }
	opened, reason = launchBrowser("https://localhost:3002")
	if opened {
		t.Fatal("a failed launcher must not report success")
	}
	if !strings.Contains(reason, "could not start a browser") {
		t.Fatalf("reason = %q", reason)
	}
}

var errTestLauncher = errTest("no browser here")

type errTest string

func (e errTest) Error() string { return string(e) }

// withCleanBrowserEnv clears every variable the guards read, so one subtest's setting
// cannot decide another's outcome.
func withCleanBrowserEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"KOPIV2_OPEN_BROWSER", "KOPIV2_NO_BROWSER", "KOPIV2_SUPERVISED", "KUBERNETES_SERVICE_HOST"} {
		t.Setenv(key, "")
	}
}
