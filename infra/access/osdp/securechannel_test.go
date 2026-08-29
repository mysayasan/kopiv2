package osdp

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// siteKey is a non-default base key for tests that need a properly rekeyed reader.
var siteKey = SCBK{
	0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7,
	0xB0, 0xB1, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7,
}

// TestSCBKDefaultValue pins the well-known install-mode key. Getting it wrong means every
// factory-fresh reader silently refuses to enrol.
func TestSCBKDefaultValue(t *testing.T) {
	want, _ := hex.DecodeString("303132333435363738393a3b3c3d3e3f")
	if !bytes.Equal(SCBKDefault[:], want) {
		t.Errorf("SCBK-D = %x, want %x", SCBKDefault[:], want)
	}
	if !SCBKDefault.IsDefault() {
		t.Error("SCBKDefault.IsDefault() = false")
	}
	if siteKey.IsDefault() {
		t.Error("a site key reported itself as the default key")
	}
}

// TestKnownAnswerVectors pins the cryptographic constructions so they cannot drift silently.
//
// ⚠️ THESE VECTORS ARE SELF-GENERATED. They were produced by this implementation, not by an
// independent authority, so they prove only that the code has not CHANGED — never that it is
// correct. Both sides of the protocol live in this package and share these primitives, so every
// handshake test in this file passes whenever the two halves agree with each other, including when
// both are wrong in the same way.
//
// Before trusting a real reader, someone must recompute these against libosdp's osdp_sc.c or an
// OSDP Bench capture and replace the constants below with values from that source. Until that
// happens, treat Secure Channel here as structurally complete and cryptographically unconfirmed.
func TestKnownAnswerVectors(t *testing.T) {
	var rndA = [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	var rndB = [8]byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18}

	sEnc, sMac1, sMac2 := deriveSessionKeys(siteKey, rndA)
	cc := clientCryptogram(sEnc, rndA, rndB)
	sc := serverCryptogram(sEnc, rndA, rndB)
	rmac := initialRMAC(sMac1, sMac2, sc)

	for _, c := range []struct{ name, got, want string }{
		{"S-ENC", hex.EncodeToString(sEnc[:]), "cc068c4c82142364865b2fea27e428ae"},
		{"S-MAC1", hex.EncodeToString(sMac1[:]), "9407e84ecc9a58d3ff603ab9af2b66ae"},
		{"S-MAC2", hex.EncodeToString(sMac2[:]), "abe6142bffe8f0657e8f850a173151f9"},
		{"client cryptogram", hex.EncodeToString(cc[:]), "143d08d5eee95269026b110d9128b0d1"},
		{"server cryptogram", hex.EncodeToString(sc[:]), "c99b0267fb1b518b0ad633c53a12486b"},
		{"initial R-MAC", hex.EncodeToString(rmac[:]), "9a75f1fb95daec9069db61ce1e1d93d0"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", c.name, c.got, c.want)
		}
	}

	// The one property here that is structural rather than numeric, and therefore worth asserting
	// regardless of whether the constants are right: the two cryptograms must DIFFER. They are the
	// same function over the same two randoms with the operand order reversed, and if that reversal
	// were lost, each side could replay the other's cryptogram back and authenticate without ever
	// holding the key.
	if bytes.Equal(cc[:], sc[:]) {
		t.Fatal("client and server cryptograms are identical — either side could replay the other's")
	}

	// Likewise: all three session keys must be distinct, or the MAC and encryption keys collapse
	// into one and the MAC stops being independent evidence.
	if bytes.Equal(sEnc[:], sMac1[:]) || bytes.Equal(sEnc[:], sMac2[:]) || bytes.Equal(sMac1[:], sMac2[:]) {
		t.Error("session keys are not distinct — the selector byte is not reaching the derivation")
	}
}

