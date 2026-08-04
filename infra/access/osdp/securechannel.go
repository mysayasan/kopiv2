package osdp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
)

// OSDP Secure Channel: AES-128 session establishment, per-packet MAC, and payload encryption.
//
// ─── VERIFICATION STATUS ──────────────────────────────────────────────────────────────────────
//
// The cryptographic constructions below were CHECKED LINE BY LINE against libosdp's osdp_sc.c and
// osdp_phy.c (goToMain/libosdp, master, read 2026-08-04). Confirmed identical:
//
//   - SCBK-D                     0x30…0x3F
//   - session-key derivation     0x01,{0x82|0x01|0x02} ‖ RND.A[0:6], zero-filled, AES-ECB(SCBK)
//   - client (PD) cryptogram     ENC(S-ENC, RND.A ‖ RND.B)
//   - server (CP) cryptogram     ENC(S-ENC, RND.B ‖ RND.A)   — operands reversed, deliberately
//   - initial R-MAC              ENC(S-MAC2, ENC(S-MAC1, server cryptogram))
//   - payload CBC IV             bitwise complement of the opposite direction's chaining MAC
//   - MAC                        CBC-MAC, S-MAC1 for all blocks but the last, S-MAC2 for the last
//   - MAC placement              4 bytes after the payload, before the CRC, computed over
//                                everything preceding it
//   - CRC byte order             little-endian on the wire (bwrite_u16_le)
//
// That check found ONE divergence, now fixed: payload padding. See padPayload — libosdp appends
// the 0x80 marker UNCONDITIONALLY before encrypting, while the MAC path appends it only when the
// input is not block-aligned. This file had used the MAC rule for both, so a payload whose length
// was an exact multiple of 16 went out unpadded. Both halves of this package agreed with each
// other, so every round-trip test passed, and a real reader would have rejected the frame.
//
// WHAT IS STILL UNVERIFIED: interoperability with actual hardware. Reading the reference
// implementation removes the guesswork, not the firmware quirks, and no test in this package can
// prove interop — cp.go and pd.go share these primitives, so they pass whenever the two halves
// agree, including if both are wrong the same way. Build-order step 5 (a reader on the bench) is
// what closes that, and TestKnownAnswerVectors' pinned values remain self-generated until someone
// replaces them with an OSDP Bench capture.
//
// The failure mode remains loud, not silent: wrong constants mean the peer rejects our cryptogram
// and the session never establishes, which — because everything here is fail-closed — takes the
// reader out of service rather than quietly downgrading it. This cannot produce a door that opens
// insecurely; it produces a door that does not open at all.
// ──────────────────────────────────────────────────────────────────────────────────────────────

// SCBK is a 16-byte AES-128 Secure Channel Base Key.
type SCBK [16]byte

// SCBKDefault is the well-known install-mode base key, bytes 0x30–0x3F.
//
// A reader still using this is NOT secure — the key is in every vendor's documentation. The
// hardware plan §3.1 caps such a reader at `interior` doors until a KEYSET replaces it, and PDCAP
// capability 9 lets a CP detect the condition without attempting a handshake at all.
var SCBKDefault = SCBK{
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
	0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F,
}

// IsDefault reports whether k is the well-known install-mode key.
func (k SCBK) IsDefault() bool {
	// Constant time: not because an attacker can time this, but because copying the habit costs
	// nothing and the one place it is skipped is the one that gets reused for a real comparison.
	return subtle.ConstantTimeCompare(k[:], SCBKDefault[:]) == 1
}

var (
	// ErrSecureChannelRefused means the PD would not complete the handshake. A door requiring
	// Secure Channel MUST go out of service on this — never fall back to cleartext.
	ErrSecureChannelRefused = errors.New("osdp: PD refused the Secure Channel handshake")
	// ErrSecureChannelBroken means an established session failed to verify. Same posture.
	ErrSecureChannelBroken = errors.New("osdp: Secure Channel integrity failure")
	// ErrNotEstablished means a secure operation was attempted before the handshake completed.
	ErrNotEstablished = errors.New("osdp: Secure Channel not established")
	// ErrCryptogram means the peer's cryptogram did not match — it does not hold the base key.
	ErrCryptogram = errors.New("osdp: cryptogram mismatch, peer does not hold the base key")
)

