// Package pairing implements the LAN discovery + adoption protocol that lets a
// myseliasan control plane find and lock onto a mymatasan node.
//
// Discovery is intentionally locate-only and authenticated: probes and announces
// are HMAC-signed with a shared fleet key, so a node answers only a legitimate
// control plane and a control plane trusts only a real fleet node. A fresh
// nonce+timestamp on every packet blunts replay of captured traffic. The actual
// bind (issuing a pairing token, recording the parent) happens over HTTPS, not
// here — UDP only locates an unpaired node.
package pairing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	// ProbeType / AnnounceType tag the two packet kinds on the wire so a stray
	// datagram on the port is rejected before any crypto work.
	ProbeType    = "mymatasan.pairing.discover"
	AnnounceType = "mymatasan.pairing.announce"

	// DefaultMulticastAddr is the IPv4 multicast group + port the node responder
	// listens on and the control-plane prober targets. 239.x is administratively
	// scoped, so it stays on the local network and does not route off-subnet.
	DefaultMulticastAddr = "239.255.90.21:49531"

	// DefaultReplayWindow bounds how stale a probe/announce timestamp may be before
	// it is rejected. Combined with the per-nonce cache this blunts replay attacks.
	DefaultReplayWindow = 30 * time.Second
)

var (
	errBadHMAC       = errors.New("pairing: hmac mismatch")
	errStale         = errors.New("pairing: stale timestamp")
	errNonceMismatch = errors.New("pairing: announce nonce does not match probe")
)

// Probe is the control-plane → node discovery request. It is HMAC-signed with the
// shared fleet key (so a node only answers a legitimate myseliasan) and carries a
// fresh nonce+timestamp (so captured probes can't be replayed).
type Probe struct {
	Type      string `json:"type"`
	Nonce     string `json:"nonce"`
	Timestamp int64  `json:"ts"`
	HMAC      string `json:"hmac"`
}

// Announce is the node → control-plane reply. It echoes the probe nonce (so the
// prober can tie replies to its own request and reject spoofed/stale announces)
// and is itself HMAC-signed so the prober knows it came from a real fleet node.
type Announce struct {
	Type      string `json:"type"`
	Nonce     string `json:"nonce"` // echoes the probe nonce
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	HTTPSPort int    `json:"httpsPort"`
	// Kind is what the node claims to be ("camera", "iot"). It is an ADVISORY DISPLAY HINT and
	// is deliberately NOT part of the HMAC.
	//
	// Two reasons, and both matter. First, adding a field to the signature would break every
	// mymatasan node already in the field the moment a control plane is upgraded: the node signs
	// without it, the parent verifies with it, and the whole fleet silently stops being
	// discoverable. Second, it does not need to be trusted here — this value only decides an
	// icon in the adoption list. The AUTHORITATIVE kind comes back from the node itself over the
	// adopt call, which is fleet-key-signed and claim-code-gated, and that is the one stored.
	//
	// So: a hostile actor on the LAN can make a fake node look like a sensor hub in a list. They
	// cannot make the control plane adopt it, and they cannot change what an adopted node is.
	Kind      string `json:"kind,omitempty"`
	Timestamp int64  `json:"ts"`
	HMAC      string `json:"hmac"`
}

// AnnounceInfo carries the node-identity fields the responder stamps into each
// announce. Supplied live by the node so a rename or port change is reflected
// without restarting the responder.
type AnnounceInfo struct {
	NodeID    string
	Name      string
	Version   string
	HTTPSPort int
	// Kind is the advisory display hint — see Announce.Kind. Not signed.
	Kind string
}

// NewProbe builds a freshly-signed discovery probe.
func NewProbe(key []byte) (Probe, error) {
	nonce, err := newNonce()
	if err != nil {
		return Probe{}, err
	}
	p := Probe{Type: ProbeType, Nonce: nonce, Timestamp: time.Now().UTC().Unix()}
	p.HMAC = sign(key, p.signingParts()...)
	return p, nil
}