// TestSessionKeysDependOnRandom is the entropy check. If RND.A did not reach the derivation, every
// session on a given base key would use identical session keys and the channel would be replayable
// forever.
func TestSessionKeysDependOnRandom(t *testing.T) {
	a := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	b := a
	b[5] = 0xFF // within the first six bytes, which are the ones the derivation consumes

	e1, m1, n1 := deriveSessionKeys(siteKey, a)
	e2, m2, n2 := deriveSessionKeys(siteKey, b)
	if e1 == e2 || m1 == m2 || n1 == n2 {
		t.Fatal("session keys did not change with RND.A — sessions would be replayable")
	}

	// And a different base key must produce different session keys for the same random.
	other := siteKey
	other[0] ^= 0xFF
	if e3, _, _ := deriveSessionKeys(other, a); e3 == e1 {
		t.Error("session keys did not change with the base key")
	}
}

// TestDiversifySCBK covers per-reader key derivation: two readers differing only in serial must not
// end up sharing a key, or compromising one reader compromises the site.
func TestDiversifySCBK(t *testing.T) {
	master := siteKey
	a := PDInfo{VendorCode: [3]byte{0, 0x1B, 0x1B}, Model: 1, Version: 1, Serial: 1000}
	b := a
	b.Serial = 1001

	ka, kb := DiversifySCBK(master, a), DiversifySCBK(master, b)
	if ka == kb {
		t.Fatal("two readers differing only in serial derived the same key")
	}
	if ka == master {
		t.Error("the derived key equals the master key")
	}
	if ka.IsDefault() || kb.IsDefault() {
		t.Error("a derived key collided with SCBK-D")
	}
}

// TestMACChainsAcrossPackets proves the MAC is not a pure function of the packet. Each direction's
// MAC seeds the other's next IV, so an attacker who records a valid packet cannot replay it later.
func TestMACChainsAcrossPackets(t *testing.T) {
	sEnc, sMac1, sMac2 := deriveSessionKeys(siteKey, [8]byte{9, 8, 7, 6, 5, 4, 3, 2})
	_ = sEnc
	data := []byte{0x53, 0x00, 0x08, 0x00, 0x05, 0x60}

	var iv [16]byte
	first := computeMAC(sMac1, sMac2, iv, data)
	second := computeMAC(sMac1, sMac2, first, data) // same bytes, chain advanced
	if first == second {
		t.Fatal("identical packets produced identical MACs — a recorded packet could be replayed")
	}

	// A one-bit change anywhere in the body must change the MAC.
	altered := append([]byte(nil), data...)
	altered[5] ^= 0x01
	if computeMAC(sMac1, sMac2, iv, altered) == first {
		t.Error("MAC did not change when the payload did")
	}
}

// TestPayloadEncryptionRoundTrip covers the CBC layer, including the padding cases that a
// block-aligned payload and an empty payload each hit differently.
func TestPayloadEncryptionRoundTrip(t *testing.T) {
	sEnc, _, _ := deriveSessionKeys(siteKey, [8]byte{1, 1, 2, 3, 5, 8, 13, 21})
	var mac [16]byte

	cases := [][]byte{
		nil,
		{0x01},
		bytes.Repeat([]byte{0xAB}, 15),
		bytes.Repeat([]byte{0xAB}, 16), // exactly one block: never padded
		bytes.Repeat([]byte{0xAB}, 17),
		bytes.Repeat([]byte{0x00}, 32), // all zeros, the case a naive unpad mangles
	}
	for _, plain := range cases {
		ct := encryptPayload(sEnc, mac, plain)
		if len(plain) > 0 && bytes.Equal(ct, plain) {
			t.Errorf("payload of %d bytes was not encrypted", len(plain))
		}
		// The end-of-message marker is appended UNCONDITIONALLY, so a block-aligned payload grows
		// by a whole block. This is the case that was wrong until the implementation was checked
		// against libosdp: reusing the MAC's conditional padding left a 16-byte payload unpadded,
		// which round-tripped perfectly here and would have been rejected by a real reader.
		if n := len(plain); n > 0 {
			want := (n + 16) &^ 15
			if len(ct) != want {
				t.Errorf("payload of %d bytes encrypted to %d, want %d "+
					"(the 0x80 marker is always appended, even when block-aligned)", n, len(ct), want)
			}
		}
		got, err := decryptPayload(sEnc, mac, ct)
		if err != nil {
			t.Fatalf("decrypt %d bytes: %v", len(plain), err)
		}
		if len(plain) == 0 {
			if len(got) != 0 {
				t.Errorf("empty payload round-tripped to % x", got)
			}
			continue
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("round trip of %d bytes gave % x, want % x", len(plain), got, plain)
		}
	}
}

