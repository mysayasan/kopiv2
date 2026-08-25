// Package webpush delivers a Web Push message to a browser's push subscription:
// RFC 8291 payload encryption (aes128gcm) and RFC 8292 VAPID authentication.
//
// Standard library only — crypto/ecdh, crypto/ecdsa, crypto/hkdf, crypto/aes — for the same
// reason infra/handoff is: this is a security protocol, and a dependency that implements one
// is a dependency whose bugs are yours anyway. It is about three hundred lines of well-specified
// arithmetic and it can be tested against its own RFC vectors.
//
// WHAT THIS PACKAGE CANNOT DO, and why the callers care so much about it: a Web Push message
// is delivered by POSTing to an endpoint the BROWSER VENDOR owns — fcm.googleapis.com,
// updates.push.services.mozilla.com, web.push.apple.com. There is no way to push to a phone
// without reaching one of them. On an install with no internet egress — which is how this
// suite's control plane is normally deployed — every send here fails at the TCP connect, and
// no amount of configuration changes that.
//
// So this package is careful to make the difference LEGIBLE rather than returning one
// undifferentiated error: see Result and its Outcome values. A caller can then say "this
// appliance cannot reach the push service" instead of "notification failed", which is the
// difference between an operator fixing their network and an operator filing a bug.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxPayloadBytes is what a subscription is guaranteed to accept. The spec floor is 4096
// octets for the whole encrypted record, and the record costs 86 bytes of header plus 17 of
// AEAD overhead and padding — so the plaintext ceiling is lower than the number everyone
// remembers. A payload over it is refused HERE rather than by the push service, because a 413
// from a vendor arrives without saying which of your notifications was too long.
const maxPayloadBytes = 4096 - 86 - 17

// recordSize is advertised in the header. One record, so it only has to exceed the content.
const recordSize = 4096

// vapidTTL is how long a VAPID token stays valid. The spec caps it at 24h; 12 keeps a
// captured token useful for less time at no cost, since one is minted per send.
const vapidTTL = 12 * time.Hour

// Subscription is what the browser handed the page, unchanged.
type Subscription struct {
	// Endpoint is the vendor URL to POST to. It identifies the device AND the vendor.
	Endpoint string `json:"endpoint"`
	// P256dh is the browser's public key, base64url, uncompressed P-256 (65 bytes).
	P256dh string `json:"p256dh"`
	// Auth is the browser's authentication secret, base64url (16 bytes).
	Auth string `json:"auth"`
}

// Keys is one install's VAPID identity. The SAME key pair must be used for every send to a
// given subscription: a browser binds the subscription to the public key it was created with,
// so regenerating this invalidates every existing subscription at once.
type Keys struct {
	// Public is base64url of the uncompressed P-256 point (65 bytes). It is handed to the
	// browser at subscribe time and is not secret.
	Public string
	// Private is base64url of the 32-byte scalar. It is.
	Private string
}

// Options tune one send.
type Options struct {
	// Subject is the VAPID `sub` claim: a mailto: or https: URL identifying who is sending,
	// so a vendor can contact the operator of a misbehaving sender. Required by the spec;
	// vendors vary in whether they enforce it.
	Subject string
	// TTL is how long the push service should hold the message for a device that is offline.
	TTL time.Duration
	// Urgency is "very-low" | "low" | "normal" | "high". Empty sends none.
	Urgency string
	// Topic collapses messages: a later message with the same topic REPLACES an undelivered
	// earlier one. Empty sends none.
	Topic string
	// Client overrides the HTTP client (tests, and any install that needs a proxy).
	Client *http.Client
}

// Outcome classifies what happened, because the four cases need four different reactions and
// an undifferentiated error gets all of them wrong.
type Outcome string

const (
	// OutcomeDelivered means the push service accepted the message. It does NOT mean the
	// phone showed it — nothing observable from here does.
	OutcomeDelivered Outcome = "delivered"
	// OutcomeUnreachable means the push service could not be contacted at all: DNS, connect,
	// or TLS. On an install with no internet egress this is the ONLY outcome that will ever
	// occur, and reporting it as a delivery failure would send somebody looking for a bug in
	// the notification code.
	OutcomeUnreachable Outcome = "unreachable"
	// OutcomeGone means the subscription is dead (404/410): the browser was uninstalled, the
	// user cleared site data, or the vendor expired it. THE CALLER MUST DELETE IT. A stale
	// subscription that is never removed is a push attempt on every notification, forever,
	// against a phone that no longer exists.
	OutcomeGone Outcome = "gone"
	// OutcomeRejected means the service was reached and refused the message: a bad VAPID
	// token, a payload too large, a rate limit. The message is the vendor's own.
	OutcomeRejected Outcome = "rejected"
)