// scState is where a session sits in the handshake.
type scState int

const (
	scIdle       scState = iota // nothing attempted
	scChallenged                // CHLNG sent, awaiting CCRYPT
	scCrypted                   // SCRYPT sent, awaiting RMAC_I
	scActive                    // established; every packet is MAC'd
	scFailed                    // refused or broken — fail closed, do not retry blind
)

// secureChannel holds one session's keys and rolling MAC state. One per PD, on each side.
type secureChannel struct {
	scbk SCBK
	// usingDefault records that this session was built on SCBK-D. It is carried through to the
	// application because a session can be perfectly valid cryptographically and still be worthless
	// as a trust signal.
	usingDefault bool

	sEnc, sMac1, sMac2 [16]byte
	rndA, rndB         [8]byte
	cUID               [8]byte
	serverCryptogram   [16]byte

	// cMac and rMac chain: each direction's MAC seeds the other's next IV, so a replayed or
	// reordered packet fails even though its own MAC is internally consistent.
	cMac, rMac [16]byte

	state scState
}

func newSecureChannel(key SCBK) *secureChannel {
	return &secureChannel{scbk: key, usingDefault: key.IsDefault(), state: scIdle}
}

func (s *secureChannel) established() bool { return s != nil && s.state == scActive }
func (s *secureChannel) failed() bool      { return s != nil && s.state == scFailed }

// fail puts the session into the terminal failed state. Everything that detects an integrity
// problem routes through here, so there is exactly one place that decides a session is dead.
func (s *secureChannel) fail() { s.state = scFailed }

// ecb encrypts exactly one 16-byte block with key. AES-ECB on a single block is the primitive the
// OSDP key schedule is built from; it is never used to encrypt message data.
func ecb(key [16]byte, block [16]byte) [16]byte {
	c, err := aes.NewCipher(key[:])
	if err != nil {
		panic("osdp: aes.NewCipher on a 16-byte key: " + err.Error()) // impossible
	}
	var out [16]byte
	c.Encrypt(out[:], block[:])
	return out
}

// deriveSessionKeys produces the three session keys from the base key and the CP's random.
//
// Each key is AES-ECB(SCBK, marker) where marker is a 16-byte block: 0x01, a per-key selector, then
// the FIRST SIX bytes of RND.A, zero-padded. Six is what the spec uses, not eight — the remaining
// two bytes of RND.A never enter the derivation.
func deriveSessionKeys(scbk SCBK, rndA [8]byte) (sEnc, sMac1, sMac2 [16]byte) {
	build := func(selector byte) [16]byte {
		var b [16]byte
		b[0], b[1] = 0x01, selector
		copy(b[2:8], rndA[:6])
		return ecb(scbk, b)
	}
	return build(0x82), build(0x01), build(0x02)
}

// clientCryptogram is what the PD must produce to prove it holds the base key: ENC(S-ENC, A‖B).
func clientCryptogram(sEnc [16]byte, rndA, rndB [8]byte) [16]byte {
	var b [16]byte
	copy(b[0:8], rndA[:])
	copy(b[8:16], rndB[:])
	return ecb(sEnc, b)
}

// serverCryptogram is what the CP returns to prove the same, with the operands REVERSED: B‖A.
// The asymmetry is the point — identical operand order would let either side replay the other's
// cryptogram back at it and authenticate without the key.
func serverCryptogram(sEnc [16]byte, rndA, rndB [8]byte) [16]byte {
	var b [16]byte
	copy(b[0:8], rndB[:])
	copy(b[8:16], rndA[:])
	return ecb(sEnc, b)
}

// initialRMAC seeds the reply MAC chain from the server cryptogram, through both MAC keys.
func initialRMAC(sMac1, sMac2, srvCrypt [16]byte) [16]byte {
	return ecb(sMac2, ecb(sMac1, srvCrypt))
}