// TestSealUnsealRoundTrip covers the full packet path and, more importantly, that tampering with
// any byte of a sealed packet is detected.
func TestSealUnsealRoundTrip(t *testing.T) {
	cp, pd := pairedSessions(t)

	f := &Frame{Address: 1, Sequence: 1, Code: byte(CmdOut), Data: []byte{0x00, 0x02, 0x00, 0x00}}
	raw, err := cp.seal(f, true)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(raw, []byte{0x00, 0x02, 0x00, 0x00}) {
		t.Error("the sealed packet still contains its plaintext payload")
	}

	decoded, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("a sealed packet did not decode as a frame: %v", err)
	}
	if !decoded.Secure() {
		t.Fatal("sealed packet carries no security block")
	}
	if scs := scsType(decoded); scs != SCS17 {
		t.Errorf("SCS type = %#02x, want SCS_17 (command with data)", scs)
	}

	plain, err := pd.unseal(raw, decoded, true)
	if err != nil {
		t.Fatalf("unseal: %v", err)
	}
	if !bytes.Equal(plain.Data, f.Data) {
		t.Errorf("payload = % x, want % x", plain.Data, f.Data)
	}
	if plain.Code != f.Code {
		t.Errorf("code = %#02x, want %#02x", plain.Code, f.Code)
	}
}

// TestSealUsesNoDataSCSForEmptyPayload covers why SCS_15/16 could not be dropped from P1: POLL and
// ACK carry no data and are the overwhelming majority of traffic on the bus.
func TestSealUsesNoDataSCSForEmptyPayload(t *testing.T) {
	cp, _ := pairedSessions(t)

	raw, err := cp.seal(&Frame{Address: 1, Sequence: 1, Code: byte(CmdPoll)}, true)
	if err != nil {
		t.Fatalf("seal POLL: %v", err)
	}
	decoded, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if scs := scsType(decoded); scs != SCS15 {
		t.Errorf("SCS type for an empty command = %#02x, want SCS_15", scs)
	}
}

// TestTamperedPacketIsRejected walks a corruption across every byte of a sealed packet. Each one
// must be caught — by the CRC, by the MAC, or by framing — and none may decrypt to something the
// state machine would act on.
func TestTamperedPacketIsRejected(t *testing.T) {
	for i := 0; ; i++ {
		cp, pd := pairedSessions(t)
		raw, err := cp.seal(&Frame{Address: 1, Sequence: 1, Code: byte(CmdOut),
			Data: []byte{0x00, 0x02, 0x00, 0x00}}, true)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		if i >= len(raw) {
			break
		}

		bad := append([]byte(nil), raw...)
		bad[i] ^= 0xFF

		decoded, derr := Unmarshal(bad)
		if derr != nil {
			continue // caught by CRC or framing
		}
		if _, uerr := pd.unseal(bad, decoded, true); uerr == nil {
			t.Fatalf("tampering with byte %d of a sealed packet went undetected", i)
		}
		if !pd.failed() {
			t.Errorf("byte %d: integrity failure did not put the session into the failed state", i)
		}
	}
}