// Result is one send's outcome, with enough detail to act on.
type Result struct {
	Outcome Outcome
	// Status is the HTTP status when one was received, and 0 when the service was never
	// reached — which is itself the distinction that matters most here.
	Status int
	// Detail is the vendor's own words, or the transport error.
	Detail string
}

// Err reports whether this result should be treated as a failure by a caller that only wants
// a bool. Delivered is the only success.
func (r Result) Err() error {
	if r.Outcome == OutcomeDelivered {
		return nil
	}
	return fmt.Errorf("%s: %s", r.Outcome, r.Detail)
}

// ErrPayloadTooLarge is returned before anything is sent.
var ErrPayloadTooLarge = errors.New("the notification is too large to push")

// GenerateKeys mints a new VAPID key pair.
//
// Called once per install. Everything subscribed under the previous key stops working the
// moment this is called, so the caller stores the result and never regenerates casually.
func GenerateKeys() (Keys, error) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return Keys{}, err
	}
	return Keys{
		Public:  b64.EncodeToString(priv.PublicKey().Bytes()),
		Private: b64.EncodeToString(priv.Bytes()),
	}, nil
}

var b64 = base64.RawURLEncoding

// Send encrypts payload for the subscription and posts it to the push service.
//
// The returned Result is meaningful even when err is nil for a non-delivery: err is reserved
// for a caller that cannot be bothered to look, and Result is for one that can.
func Send(ctx context.Context, keys Keys, sub Subscription, payload []byte, opts Options) (Result, error) {
	if len(payload) > maxPayloadBytes {
		return Result{Outcome: OutcomeRejected, Detail: ErrPayloadTooLarge.Error()}, ErrPayloadTooLarge
	}
	endpoint, err := url.Parse(strings.TrimSpace(sub.Endpoint))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return Result{Outcome: OutcomeRejected, Detail: "the subscription endpoint is not a valid https URL"},
			errors.New("invalid push endpoint")
	}

	body, err := encrypt(sub, payload)
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: err.Error()}, err
	}
	token, err := vapidToken(keys, endpoint, opts.Subject)
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: err.Error()}, err
	}

	ttl := int(opts.TTL.Seconds())
	if ttl <= 0 {
		ttl = int((24 * time.Hour).Seconds())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: err.Error()}, err
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", fmt.Sprintf("%d", ttl))
	req.Header.Set("Authorization", "vapid t="+token+", k="+keys.Public)
	if u := strings.TrimSpace(opts.Urgency); u != "" {
		req.Header.Set("Urgency", u)
	}
	if t := strings.TrimSpace(opts.Topic); t != "" {
		req.Header.Set("Topic", t)
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// THE CASE THIS WHOLE PACKAGE IS SHAPED AROUND. No response at all means the push
		// service was never reached, which on an air-gapped install is not a fault to
		// investigate — it is the network being what it is.
		return Result{Outcome: classifyTransport(err), Detail: transportDetail(err)}, err
	}
	defer resp.Body.Close()
	detail := readDetail(resp)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return Result{Outcome: OutcomeDelivered, Status: resp.StatusCode, Detail: detail}, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return Result{Outcome: OutcomeGone, Status: resp.StatusCode, Detail: detail},
			fmt.Errorf("the subscription is gone (%d)", resp.StatusCode)
	default:
		return Result{Outcome: OutcomeRejected, Status: resp.StatusCode, Detail: detail},
			fmt.Errorf("the push service refused the message (%d)", resp.StatusCode)
	}
}

// classifyTransport separates "could not reach the service" from everything else.
//
// The distinction between the two context errors is the whole of this function, and getting
// it wrong cost this feature its most important answer: it originally treated BOTH as "not
// the network's fault", which made an appliance with no internet route report that the push
// service had REFUSED it — the one verdict that sends an operator hunting a bug in the
// product instead of reading the sentence about air-gapped sites. The live bench caught it,
// because a real request to an address nothing answers on ends exactly this way.
//
//	Canceled          the CALLER gave up — the browser disconnected, the process is shutting
//	                  down. Nothing was learned about the network, so claiming no egress
//	                  would be an invention.
//	DeadlineExceeded  OUR OWN allowance ran out with no response. That is not a fact about
//	                  the caller; it is the thing "unreachable" means. A push service that
//	                  cannot answer inside twenty seconds has not been reached.
func classifyTransport(err error) Outcome {
	if errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return OutcomeRejected
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return OutcomeUnreachable
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return OutcomeUnreachable
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return OutcomeUnreachable
	}
	return OutcomeUnreachable
}

func transportDetail(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "the push service's address could not be resolved (" + dnsErr.Name + ")"
	}
	return err.Error()
}

