package atrest

import (
	"bytes"
	"testing"
)

func TestPassphraseRoundTrip(t *testing.T) {
	plain := []byte(`{"secret":"camera-password","n":42}`)
	blob, err := EncryptWithPassphrase("correct horse battery staple", plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Contains(blob, []byte("camera-password")) {
		t.Fatal("ciphertext leaks plaintext")
	}
	got, err := DecryptWithPassphrase("correct horse battery staple", blob)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip mismatch: got %q want %q", got, plain)
	}
}

func TestPassphraseWrongPassphraseFails(t *testing.T) {
	blob, err := EncryptWithPassphrase("right-one", []byte("data"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := DecryptWithPassphrase("wrong-one", blob); err == nil {
		t.Fatal("expected decrypt with wrong passphrase to fail")
	}
}

func TestPassphraseTamperFails(t *testing.T) {
	blob, err := EncryptWithPassphrase("pp", []byte("data-to-protect"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	blob[len(blob)-1] ^= 0xFF // flip a ciphertext bit
	if _, err := DecryptWithPassphrase("pp", blob); err == nil {
		t.Fatal("expected decrypt of tampered blob to fail")
	}
}

func TestPassphraseEmptyRejected(t *testing.T) {
	if _, err := EncryptWithPassphrase("   ", []byte("x")); err == nil {
		t.Fatal("expected empty passphrase to be rejected")
	}
}