// DiversifySCBK derives a per-reader base key from a site master key and the reader's identity, so
// that recovering one reader's key does not unlock the whole site.
//
// The input block is the PD's vendor code, model, version and serial, with the first six bytes
// repeated to fill 16. Only meaningful once PDID has been read — which is why identification always
// precedes the handshake in the CP state machine.
func DiversifySCBK(master SCBK, info PDInfo) SCBK {
	var b [16]byte
	copy(b[0:3], info.VendorCode[:])
	b[3] = info.Model
	b[4] = info.Version
	b[5] = byte(info.Serial)
	b[6] = byte(info.Serial >> 8)
	b[7] = byte(info.Serial >> 16)
	b[8] = byte(info.Serial >> 24)
	// Bytes 9..15 repeat from the start rather than being zeroed: a run of zeros would make the
	// derived keys of two readers differing only in a late serial byte structurally similar.
	for i := 9; i < 16; i++ {
		b[i] = b[i-9]
	}
	return SCBK(ecb(master, b))
}

// eomMarker is OSDP's end-of-message byte, the 0x80 that terminates a padded block.
const eomMarker = 0x80

// padMAC pads a MAC input to a block boundary with ISO/IEC 9797-1 method 2 — but ONLY when the
// input is not already block-aligned. A block-aligned MAC input is MAC'd as-is.
//
// This differs from padPayload below, and the asymmetry is libosdp's, not ours: osdp_compute_mac
// appends 0x80 only when `rem != 0`, while osdp_encrypt_data appends it unconditionally. Using one
// rule for both is the natural mistake and it silently corrupts exactly one case.
func padMAC(data []byte) []byte {
	if len(data)%16 == 0 {
		return append([]byte(nil), data...)
	}
	out := make([]byte, len(data)+16-len(data)%16)
	copy(out, data)
	out[len(data)] = eomMarker
	return out
}

// padPayload pads plaintext for encryption. The 0x80 marker is ALWAYS appended, even when the
// payload is already a multiple of 16, and the result is then zero-filled to the next boundary.
//
// Verified against libosdp's osdp_encrypt_data, which does `data[length] = OSDP_SC_EOM_MARKER`
// before computing `AES_PAD_LEN(length + 1)` — unconditionally. A 16-byte payload therefore
// encrypts to 32 bytes, not 16.
//
// This was WRONG here until it was checked against the source: the encrypt path reused the
// conditional MAC padding, so a payload whose length happened to be a multiple of 16 was left
// unpadded. Both ends of this package agreed with each other, so every round-trip test passed —
// and a real reader would have rejected the frame. It is the single concrete reason the "our tests
// only prove we agree with ourselves" warning was worth acting on rather than merely writing down.
func padPayload(data []byte) []byte {
	out := make([]byte, (len(data)+16)&^15)
	copy(out, data)
	out[len(data)] = eomMarker
	return out
}

// unpadPayload strips that padding: trailing zeros, then the mandatory marker.
//
// The marker is REQUIRED. Its absence means the plaintext is not what the peer sent — a wrong key,
// a desynchronised MAC chain, or tampering — so this reports an error rather than guessing, and the
// caller fails the session closed.
func unpadPayload(data []byte) ([]byte, error) {
	i := len(data) - 1
	for i >= 0 && data[i] == 0x00 {
		i--
	}
	if i < 0 || data[i] != eomMarker {
		return nil, fmt.Errorf("%w: decrypted payload has no end-of-message marker", ErrSecureChannelBroken)
	}
	return data[:i], nil
}

