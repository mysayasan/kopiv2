package handoff

import (
	"bytes"
	"errors"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	r, err := NewRecipient()
	if err != nil {
		t.Fatalf("new recipient: %v", err)
	}
	msg := []byte(`{"cameras":[{"host":"10.0.0.7","password":"hunter2"}]}`)
	aad := []byte("node-b")

	sealed, err := Seal(r.PublicKey(), aad, msg)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	// The whole point of the exercise: the credential must not be readable in the blob the
	// control plane carries. Asserting on the ciphertext rather than only on the round trip,
	// because a "sealing" function that returned its input unchanged would pass a round-trip
	// test perfectly.
	if bytes.Contains(sealed, []byte("hunter2")) {
		t.Fatal("the sealed bundle contains the plaintext password")
	}
	if bytes.Contains(sealed, []byte("10.0.0.7")) {
		t.Fatal("the sealed bundle contains the plaintext camera address")
	}

	got, err := r.Open(sealed, aad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("round trip mismatch: got %q want %q", got, msg)
	}
}

// A bundle sealed for node B must not open on node C. This is the replay protection that
// makes it safe for the control plane to carry the envelope at all.
func TestOpenRefusesADifferentRecipient(t *testing.T) {
	b, _ := NewRecipient()
	c, _ := NewRecipient()

	sealed, err := Seal(b.PublicKey(), []byte("node-b"), []byte("secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := c.Open(sealed, []byte("node-b")); !errors.Is(err, ErrNotForYou) {
		t.Fatalf("a different recipient opened the bundle (err=%v)", err)
	}
}

// The associated data is what names the intended recipient. Opening with a different one
// must fail even when the private key is right — otherwise the control plane could stage
// node A's cameras onto any node it liked by relabelling the request.
func TestOpenRefusesMismatchedAAD(t *testing.T) {
	r, _ := NewRecipient()
	sealed, err := Seal(r.PublicKey(), []byte("node-b"), []byte("secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := r.Open(sealed, []byte("node-c")); !errors.Is(err, ErrNotForYou) {
		t.Fatalf("the bundle opened under the wrong associated data (err=%v)", err)
	}
}

func TestOpenRefusesTamperedCiphertext(t *testing.T) {
	r, _ := NewRecipient()
	sealed, err := Seal(r.PublicKey(), []byte("node-b"), []byte("secret payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	for _, at := range []int{0, 1, len(sealed) / 2, len(sealed) - 1} {
		tampered := append([]byte(nil), sealed...)
		tampered[at] ^= 0xff
		if _, err := r.Open(tampered, []byte("node-b")); err == nil {
			t.Fatalf("a bundle flipped at byte %d still opened", at)
		}
	}
}

// A substituted ephemeral public key is the classic attempt: swap in a key the attacker
// controls so the recipient derives a key the attacker knows. The header is authenticated,
// so it must not open.
func TestOpenRefusesSubstitutedEphemeralKey(t *testing.T) {
	r, _ := NewRecipient()
	sealed, err := Seal(r.PublicKey(), nil, []byte("secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	attacker, _ := NewRecipient()
	tampered := append([]byte(nil), sealed...)
	copy(tampered[1:1+pubSize], attacker.PublicKey())
	if _, err := r.Open(tampered, nil); err == nil {
		t.Fatal("a bundle with a substituted ephemeral key opened")
	}
}

func TestOpenRejectsMalformed(t *testing.T) {
	r, _ := NewRecipient()
	cases := map[string][]byte{
		"empty":       {},
		"short":       bytes.Repeat([]byte{1}, 8),
		"bad version": append([]byte{99}, bytes.Repeat([]byte{0}, 1+pubSize+nonceSize+16)...),
		"header only": bytes.Repeat([]byte{formatVersion}, 1+pubSize+nonceSize),
	}
	for name, in := range cases {
		if _, err := r.Open(in, nil); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: expected ErrMalformed, got %v", name, err)
		}
	}
}

func TestSealRejectsBadRecipientKey(t *testing.T) {
	if _, err := Seal(nil, nil, []byte("x")); err == nil {
		t.Fatal("sealing to a nil public key was allowed")
	}
	if _, err := Seal(bytes.Repeat([]byte{0}, 31), nil, []byte("x")); err == nil {
		t.Fatal("sealing to a short public key was allowed")
	}
}

// Two bundles of the same plaintext to the same recipient must differ: a fresh ephemeral
// key and nonce per call. If they matched, the ciphertext itself would leak that the staged
// camera set had not changed between two syncs.
func TestSealIsNotDeterministic(t *testing.T) {
	r, _ := NewRecipient()
	a, _ := Seal(r.PublicKey(), nil, []byte("same"))
	b, _ := Seal(r.PublicKey(), nil, []byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext produced identical bundles")
	}
}