// TestCleartextOnSecureSessionIsRefused is the downgrade attack. Once a session is up there is no
// path back to plaintext — accepting one would let a tap on the pair strip the encryption and have
// the CP carry on as if nothing happened.
func TestCleartextOnSecureSessionIsRefused(t *testing.T) {
	cp, pd := pairedSessions(t)

	plainRaw, err := (&Frame{Address: 1, Sequence: 1, Code: byte(CmdOut),
		Data: []byte{0x00, 0x02, 0x00, 0x00}}).Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := Unmarshal(plainRaw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, err := pd.unseal(plainRaw, decoded, true); err == nil {
		t.Fatal("a cleartext packet was accepted on an established session")
	}
	if !pd.failed() {
		t.Error("a downgrade attempt did not fail the session closed")
	}
	_ = cp
}

// TestWrongKeyFailsHandshake proves the handshake actually authenticates: a peer that does not hold
// the base key cannot complete it.
func TestWrongKeyFailsHandshake(t *testing.T) {
	wrong := siteKey
	wrong[0] ^= 0xFF

	cp := newSecureChannel(siteKey)
	pd := newSecureChannel(wrong)

	chlng, err := cp.challenge(1)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	ccrypt, err := pd.answerCHLNG(1, [8]byte{}, chlng.Data)
	if err != nil {
		t.Fatalf("answerCHLNG: %v", err)
	}
	if _, err := cp.acceptCCRYPT(1, ccrypt.Data); err == nil {
		t.Fatal("the CP accepted a cryptogram computed with the wrong key")
	}
	if !cp.failed() {
		t.Error("a cryptogram mismatch did not fail the session closed")
	}
}

// pairedSessions returns two secureChannels that have completed a handshake with each other.
func pairedSessions(t *testing.T) (cp, pd *secureChannel) {
	t.Helper()
	cp, pd = newSecureChannel(siteKey), newSecureChannel(siteKey)

	chlng, err := cp.challenge(1)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	ccrypt, err := pd.answerCHLNG(1, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, chlng.Data)
	if err != nil {
		t.Fatalf("answerCHLNG: %v", err)
	}
	scrypt, err := cp.acceptCCRYPT(1, ccrypt.Data)
	if err != nil {
		t.Fatalf("acceptCCRYPT: %v", err)
	}
	rmac, err := pd.answerSCRYPT(1, scrypt.Data)
	if err != nil {
		t.Fatalf("answerSCRYPT: %v", err)
	}
	if err := cp.acceptRMACI(rmac.Data); err != nil {
		t.Fatalf("acceptRMACI: %v", err)
	}
	if !cp.established() || !pd.established() {
		t.Fatal("handshake completed without establishing both sides")
	}
	return cp, pd
}

// --- end-to-end over a Bus ---------------------------------------------------------------------

// TestBusEstablishesSecureChannel is the integration path: identify, handshake, then serve badges
// over an encrypted session.
func TestBusEstablishesSecureChannel(t *testing.T) {
	pd := NewPD(1)
	pd.SCBK = siteKey
	key := siteKey
	h := newSecureHarness(t, fastOpts(), PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: true}, pd)

	ev := h.awaitKind(3*time.Second, 1, EventOnline)
	if !ev.SecureSession {
		t.Fatal("reader came online without an established Secure Channel")
	}
	if ev.DefaultKeySession {
		t.Error("a site-keyed session reported itself as running on the default key")
	}
	if !h.bus.Secure(1) {
		t.Error("Bus.Secure reported no session")
	}

	// And ordinary traffic keeps working over the sealed channel.
	want := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	h.with(func(pds []*PD) { pds[0].PresentCard(CardRead{Format: 1, BitCount: 26, Data: want}) })
	card := h.awaitKind(3*time.Second, 1, EventCard)
	if !bytes.Equal(card.Card.Data, want) {
		t.Errorf("card over a secure session = % x, want % x", card.Card.Data, want)
	}
	h.with(func(pds []*PD) {
		if !pds[0].Secure() {
			t.Error("the reader lost its session while serving traffic")
		}
	})
}