// computeMAC produces the rolling packet MAC over data.
//
// It is a CBC-MAC with a twist: every block but the last uses S-MAC1, the final block uses S-MAC2,
// and the IV is the OTHER direction's last MAC. That cross-direction chaining is what makes a
// replayed packet fail — its MAC was correct when first sent, but the chain has moved on.
func computeMAC(sMac1, sMac2, iv [16]byte, data []byte) [16]byte {
	buf := padMAC(data)
	if len(buf) == 0 {
		buf = make([]byte, 16)
	}

	cur := iv
	if len(buf) > 16 {
		c, _ := aes.NewCipher(sMac1[:])
		enc := cipher.NewCBCEncrypter(c, cur[:])
		head := make([]byte, len(buf)-16)
		enc.CryptBlocks(head, buf[:len(buf)-16])
		copy(cur[:], head[len(head)-16:])
	}

	c, _ := aes.NewCipher(sMac2[:])
	enc := cipher.NewCBCEncrypter(c, cur[:])
	var out [16]byte
	enc.CryptBlocks(out[:], buf[len(buf)-16:])
	return out
}

// cryptIV returns the payload-encryption IV: the bitwise complement of the chaining MAC. Inverting
// it keeps the encryption IV distinct from the MAC value that is transmitted in the clear.
func cryptIV(mac [16]byte) [16]byte {
	var iv [16]byte
	for i := range mac {
		iv[i] = ^mac[i]
	}
	return iv
}

func encryptPayload(sEnc, mac [16]byte, plain []byte) []byte {
	if len(plain) == 0 {
		return nil
	}
	buf := padPayload(plain)
	iv := cryptIV(mac)
	c, _ := aes.NewCipher(sEnc[:])
	cipher.NewCBCEncrypter(c, iv[:]).CryptBlocks(buf, buf)
	return buf
}

func decryptPayload(sEnc, mac [16]byte, ct []byte) ([]byte, error) {
	if len(ct) == 0 {
		return nil, nil
	}
	if len(ct)%16 != 0 {
		return nil, fmt.Errorf("%w: ciphertext is %d bytes, not a multiple of 16", ErrSecureChannelBroken, len(ct))
	}
	buf := append([]byte(nil), ct...)
	iv := cryptIV(mac)
	c, _ := aes.NewCipher(sEnc[:])
	cipher.NewCBCDecrypter(c, iv[:]).CryptBlocks(buf, buf)
	return unpadPayload(buf)
}

// --- handshake, CP side ---------------------------------------------------------------------

// challenge starts a handshake and returns the CHLNG frame to send.
func (s *secureChannel) challenge(addr uint8) (*Frame, error) {
	if _, err := rand.Read(s.rndA[:]); err != nil {
		// Failing closed on a broken RNG is not paranoia: a predictable RND.A makes the whole
		// session derivable by anyone listening to the bus.
		s.fail()
		return nil, fmt.Errorf("osdp: reading randomness for the challenge: %w", err)
	}
	s.state = scChallenged
	return &Frame{
		Address: addr,
		SCB:     SCB(SCS11, s.keyType()),
		Code:    byte(CmdChlng),
		Data:    append([]byte(nil), s.rndA[:]...),
	}, nil
}

// keyType is the SCB's third byte: which base key this session is built on.
func (s *secureChannel) keyType() byte {
	if s.usingDefault {
		return 0x01 // SCBK-D, install mode
	}
	return 0x00 // site SCBK
}

// acceptCCRYPT verifies the PD's cryptogram and returns the SCRYPT frame to send.
func (s *secureChannel) acceptCCRYPT(addr uint8, data []byte) (*Frame, error) {
	if s.state != scChallenged {
		s.fail()
		return nil, fmt.Errorf("%w: CCRYPT arrived in state %d", ErrSecureChannelBroken, s.state)
	}
	if len(data) < 32 {
		s.fail()
		return nil, fmt.Errorf("%w: CCRYPT payload is %d bytes, want 32", ErrSecureChannelBroken, len(data))
	}
	copy(s.cUID[:], data[0:8])
	copy(s.rndB[:], data[8:16])

	s.sEnc, s.sMac1, s.sMac2 = deriveSessionKeys(s.scbk, s.rndA)

	want := clientCryptogram(s.sEnc, s.rndA, s.rndB)
	// Constant-time: a byte-at-a-time comparison here leaks how much of a forged cryptogram was
	// correct, which is enough to reconstruct it one byte at a time.
	if subtle.ConstantTimeCompare(want[:], data[16:32]) != 1 {
		s.fail()
		return nil, ErrCryptogram
	}

	s.serverCryptogram = serverCryptogram(s.sEnc, s.rndA, s.rndB)
	s.state = scCrypted
	return &Frame{
		Address: addr,
		SCB:     SCB(SCS13, s.keyType()),
		Code:    byte(CmdSCrypt),
		Data:    append([]byte(nil), s.serverCryptogram[:]...),
	}, nil
}