// NewAnnounce builds a signed announce that echoes the given probe nonce.
func NewAnnounce(key []byte, probeNonce string, info AnnounceInfo) (Announce, error) {
	a := Announce{
		Type:      AnnounceType,
		Nonce:     probeNonce,
		NodeID:    info.NodeID,
		Name:      info.Name,
		Version:   info.Version,
		HTTPSPort: info.HTTPSPort,
		Kind:      info.Kind,
		Timestamp: time.Now().UTC().Unix(),
	}
	a.HMAC = sign(key, a.signingParts()...)
	return a, nil
}

// Marshal serialises the packet for the wire.
func (p Probe) Marshal() []byte    { b, _ := json.Marshal(p); return b }
func (a Announce) Marshal() []byte { b, _ := json.Marshal(a); return b }

// ParseProbe decodes and authenticates a probe: right type, valid HMAC for key,
// and a timestamp within window of now. The nonce-replay check is the responder's
// job (it needs shared state); this stays a pure, stateless decode.
func ParseProbe(data, key []byte, window time.Duration) (Probe, error) {
	var p Probe
	if err := json.Unmarshal(data, &p); err != nil {
		return Probe{}, err
	}
	if p.Type != ProbeType {
		return Probe{}, fmt.Errorf("pairing: not a probe (%q)", p.Type)
	}
	if !verify(key, p.HMAC, p.signingParts()...) {
		return Probe{}, errBadHMAC
	}
	if !fresh(p.Timestamp, window) {
		return Probe{}, errStale
	}
	return p, nil
}

// ParseAnnounce decodes and authenticates an announce. When expectNonce is set the
// announce must echo it, which lets the prober reject replies that don't belong to
// its current probe round.
func ParseAnnounce(data, key []byte, window time.Duration, expectNonce string) (Announce, error) {
	var a Announce
	if err := json.Unmarshal(data, &a); err != nil {
		return Announce{}, err
	}
	if a.Type != AnnounceType {
		return Announce{}, fmt.Errorf("pairing: not an announce (%q)", a.Type)
	}
	if expectNonce != "" && a.Nonce != expectNonce {
		return Announce{}, errNonceMismatch
	}
	if !verify(key, a.HMAC, a.signingParts()...) {
		return Announce{}, errBadHMAC
	}
	if !fresh(a.Timestamp, window) {
		return Announce{}, errStale
	}
	return a, nil
}

// SignAssertion builds the HMAC that authenticates the HTTPS adoption call. The
// control plane signs an ordered set of fields (parent id, nonce, timestamp) with
// the fleet key; the node verifies it with VerifyAssertion. This proves the caller
// holds the fleet key without ever putting the key on the wire.
func SignAssertion(key []byte, parts ...string) string { return sign(key, parts...) }

// VerifyAssertion is the node-side counterpart to SignAssertion.
func VerifyAssertion(key []byte, gotHex string, parts ...string) bool {
	return verify(key, gotHex, parts...)
}

// AssertionFresh reports whether an adoption assertion timestamp is within window
// of now, blunting replay of a captured adopt request.
func AssertionFresh(ts int64, window time.Duration) bool { return fresh(ts, window) }

func (p Probe) signingParts() []string {
	return []string{p.Type, p.Nonce, strconv.FormatInt(p.Timestamp, 10)}
}

func (a Announce) signingParts() []string {
	return []string{a.Type, a.Nonce, a.NodeID, a.Name, a.Version, strconv.Itoa(a.HTTPSPort), strconv.FormatInt(a.Timestamp, 10)}
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// sign computes HMAC-SHA256 over the parts, separated by a NUL byte so that the
// concatenation is unambiguous (no field-boundary smuggling).
func sign(key []byte, parts ...string) string {
	mac := hmac.New(sha256.New, key)
	for _, p := range parts {
		mac.Write([]byte(p))
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func verify(key []byte, gotHex string, parts ...string) bool {
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(sign(key, parts...))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

func fresh(ts int64, window time.Duration) bool {
	if window <= 0 {
		window = DefaultReplayWindow
	}
	diff := time.Now().UTC().Unix() - ts
	if diff < 0 {
		diff = -diff
	}
	return diff <= int64(window.Seconds())
}