// TestBusRefusedSecureChannelFailsClosed is THE security-critical test, and the plan says it is
// precisely the case nobody tests because a real reader will not refuse on request.
//
// A door that requires Secure Channel and cannot get one must go OUT OF SERVICE and alarm. It must
// never quietly fall back to cleartext, because "the door kept working" is how a tapped RS-485
// segment goes unnoticed for a year.
func TestBusRefusedSecureChannelFailsClosed(t *testing.T) {
	pd := NewPD(1)
	pd.SCBK = siteKey
	pd.Faults.RefuseSecureChannel = true
	key := siteKey
	h := newSecureHarness(t, fastOpts(), PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: true}, pd)

	ev := h.awaitKind(3*time.Second, 1, EventOffline)
	if !strings.Contains(ev.Reason, "Secure Channel") {
		t.Errorf("offline reason %q does not name the Secure Channel failure", ev.Reason)
	}
	if !strings.Contains(ev.Reason, "no cleartext fallback") {
		t.Errorf("offline reason %q does not state the fail-closed posture", ev.Reason)
	}
	if got := h.bus.Status(1); got != StatusOffline {
		t.Errorf("status = %s, want offline", got)
	}
	if h.bus.Secure(1) {
		t.Error("Bus.Secure reported a session on a reader that refused the handshake")
	}

	// The reader must NOT be serving traffic. Present a card; it must not reach the application,
	// because a badge answered over a channel we required to be encrypted is the exact failure.
	h.with(func(pds []*PD) { pds[0].PresentCard(CardRead{Format: 1, BitCount: 26, Data: []byte{0x01}}) })
	select {
	case ev := <-h.bus.Events():
		if ev.Kind == EventCard {
			t.Fatal("a badge was served over a refused Secure Channel — this is the downgrade failure")
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// TestBusSecureChannelDropMidSessionFailsClosed is the harder half: a reader that is already
// trusted, already bound to a live door, and loses its session mid-conversation.
func TestBusSecureChannelDropMidSessionFailsClosed(t *testing.T) {
	pd := NewPD(1)
	pd.SCBK = siteKey
	key := siteKey
	opts := fastOpts()
	h := newSecureHarness(t, opts, PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: true}, pd)

	if ev := h.awaitKind(3*time.Second, 1, EventOnline); !ev.SecureSession {
		t.Fatal("setup: reader did not come up secure")
	}

	h.with(func(pds []*PD) { pds[0].Faults.DropSecureChannel = true })

	h.await(4*time.Second, "the reader going out of service", func(e Event) bool {
		return e.Kind == EventOffline && e.Address == 1
	})
	if h.bus.Secure(1) {
		t.Error("Bus.Secure still reports a session after it dropped")
	}
	if st := h.bus.Stats()[1]; st.Offlines == 0 {
		t.Error("a dropped session was not counted as taking the reader out of service")
	}
}

// TestBusFlappingSecureChannelIsDamped covers a reader that establishes a session and immediately
// loses it — a failing transceiver, or someone on the pair.
//
// Without backoff it re-handshakes every slot and flaps online/offline about ten times a second,
// burying the audit log and thrashing the door in and out of service. Live-running the sc-drop
// scenario is what surfaced it; the in-process tests had all passed.
func TestBusFlappingSecureChannelIsDamped(t *testing.T) {
	pd := NewPD(1)
	pd.SCBK = siteKey
	key := siteKey
	opts := fastOpts()
	opts.SecureRetryBackoff = 150 * time.Millisecond
	opts.SecureRetryMax = time.Second
	h := newSecureHarness(t, opts, PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: true}, pd)

	if ev := h.awaitKind(3*time.Second, 1, EventOnline); !ev.SecureSession {
		t.Fatal("setup: reader did not come up secure")
	}
	h.with(func(pds []*PD) { pds[0].Faults.DropSecureChannel = true })

	// Assert the GAPS GROW, not merely that there are few events.
	//
	// A transition count alone is too blunt: an earlier version of this test counted events and
	// passed while the damping was in fact bypassed entirely on the silent-drop path, because the
	// offline-detection delay happened to be close to the backoff. Recording when each
	// re-establishment lands and checking the intervals actually double is what distinguishes real
	// exponential backoff from a fixed retry interval.
	var onlineAt []time.Time
	deadline := time.After(3 * time.Second)
	for len(onlineAt) < 4 {
		select {
		case ev, ok := <-h.bus.Events():
			if !ok {
				t.Fatal("event channel closed")
			}
			if ev.Kind == EventOnline {
				onlineAt = append(onlineAt, ev.At)
			}
		case <-deadline:
			t.Fatalf("only %d re-establishments in 3s; expected at least 4 to measure the backoff", len(onlineAt))
		}
	}

	gaps := make([]time.Duration, 0, len(onlineAt)-1)
	for i := 1; i < len(onlineAt); i++ {
		gaps = append(gaps, onlineAt[i].Sub(onlineAt[i-1]))
	}
	// Each gap should be meaningfully longer than the last. Allow slack for scheduling, but a
	// flat sequence means the backoff is not being applied on whichever path this reader took.
	for i := 1; i < len(gaps); i++ {
		if gaps[i] < gaps[i-1]*3/2 {
			t.Errorf("re-establishment gaps are not backing off: %v — a flapping session would "+
				"thrash the door and bury the audit log", gaps)
			break
		}
	}
}