// acceptRMACI completes the handshake.
func (s *secureChannel) acceptRMACI(data []byte) error {
	if s.state != scCrypted {
		s.fail()
		return fmt.Errorf("%w: RMAC_I arrived in state %d", ErrSecureChannelBroken, s.state)
	}
	if len(data) < 16 {
		s.fail()
		return fmt.Errorf("%w: RMAC_I payload is %d bytes, want 16", ErrSecureChannelBroken, len(data))
	}
	want := initialRMAC(s.sMac1, s.sMac2, s.serverCryptogram)
	if subtle.ConstantTimeCompare(want[:], data[:16]) != 1 {
		s.fail()
		return fmt.Errorf("%w: initial R-MAC mismatch", ErrSecureChannelBroken)
	}
	copy(s.rMac[:], data[:16])
	s.state = scActive
	return nil
}

// --- handshake, PD side ---------------------------------------------------------------------

// answerCHLNG produces the CCRYPT reply.
func (s *secureChannel) answerCHLNG(addr uint8, cUID [8]byte, data []byte) (*Frame, error) {
	if len(data) < 8 {
		s.fail()
		return nil, fmt.Errorf("%w: CHLNG payload is %d bytes, want 8", ErrSecureChannelBroken, len(data))
	}
	copy(s.rndA[:], data[:8])
	if _, err := rand.Read(s.rndB[:]); err != nil {
		s.fail()
		return nil, fmt.Errorf("osdp: reading randomness for the challenge response: %w", err)
	}
	s.cUID = cUID
	s.sEnc, s.sMac1, s.sMac2 = deriveSessionKeys(s.scbk, s.rndA)

	crypt := clientCryptogram(s.sEnc, s.rndA, s.rndB)
	out := make([]byte, 0, 32)
	out = append(out, s.cUID[:]...)
	out = append(out, s.rndB[:]...)
	out = append(out, crypt[:]...)

	s.state = scChallenged
	return &Frame{Address: addr, Reply: true, SCB: SCB(SCS12, 0x00), Code: byte(RplCCrypt), Data: out}, nil
}

// answerSCRYPT verifies the CP's cryptogram and produces RMAC_I.
func (s *secureChannel) answerSCRYPT(addr uint8, data []byte) (*Frame, error) {
	if s.state != scChallenged {
		s.fail()
		return nil, fmt.Errorf("%w: SCRYPT arrived in state %d", ErrSecureChannelBroken, s.state)
	}
	if len(data) < 16 {
		s.fail()
		return nil, fmt.Errorf("%w: SCRYPT payload is %d bytes, want 16", ErrSecureChannelBroken, len(data))
	}
	want := serverCryptogram(s.sEnc, s.rndA, s.rndB)
	if subtle.ConstantTimeCompare(want[:], data[:16]) != 1 {
		s.fail()
		return nil, ErrCryptogram
	}
	s.serverCryptogram = want
	rmac := initialRMAC(s.sMac1, s.sMac2, want)
	copy(s.rMac[:], rmac[:])
	s.state = scActive
	return &Frame{Address: addr, Reply: true, SCB: SCB(SCS14, 0x00), Code: byte(RplRMacI),
		Data: append([]byte(nil), rmac[:]...)}, nil
}

// --- per-packet sealing ------------------------------------------------------------------------

// macLen is how many bytes of the 16-byte MAC ride on the wire.
const macLen = 4

