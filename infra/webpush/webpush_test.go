package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// THE ONE TEST THAT MATTERS: the RFC 8291 §5 worked example, reproduced BYTE FOR BYTE.
//
// A round-trip against a decryptor written in the same sitting proves only that the two
// halves agree with each other — which is exactly what a pair of matching misreadings of the
// spec also does, and no browser would decrypt either. The published vector is the only
// evidence available here that this interoperates with anything.
func TestRFC8291WorkedExample(t *testing.T) {
	const (
		plaintext = "When I grow up, I want to be a watermelon"
		uaPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
		uaAuth    = "BTBZMqHH6r4Tts7J_aSIgg"
		asPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
		saltB64   = "DGv6ra1nlYgDCS1FRnbzlw"
		want      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIg" +
			"Dll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2K" +
			"s3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	)

	rawPriv, err := base64.RawURLEncoding.DecodeString(asPrivate)
	if err != nil {
		t.Fatalf("decode sender key: %v", err)
	}
	asPriv, err := ecdh.P256().NewPrivateKey(rawPriv)
	if err != nil {
		t.Fatalf("sender key: %v", err)
	}
	salt, err := base64.RawURLEncoding.DecodeString(saltB64)
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}

	body, err := encryptWith(
		Subscription{P256dh: uaPublic, Auth: uaAuth}, []byte(plaintext), salt, asPriv)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got := base64.RawURLEncoding.EncodeToString(body)
	if got != want {
		t.Fatalf("the encrypted record does not match RFC 8291 §5.\n got: %s\nwant: %s", got, want)
	}
}