// TestSilentDropAppliesSecureBackoff pins the specific leak the flap test could not see.
//
// A reader that dies SILENTLY mid-session never reaches secureChannelLost — it is the ordinary
// timeout path that notices — so the backoff has to be applied there too. Without it a dropping
// reader re-handshakes on the very next slot and flaps as fast as the bus will carry it.
func TestSilentDropAppliesSecureBackoff(t *testing.T) {
	opts := fastOpts().Defaults()
	opts.SecureRetryBackoff = time.Second
	opts.SecureRetryMax = 10 * time.Second

	b := NewBus(nopTransport{}, opts)
	key := siteKey
	if err := b.AddPD(PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: true}); err != nil {
		t.Fatalf("AddPD: %v", err)
	}
	pd := b.pds[1]

	// A reader that was never up must NOT be delayed — it has to be free to enrol the moment it
	// appears, which is why the backoff is conditional on having HAD a session.
	b.fail(pd, "no reply")
	b.fail(pd, "no reply")
	b.fail(pd, "no reply")
	if !pd.scRetryAt.IsZero() {
		t.Error("an absent reader was put into secure-channel backoff; it would be delayed on enrolment")
	}

	// Now give it an established session and let it go silent.
	pd.failures, pd.announced, pd.status = 0, false, StatusOnline
	pd.sc = newSecureChannel(key)
	pd.sc.state = scActive
	for range opts.OfflineAfter {
		b.fail(pd, "no reply")
	}
	if pd.scRetryAt.IsZero() {
		t.Fatal("an established session that went silent did not apply the secure-channel backoff")
	}
	// Re-identification comes first on recovery — going offline deliberately clears the identity —
	// so stand that back up before checking the time gate in isolation.
	pd.info, pd.caps = PDInfo{Serial: 1}, []Capability{{Function: CapCommSecurity, Compliance: CommSecAES128}}

	if pd.wantsSecureChannel(time.Now()) {
		t.Error("the CP would re-handshake immediately after a silent drop")
	}
	if !pd.wantsSecureChannel(pd.scRetryAt.Add(time.Millisecond)) {
		t.Error("the CP would never retry after the backoff expired")
	}
}

// TestSecureRetryDelay pins the backoff curve itself.
func TestSecureRetryDelay(t *testing.T) {
	base, max := 500*time.Millisecond, 4*time.Second
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{5, 4 * time.Second}, // capped
		{50, 4 * time.Second},
	}
	for _, c := range cases {
		if got := secureRetryDelay(base, max, c.failures); got != c.want {
			t.Errorf("secureRetryDelay(%d) = %s, want %s", c.failures, got, c.want)
		}
	}
}