// seal turns a plaintext frame into its secure on-the-wire form: an SCB, an encrypted payload, and
// a truncated MAC, with the CRC recomputed over the result.
//
// isCmd selects direction, which decides both which SCS type is used and which chaining MAC seeds
// the IV. Getting that backwards produces a session that works in one direction only.
func (s *secureChannel) seal(f *Frame, isCmd bool) ([]byte, error) {
	if !s.established() {
		return nil, ErrNotEstablished
	}

	chain := s.rMac
	if !isCmd {
		chain = s.cMac
	}

	// SCS_15/16 carry no data and are MAC'd only; SCS_17/18 carry an encrypted payload. POLL and
	// ACK — the overwhelming majority of bus traffic — are the no-data case, which is why both
	// pairs are needed and neither could be dropped from P1.
	var scs byte
	var payload []byte
	if len(f.Data) == 0 {
		scs = SCS15
		if !isCmd {
			scs = SCS16
		}
	} else {
		scs = SCS17
		if !isCmd {
			scs = SCS18
		}
		payload = encryptPayload(s.sEnc, chain, f.Data)
	}

	sealed := &Frame{
		Address:  f.Address,
		Reply:    f.Reply,
		Sequence: f.Sequence,
		SCB:      SCB(scs),
		Code:     f.Code,
		// Reserve the MAC bytes now so LEN is correct before the MAC is computed over it.
		Data: append(payload, make([]byte, macLen)...),
	}
	raw, err := sealed.Marshal()
	if err != nil {
		return nil, err
	}

	// The MAC covers everything transmitted before it — header, SCB, code and ciphertext — but not
	// itself and not the CRC.
	body := raw[:len(raw)-2-macLen]
	mac := computeMAC(s.sMac1, s.sMac2, chain, body)
	copy(raw[len(raw)-2-macLen:len(raw)-2], mac[:macLen])

	if isCmd {
		s.cMac = mac
	} else {
		s.rMac = mac
	}

	// The CRC was computed over the placeholder MAC, so redo it over the finished packet.
	return AppendCRC(raw[:len(raw)-2]), nil
}

// unseal verifies and decrypts a received secure frame in place, returning the plaintext frame.
//
// Every failure path here calls fail(): an integrity error on an established session is not
// something to retry through. It means either a fault or an attacker on the pair, and the correct
// response to both is to tear the session down and take the reader out of service.
func (s *secureChannel) unseal(raw []byte, f *Frame, isCmd bool) (*Frame, error) {
	if !s.established() {
		return nil, ErrNotEstablished
	}
	if !f.Secure() {
		// An established session that suddenly receives a cleartext packet is the downgrade attack
		// an RS-485 tap wants. Refuse it: there is no path back to plaintext once secured.
		s.fail()
		return nil, fmt.Errorf("%w: cleartext packet on an established session", ErrSecureChannelBroken)
	}
	if len(f.Data) < macLen {
		s.fail()
		return nil, fmt.Errorf("%w: packet too short to carry a MAC", ErrSecureChannelBroken)
	}

	chain := s.rMac
	if !isCmd {
		chain = s.cMac
	}

	body := raw[:len(raw)-2-macLen]
	got := raw[len(raw)-2-macLen : len(raw)-2]
	want := computeMAC(s.sMac1, s.sMac2, chain, body)
	if subtle.ConstantTimeCompare(want[:macLen], got) != 1 {
		s.fail()
		return nil, fmt.Errorf("%w: packet MAC mismatch", ErrSecureChannelBroken)
	}

	ct := f.Data[:len(f.Data)-macLen]
	plain, err := decryptPayload(s.sEnc, chain, ct)
	if err != nil {
		s.fail()
		return nil, err
	}

	if isCmd {
		s.cMac = want
	} else {
		s.rMac = want
	}

	return &Frame{
		Address:  f.Address,
		Reply:    f.Reply,
		Sequence: f.Sequence,
		SCB:      f.SCB,
		Code:     f.Code,
		Data:     plain,
	}, nil
}

// scsType returns the SCS type of a secure frame, or 0 if it carries no security block.
func scsType(f *Frame) byte {
	if len(f.SCB) < 2 {
		return 0
	}
	return f.SCB[1]
}

// isHandshake reports whether an SCS type belongs to the handshake rather than an active session.
func isHandshake(scs byte) bool { return scs >= SCS11 && scs <= SCS14 }