func readDetail(resp *http.Response) string {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

// encrypt implements RFC 8291 §3.4 over the RFC 8188 aes128gcm content encoding.
//
// Two HKDF stages, and they are not interchangeable. The first mixes the ECDH secret with the
// subscription's AUTH SECRET — which is what stops anyone who merely intercepted the public
// keys from decrypting — and the second derives the record key and nonce from the per-message
// salt. Collapsing them into one would produce something that looks like it works and is not
// what any browser will decrypt.
func encrypt(sub Subscription, plaintext []byte) ([]byte, error) {
	// A fresh sender key per MESSAGE, not per subscription: two messages sharing a key share
	// a content key, after which only the nonce separates them.
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encryptWith(sub, plaintext, salt, asPriv)
}

// encryptWith is encrypt with the two random inputs supplied.
//
// The seam exists so the RFC 8291 §5 test vector can be reproduced BYTE FOR BYTE. A
// round-trip test against a decryptor written in the same sitting proves only that the two
// halves agree with each other, which is exactly what a pair of matching misreadings of the
// spec also does — and no browser would decrypt either.
func encryptWith(sub Subscription, plaintext, salt []byte, asPriv *ecdh.PrivateKey) ([]byte, error) {
	uaPublic, err := b64.DecodeString(strings.TrimRight(sub.P256dh, "="))
	if err != nil || len(uaPublic) != 65 {
		return nil, errors.New("the subscription's public key is not a valid P-256 point")
	}
	authSecret, err := b64.DecodeString(strings.TrimRight(sub.Auth, "="))
	if err != nil || len(authSecret) == 0 {
		return nil, errors.New("the subscription's auth secret is not valid")
	}
	uaKey, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, errors.New("the subscription's public key is not on the curve")
	}

	asPublic := asPriv.PublicKey().Bytes()
	shared, err := asPriv.ECDH(uaKey)
	if err != nil {
		return nil, err
	}

	keyInfo := append([]byte("WebPush: info\x00"), uaPublic...)
	keyInfo = append(keyInfo, asPublic...)
	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}

	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 0x02 is the LAST-RECORD delimiter. 0x01 would say "another record follows", and a
	// browser that believes it will wait for one that never comes.
	content := append(append([]byte{}, plaintext...), 0x02)

	header := make([]byte, 0, 16+4+1+len(asPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)

	return gcm.Seal(header, nonce, content, nil), nil
}

// vapidToken builds the RFC 8292 signed token identifying this sender to the push service.
func vapidToken(keys Keys, endpoint *url.URL, subject string) (string, error) {
	priv, err := ecdsaKey(keys.Private)
	if err != nil {
		return "", err
	}
	sub := strings.TrimSpace(subject)
	if sub == "" {
		// The spec requires a contact. A vendor that enforces it rejects an empty one with a
		// message nobody can act on, so a usable default beats an empty claim.
		sub = "mailto:admin@localhost"
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		// The audience is the ORIGIN of the endpoint, never the full URL: the path identifies
		// the device, and putting it in a signed token hands the vendor a token bound to one
		// subscription that will not verify for another.
		"aud": endpoint.Scheme + "://" + endpoint.Host,
		"exp": time.Now().Add(vapidTTL).Unix(),
		"sub": sub,
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + b64.EncodeToString(claims)

	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return "", err
	}
	// JOSE wants the raw r||s pair, each left-padded to the curve size — NOT the ASN.1
	// encoding ecdsa.SignASN1 produces. A DER signature here is accepted by nothing.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// ecdsaKey turns the stored 32-byte scalar into a signing key.
//
// The public point is taken from crypto/ecdh rather than from elliptic.ScalarBaseMult, which
// is deprecated — the two agree because VAPID's signing key and its ECDH key are the same
// P-256 key.
func ecdsaKey(privateB64 string) (*ecdsa.PrivateKey, error) {
	raw, err := b64.DecodeString(strings.TrimRight(privateB64, "="))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("the VAPID private key is not a valid P-256 scalar")
	}
	ecdhPriv, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return nil, errors.New("the VAPID private key is not a valid P-256 scalar")
	}
	pub := ecdhPriv.PublicKey().Bytes()
	if len(pub) != 65 {
		return nil, errors.New("the VAPID key produced an unexpected public point")
	}
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(pub[1:33]),
			Y:     new(big.Int).SetBytes(pub[33:65]),
		},
		D: new(big.Int).SetBytes(raw),
	}, nil
}

// PublicKeyOf returns the public half of a stored private key, so a caller that persisted only
// the private half can still hand the browser what it needs.
func PublicKeyOf(privateB64 string) (string, error) {
	raw, err := b64.DecodeString(strings.TrimRight(privateB64, "="))
	if err != nil || len(raw) != 32 {
		return "", errors.New("the VAPID private key is not a valid P-256 scalar")
	}
	priv, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(priv.PublicKey().Bytes()), nil
}
