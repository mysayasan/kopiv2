package atrest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBytesRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	for _, size := range []int{0, 1, 100, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 7} {
		plain := make([]byte, size)
		for i := range plain {
			plain[i] = byte(i * 7)
		}
		enc, err := c.EncryptBytes(plain)
		if err != nil {
			t.Fatalf("size %d encrypt: %v", size, err)
		}
		if size > 0 && !IsEncrypted(enc) {
			t.Fatalf("size %d: ciphertext lacks magic", size)
		}
		dec, err := c.DecryptBytes(enc)
		if err != nil {
			t.Fatalf("size %d decrypt: %v", size, err)
		}
		if !bytes.Equal(dec, plain) {
			t.Fatalf("size %d: round-trip mismatch", size)
		}
	}
}

func TestLegacyPlaintextPassthrough(t *testing.T) {
	c := newTestCipher(t)
	plain := []byte("legacy un-encrypted jpeg bytes")
	out, err := c.DecryptBytes(plain) // no magic -> returned unchanged
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatal("legacy plaintext should pass through unchanged")
	}
}

func TestTamperDetected(t *testing.T) {
	c := newTestCipher(t)
	enc, _ := c.EncryptBytes([]byte("top secret footage index"))
	enc[len(enc)-1] ^= 0xFF // flip a tag byte
	if _, err := c.DecryptBytes(enc); err == nil {
		t.Fatal("tampered ciphertext should fail to decrypt")
	}
}

func TestTruncationDetected(t *testing.T) {
	c := newTestCipher(t)
	enc, _ := c.EncryptBytes(bytes.Repeat([]byte("x"), 3*chunkSize))
	if _, err := c.DecryptBytes(enc[:len(enc)-50]); err == nil {
		t.Fatal("truncated ciphertext should fail to decrypt")
	}
}

func TestWrongKeyFails(t *testing.T) {
	c := newTestCipher(t)
	enc, _ := c.EncryptBytes([]byte("secret"))
	other := make([]byte, KeySize)
	other[0] = 0xAA
	c2, _ := NewCipher(other)
	if _, err := c2.DecryptBytes(enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestFileRoundTripAndInPlace(t *testing.T) {
	c := newTestCipher(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	payload := bytes.Repeat([]byte("video"), 5000)
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.EncryptFileInPlace(src); err != nil {
		t.Fatalf("encrypt in place: %v", err)
	}
	raw, _ := os.ReadFile(src)
	if !IsEncrypted(raw) {
		t.Fatal("file on disk should be encrypted")
	}
	tmp, cleanup, err := c.DecryptToTempFile(src)
	if err != nil {
		t.Fatalf("decrypt to temp: %v", err)
	}
	defer cleanup()
	got, _ := os.ReadFile(tmp)
	if !bytes.Equal(got, payload) {
		t.Fatal("file round-trip mismatch")
	}
}

func TestKeyStoreLoadCreateDestroy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret", "atrest.key")

	ks, err := LoadOrCreate(path)
	if err != nil || !ks.Enabled() {
		t.Fatalf("create: %v enabled=%v", err, ks.Enabled())
	}
	enc, _ := ks.Cipher().EncryptBytes([]byte("data"))

	// Reload uses the same key (can still decrypt).
	ks2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if dec, err := ks2.Cipher().DecryptBytes(enc); err != nil || string(dec) != "data" {
		t.Fatalf("reloaded key should decrypt: %v", err)
	}

	if err := ks2.Destroy(); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key file should be gone, stat err = %v", err)
	}
	// After destroy a fresh LoadOrCreate makes a NEW key that cannot read old ciphertext.
	ks3, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ks3.Cipher().DecryptBytes(enc); err == nil {
		t.Fatal("new key must not decrypt data from the destroyed key")
	}
}