// The sender key and salt must be fresh per MESSAGE. Two messages sharing them share a content
// key and a nonce, which is the one thing AES-GCM must never do.
func TestEachMessageGetsFreshKeyAndSalt(t *testing.T) {
	sub := testSubscription(t)
	a, err := encrypt(sub, []byte("same"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	b, err := encrypt(sub, []byte("same"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(a[:16]) == string(b[:16]) {
		t.Fatal("two messages reused the same salt")
	}
	// Bytes 21..86 are the sender's public key, per the aes128gcm header layout.
	if string(a[21:86]) == string(b[21:86]) {
		t.Fatal("two messages reused the same sender key")
	}
}

// The header is a fixed layout and a browser parses it positionally: salt(16), record size(4),
// key length(1), sender key(65). Getting any offset wrong yields something that decrypts to
// nothing, with no error anyone can read.
func TestRecordHeaderLayout(t *testing.T) {
	sub := testSubscription(t)
	body, err := encrypt(sub, []byte("x"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(body) < 86 {
		t.Fatalf("record is too short to contain a header: %d bytes", len(body))
	}
	if body[20] != 65 {
		t.Fatalf("key length byte is %d, want 65", body[20])
	}
	rs := uint32(body[16])<<24 | uint32(body[17])<<16 | uint32(body[18])<<8 | uint32(body[19])
	if rs != recordSize {
		t.Fatalf("record size is %d, want %d", rs, recordSize)
	}
	if body[21] != 0x04 {
		t.Fatalf("sender key is not an uncompressed point (first byte %#x)", body[21])
	}
}

// The VAPID token is a JOSE object with a raw r||s signature, and its audience is the ORIGIN
// of the endpoint — never the full URL, whose path identifies one device.
func TestVAPIDToken(t *testing.T) {
	keys, err := GenerateKeys()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	endpoint := mustURL(t, "https://fcm.googleapis.com/fcm/send/abc123?x=1")
	tok, err := vapidToken(keys, endpoint, "mailto:ops@example.org")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", tok)
	}
	var header map[string]string
	decodeJSON(t, parts[0], &header)
	if header["alg"] != "ES256" || header["typ"] != "JWT" {
		t.Fatalf("wrong header: %+v", header)
	}
	var claims map[string]any
	decodeJSON(t, parts[1], &claims)
	if claims["aud"] != "https://fcm.googleapis.com" {
		t.Fatalf("audience must be the endpoint's ORIGIN, got %v", claims["aud"])
	}
	if claims["sub"] != "mailto:ops@example.org" {
		t.Fatalf("wrong subject: %v", claims["sub"])
	}
	exp, _ := claims["exp"].(float64)
	if int64(exp) <= time.Now().Unix() {
		t.Fatal("the token is already expired")
	}
	// JOSE wants r||s, 64 bytes. An ASN.1 signature is longer and variable, and is accepted
	// by nothing.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("signature is not base64url: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want a raw 64-byte r||s pair", len(sig))
	}
	// A missing contact must not produce an empty claim the vendor cannot act on.
	tok2, err := vapidToken(keys, endpoint, "   ")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	var claims2 map[string]any
	decodeJSON(t, strings.Split(tok2, ".")[1], &claims2)
	if s, _ := claims2["sub"].(string); s == "" {
		t.Fatal("an empty subject reached the token")
	}
}

// THE DISTINCTION THIS PACKAGE EXISTS TO MAKE. An install with no egress must be told it has
// no egress — not that its notifications failed.
func TestOutcomesAreDistinguished(t *testing.T) {
	keys, _ := GenerateKeys()
	sub := testSubscription(t)

	cases := []struct {
		name   string
		status int
		want   Outcome
	}{
		{"accepted", http.StatusCreated, OutcomeDelivered},
		{"no content", http.StatusOK, OutcomeDelivered},
		{"subscription expired", http.StatusGone, OutcomeGone},
		{"subscription unknown", http.StatusNotFound, OutcomeGone},
		{"bad token", http.StatusUnauthorized, OutcomeRejected},
		{"rate limited", http.StatusTooManyRequests, OutcomeRejected},
	}
	for _, tc := range cases {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Assert the wire format while we are here: these headers are not optional.
			if r.Header.Get("Content-Encoding") != "aes128gcm" {
				t.Errorf("%s: missing aes128gcm content encoding", tc.name)
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") {
				t.Errorf("%s: missing VAPID authorization", tc.name)
			}
			if r.Header.Get("TTL") == "" {
				t.Errorf("%s: missing TTL", tc.name)
			}
			w.WriteHeader(tc.status)
		}))
		sub.Endpoint = srv.URL + "/push/1"
		res, _ := Send(context.Background(), keys, sub, []byte(`{"t":"x"}`),
			Options{Subject: "mailto:a@b.c", Client: srv.Client()})
		if res.Outcome != tc.want {
			t.Fatalf("%s: outcome %q, want %q", tc.name, res.Outcome, tc.want)
		}
		if res.Status != tc.status {
			t.Fatalf("%s: status %d, want %d", tc.name, res.Status, tc.status)
		}
		srv.Close()
	}

	// Nothing listening at all — the air-gapped case.
	sub.Endpoint = "https://127.0.0.1:1/push/1"
	res, _ := Send(context.Background(), keys, sub, []byte("x"), Options{Subject: "mailto:a@b.c"})
	if res.Outcome != OutcomeUnreachable {
		t.Fatalf("an unreachable push service reported %q, want %q", res.Outcome, OutcomeUnreachable)
	}
	if res.Status != 0 {
		t.Fatalf("a service that was never reached reported HTTP %d", res.Status)
	}
}

// A caller cancelling is not the network being unreachable, and reporting it as such would
// have an operator investigating a firewall over a request they aborted.
func TestCancelledSendIsNotReportedAsNoEgress(t *testing.T) {
	keys, _ := GenerateKeys()
	sub := testSubscription(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	sub.Endpoint = srv.URL + "/push/1"

	// An ALREADY-cancelled context, so the transport returns before it touches the network.
	// The first version of this test had the handler block on r.Context().Done() and the
	// deferred srv.Close() wait for that handler — a deadlock in the test, not the code,
	// which is a good reminder that a hanging suite is usually the harness.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _ := Send(ctx, keys, sub, []byte("x"), Options{Subject: "mailto:a@b.c", Client: srv.Client()})
	if res.Outcome == OutcomeUnreachable {
		t.Fatal("a cancelled request was reported as an unreachable push service")
	}
}

// The other half of that distinction, and the half the live bench had to teach this package.
//
// Our own deadline expiring with no response is EXACTLY what "could not be reached" means: it
// is what a request to an address nothing answers on does. Reporting it as a refusal — which
// this package did until a bench sent a real message to a black hole — tells an air-gapped
// operator that a push service rejected them, and sends them looking for a fault in a product
// that is working precisely as it must.
func TestOurOwnTimeoutIsNoEgressNotARefusal(t *testing.T) {
	keys, _ := GenerateKeys()
	sub := testSubscription(t)

	// A socket that ACCEPTS and then says nothing, so the TLS handshake hangs until our own
	// deadline expires. Deliberately not an httptest server with a blocking handler: the
	// handler would still be blocked when the test tore the server down, and Close() waits
	// for handlers — the suite would hang rather than fail, which is a worse outcome than
	// the bug being tested for.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var held []net.Conn
	defer func() {
		ln.Close()
		mu.Lock()
		for _, c := range held {
			c.Close()
		}
		mu.Unlock()
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	sub.Endpoint = "https://" + ln.Addr().String() + "/push/1"

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, _ := Send(ctx, keys, sub, []byte("x"), Options{Subject: "mailto:a@b.c"})
	if res.Outcome != OutcomeUnreachable {
		t.Fatalf("a request that timed out with no response was reported as %q, want %q — an "+
			"install with no route out must never be told the push service refused it",
			res.Outcome, OutcomeUnreachable)
	}
}

// Refused HERE, with the size, rather than by a vendor returning 413 without saying which
// notification was too long.
func TestOversizePayloadIsRefusedBeforeSending(t *testing.T) {
	keys, _ := GenerateKeys()
	sub := testSubscription(t)
	sub.Endpoint = "https://example.invalid/push/1"
	res, err := Send(context.Background(), keys, sub, make([]byte, maxPayloadBytes+1), Options{})
	if err == nil {
		t.Fatal("an oversize payload was accepted")
	}
	if res.Outcome != OutcomeRejected {
		t.Fatalf("outcome %q, want %q", res.Outcome, OutcomeRejected)
	}
}

// A subscription whose key material is junk must fail as a REJECTION with a readable reason,
// never as a panic and never as "unreachable" — nothing was ever reached.
func TestMalformedSubscriptionIsRejected(t *testing.T) {
	keys, _ := GenerateKeys()
	for name, sub := range map[string]Subscription{
		"bad public key": {Endpoint: "https://example.invalid/p", P256dh: "notbase64!!", Auth: "BTBZMqHH6r4Tts7J_aSIgg"},
		"short key":      {Endpoint: "https://example.invalid/p", P256dh: base64.RawURLEncoding.EncodeToString([]byte("short")), Auth: "BTBZMqHH6r4Tts7J_aSIgg"},
		"empty auth":     {Endpoint: "https://example.invalid/p", P256dh: testP256dh(), Auth: ""},
		"bad endpoint":   {Endpoint: "http://insecure/p", P256dh: testP256dh(), Auth: "BTBZMqHH6r4Tts7J_aSIgg"},
	} {
		res, err := Send(context.Background(), keys, sub, []byte("x"), Options{})
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if res.Outcome != OutcomeRejected {
			t.Fatalf("%s: outcome %q, want %q", name, res.Outcome, OutcomeRejected)
		}
		if res.Detail == "" {
			t.Fatalf("%s: refused with no reason", name)
		}
	}
}

func TestPublicKeyOfMatchesGenerated(t *testing.T) {
	keys, err := GenerateKeys()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	pub, err := PublicKeyOf(keys.Private)
	if err != nil {
		t.Fatalf("PublicKeyOf: %v", err)
	}
	if pub != keys.Public {
		t.Fatalf("recovered public key %q, want %q", pub, keys.Public)
	}
	if _, err := PublicKeyOf("nonsense"); err == nil {
		t.Fatal("a junk private key produced a public key")
	}
}

// --- helpers ---------------------------------------------------------------------------

func testP256dh() string {
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
}

func testSubscription(t *testing.T) Subscription {
	t.Helper()
	return Subscription{
		Endpoint: "https://example.invalid/push/1",
		P256dh:   testP256dh(),
		Auth:     "BTBZMqHH6r4Tts7J_aSIgg",
	}
}

func decodeJSON(t *testing.T, part string, into any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("decode %q: %v", part, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}
