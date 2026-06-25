package pairing

import (
	"testing"
	"time"
)

var (
	testKey   = []byte("fleet-key-aaaaaaaaaaaaaaaaaaaaaa")
	otherKey  = []byte("fleet-key-bbbbbbbbbbbbbbbbbbbbbb")
	testInfo  = AnnounceInfo{NodeID: "node-1", Name: "Lobby NVR", Version: "1.2.3", HTTPSPort: 8443}
	defWindow = DefaultReplayWindow
)

func TestProbeRoundTrip(t *testing.T) {
	p, err := NewProbe(testKey)
	if err != nil {
		t.Fatalf("NewProbe: %v", err)
	}
	got, err := ParseProbe(p.Marshal(), testKey, defWindow)
	if err != nil {
		t.Fatalf("ParseProbe: %v", err)
	}
	if got.Nonce != p.Nonce {
		t.Fatalf("nonce: got %q want %q", got.Nonce, p.Nonce)
	}
}

func TestProbeRejectsWrongKey(t *testing.T) {
	p, _ := NewProbe(testKey)
	if _, err := ParseProbe(p.Marshal(), otherKey, defWindow); err != errBadHMAC {
		t.Fatalf("wrong key: got %v want errBadHMAC", err)
	}
}

func TestProbeRejectsTamper(t *testing.T) {
	p, _ := NewProbe(testKey)
	p.Nonce = "tampered" // changes signed content but keeps the old HMAC
	if _, err := ParseProbe(p.Marshal(), testKey, defWindow); err != errBadHMAC {
		t.Fatalf("tamper: got %v want errBadHMAC", err)
	}
}

func TestProbeRejectsStale(t *testing.T) {
	p, _ := NewProbe(testKey)
	p.Timestamp = time.Now().Add(-time.Hour).Unix()
	p.HMAC = sign(testKey, p.signingParts()...) // re-sign so only freshness fails
	if _, err := ParseProbe(p.Marshal(), testKey, defWindow); err != errStale {
		t.Fatalf("stale: got %v want errStale", err)
	}
}

func TestAnnounceRoundTrip(t *testing.T) {
	a, err := NewAnnounce(testKey, "nonce-xyz", testInfo)
	if err != nil {
		t.Fatalf("NewAnnounce: %v", err)
	}
	got, err := ParseAnnounce(a.Marshal(), testKey, defWindow, "nonce-xyz")
	if err != nil {
		t.Fatalf("ParseAnnounce: %v", err)
	}
	if got.NodeID != testInfo.NodeID || got.HTTPSPort != testInfo.HTTPSPort {
		t.Fatalf("fields not preserved: %+v", got)
	}
}

func TestAnnounceRejectsNonceMismatch(t *testing.T) {
	a, _ := NewAnnounce(testKey, "server-nonce", testInfo)
	if _, err := ParseAnnounce(a.Marshal(), testKey, defWindow, "different-nonce"); err != errNonceMismatch {
		t.Fatalf("nonce mismatch: got %v want errNonceMismatch", err)
	}
}

func TestAnnounceRejectsWrongKey(t *testing.T) {
	a, _ := NewAnnounce(testKey, "n", testInfo)
	if _, err := ParseAnnounce(a.Marshal(), otherKey, defWindow, "n"); err != errBadHMAC {
		t.Fatalf("wrong key: got %v want errBadHMAC", err)
	}
}

func TestNonceCacheDetectsReplay(t *testing.T) {
	c := newNonceCache(time.Minute)
	if c.seenBefore("abc") {
		t.Fatal("first sight should not be a replay")
	}
	if !c.seenBefore("abc") {
		t.Fatal("second sight should be a replay")
	}
	if c.seenBefore("def") {
		t.Fatal("a different nonce should not be a replay")
	}
}

func TestNonceCacheEvictsExpired(t *testing.T) {
	c := newNonceCache(10 * time.Millisecond)
	c.seenBefore("abc")
	time.Sleep(25 * time.Millisecond)
	if c.seenBefore("abc") {
		t.Fatal("expired nonce should no longer count as a replay")
	}
}

// newTestResponder builds a responder whose discoverability is controlled by the
// returned pointer so tests can flip the paired/unpaired gate.
func newTestResponder(discoverable *bool) *Responder {
	return NewResponder(ResponderConfig{
		FleetKey:     func() []byte { return testKey },
		Discoverable: func() bool { return *discoverable },
		AnnounceInfo: func() AnnounceInfo { return testInfo },
	})
}

func TestResponderRepliesToValidProbe(t *testing.T) {
	on := true
	r := newTestResponder(&on)
	p, _ := NewProbe(testKey)
	out, ok := r.reply(p.Marshal())
	if !ok {
		t.Fatal("valid probe should get a reply")
	}
	ann, err := ParseAnnounce(out, testKey, defWindow, p.Nonce)
	if err != nil {
		t.Fatalf("reply not a valid announce: %v", err)
	}
	if ann.NodeID != testInfo.NodeID {
		t.Fatalf("announce node id: got %q want %q", ann.NodeID, testInfo.NodeID)
	}
}

func TestResponderSilentWhenPaired(t *testing.T) {
	on := false // node is paired → not discoverable
	r := newTestResponder(&on)
	p, _ := NewProbe(testKey)
	if _, ok := r.reply(p.Marshal()); ok {
		t.Fatal("paired node must not answer probes")
	}
}

func TestResponderSilentOnWrongKey(t *testing.T) {
	on := true
	r := newTestResponder(&on)
	p, _ := NewProbe(otherKey) // probe signed with a key the node doesn't share
	if _, ok := r.reply(p.Marshal()); ok {
		t.Fatal("probe from an unknown key must be ignored")
	}
}

func TestResponderSilentOnReplay(t *testing.T) {
	on := true
	r := newTestResponder(&on)
	p, _ := NewProbe(testKey)
	if _, ok := r.reply(p.Marshal()); !ok {
		t.Fatal("first probe should be answered")
	}
	if _, ok := r.reply(p.Marshal()); ok {
		t.Fatal("replayed probe (same nonce) must be ignored")
	}
}

func TestResponderSilentWithoutFleetKey(t *testing.T) {
	on := true
	r := NewResponder(ResponderConfig{
		FleetKey:     func() []byte { return nil },
		Discoverable: func() bool { return on },
		AnnounceInfo: func() AnnounceInfo { return testInfo },
	})
	p, _ := NewProbe(testKey)
	if _, ok := r.reply(p.Marshal()); ok {
		t.Fatal("a node without a fleet key must never answer")
	}
}
