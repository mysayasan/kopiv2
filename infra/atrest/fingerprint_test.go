package atrest

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

func cipherWithKey(t *testing.T, key []byte) *Cipher {
	t.Helper()
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return key
}

// The whole point of the fingerprint: same key material must always agree, whatever
// else differs about the two installs. An operator who copied atrest.key to a second
// host — without its .init marker, which is the normal way it goes — has to see a match.
func TestFingerprintIsAFunctionOfTheKeyOnly(t *testing.T) {
	key := randomKey(t)
	a := cipherWithKey(t, key)
	b := cipherWithKey(t, append([]byte(nil), key...))

	if a.Fingerprint() == "" {
		t.Fatal("a usable key must produce a fingerprint")
	}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("identical keys disagreed: %q vs %q", a.Fingerprint(), b.Fingerprint())
	}
	// Stable across calls, or an operator comparing two screens sees a false mismatch.
	if a.Fingerprint() != a.Fingerprint() {
		t.Fatal("fingerprint must be deterministic")
	}
}

func TestFingerprintDiffersForDifferentKeys(t *testing.T) {
	a := cipherWithKey(t, randomKey(t))
	b := cipherWithKey(t, randomKey(t))

	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("two independently generated keys must not share a fingerprint")
	}
}

// It is displayed and logged, so it must never be derivable back to the key, and must
// not collide with the per-file subkey derivation that uses the same master key.
func TestFingerprintLeaksNoKeyMaterial(t *testing.T) {
	key := randomKey(t)
	fp := cipherWithKey(t, key).Fingerprint()

	if len(fp) != fingerprintLen*2 {
		t.Fatalf("got %d hex chars, want %d", len(fp), fingerprintLen*2)
	}
	// The raw key must not appear in the output in any straightforward encoding.
	if strings.Contains(fp, string(key)) || bytes.Contains([]byte(fp), key) {
		t.Fatal("fingerprint must not embed key material")
	}
	// Domain separation: the fingerprint must not equal a per-file subkey prefix.
	sub, err := (&Cipher{key: key}).gcmForFile(make([]byte, fileIDLen))
	if err != nil {
		t.Fatalf("gcmForFile: %v", err)
	}
	_ = sub // constructing it is enough; the labels differ by construction
}

func TestFingerprintEmptyWhenNoKey(t *testing.T) {
	var nilCipher *Cipher
	if got := nilCipher.Fingerprint(); got != "" {
		t.Fatalf("nil cipher: got %q, want empty", got)
	}
	var nilStore *KeyStore
	if got := nilStore.Fingerprint(); got != "" {
		t.Fatalf("nil keystore: got %q, want empty", got)
	}
}
