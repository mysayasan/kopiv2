package services

import "testing"

// The protocol guard. `Protocol` is read by no code anywhere, which is precisely why an
// unsupported value has to be refused at the door: nothing downstream would ever object, and the
// device it creates is provisioned, enabled and permanently silent with no error at any layer.

func TestSupportedProtocols_RejectsATransportWithNoRouteBehindIt(t *testing.T) {
	// "http" was offered by the Add-device form for a long time. This app has never had an HTTP
	// ingest route, so a device created with it could never report anything, ever.
	if supportedProtocols["http"] {
		t.Fatal("http must not be a supported protocol while there is no HTTP ingest route")
	}
}

func TestSupportedProtocols_KeepsTheTwoThatWork(t *testing.T) {
	for _, name := range []string{"mqtt", "modbus"} {
		if !supportedProtocols[name] {
			t.Fatalf("%q is a real transport and must stay supported", name)
		}
	}
}

func TestSupportedProtocolList_IsStableAndReadable(t *testing.T) {
	// It goes into the error an operator reads, so it must be sorted rather than map-ordered.
	got := supportedProtocolList()
	if got != "modbus or mqtt" {
		t.Fatalf("protocol list should be sorted and human-readable, got %q", got)
	}
}