// TestBusSecureChannelOptionalDegradesButWarns covers the `interior` door case: Secure Channel is
// wanted but not required, so a refusal is a fault rather than an outage.
func TestBusSecureChannelOptionalDegradesButWarns(t *testing.T) {
	pd := NewPD(1)
	pd.SCBK = siteKey
	pd.Faults.RefuseSecureChannel = true
	key := siteKey
	h := newSecureHarness(t, fastOpts(), PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: false}, pd)

	ev := h.await(3*time.Second, "a secure-channel fault", func(e Event) bool {
		return e.Kind == EventFault && strings.Contains(e.Reason, "Secure Channel")
	})
	if !strings.Contains(ev.Reason, "continuing in the clear") {
		t.Errorf("fault reason %q does not say the reader degraded to cleartext", ev.Reason)
	}
	// The other half of the classification: marking the protocol faults must not demote the
	// security ones, which are the reason a secure-channel alarm exists at all.
	if ev.Fault != FaultSecureChannel {
		t.Errorf("secure-channel fault classified as %s, want secure-channel", ev.Fault)
	}

	// It must still come online and serve, because this door did not require encryption.
	online := h.awaitKind(3*time.Second, 1, EventOnline)
	if online.SecureSession {
		t.Error("reported a secure session after the handshake was refused")
	}
	if st := h.bus.Stats()[1]; st.SecureFailures == 0 {
		t.Error("the secure-channel failure was not counted")
	}
}

// TestBusDefaultKeySessionIsFlagged covers the trust cap. A session on SCBK-D is cryptographically
// real and worth nothing as a trust signal, so it must be distinguishable from a site-keyed one.
func TestBusDefaultKeySessionIsFlagged(t *testing.T) {
	pd := NewPD(1) // ships on SCBK-D
	key := SCBKDefault
	h := newSecureHarness(t, fastOpts(), PDConfig{Address: 1, SCBK: &key, RequireSecureChannel: true}, pd)

	ev := h.awaitKind(3*time.Second, 1, EventOnline)
	if !ev.SecureSession {
		t.Fatal("the SCBK-D handshake did not complete")
	}
	if !ev.DefaultKeySession {
		t.Error("a session built on SCBK-D was not flagged — it would be treated as trustworthy")
	}
}

// TestKeysetRequiresSecureSession is the rekey rule. Installing a base key in the clear would
// broadcast it to anyone tapping the pair — and a reader on SCBK-D is exactly the one about to be
// asked to rekey.
func TestKeysetRequiresSecureSession(t *testing.T) {
	newKey := bytes.Repeat([]byte{0x5A}, 16)
	keyset := append([]byte{0x01, 0x10}, newKey...)

	t.Run("refused in the clear", func(t *testing.T) {
		h := newHarness(t, fastOpts(), NewPD(1))
		h.awaitKind(2*time.Second, 1, EventOnline)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := h.bus.Send(ctx, 1, CmdKeySet, keyset...); err == nil {
			t.Fatal("KEYSET succeeded on a cleartext session — the site key would go out in plaintext")
		}
	})

	t.Run("accepted inside a session, and the old session dies with it", func(t *testing.T) {
		pd := NewPD(1)
		key := SCBKDefault
		h := newSecureHarness(t, fastOpts(), PDConfig{Address: 1, SCBK: &key}, pd)
		h.awaitKind(3*time.Second, 1, EventOnline)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := h.bus.Send(ctx, 1, CmdKeySet, keyset...); err != nil {
			t.Fatalf("KEYSET inside a secure session: %v", err)
		}
		h.with(func(pds []*PD) {
			if !bytes.Equal(pds[0].SCBK[:], newKey) {
				t.Errorf("reader key = % x, want % x", pds[0].SCBK[:], newKey)
			}
			if pds[0].Secure() {
				t.Error("the reader kept a session that was built on the OLD key")
			}
		})
	})
}
