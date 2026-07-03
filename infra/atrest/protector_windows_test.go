//go:build windows

package atrest

import (
	"bytes"
	"testing"
)

// TestDPAPIRoundTrip exercises the real Windows DPAPI machine-scope wrap/unwrap so we know
// the syscall wiring works, not just that it compiles.
func TestDPAPIRoundTrip(t *testing.T) {
	p := dpapiProtector{}
	dek := testDEK()
	blob, err := p.Wrap(dek)
	if err != nil {
		t.Fatalf("DPAPI wrap: %v", err)
	}
	if bytes.Equal(blob, dek) {
		t.Fatal("DPAPI blob equals plaintext DEK")
	}
	got, err := p.Unwrap(blob)
	if err != nil {
		t.Fatalf("DPAPI unwrap: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("DPAPI roundtrip DEK mismatch")
	}
}

// TestDPAPIMigrationThroughKeyStore drives file -> dpapi -> file migration end to end.
func TestDPAPIMigrationThroughKeyStore(t *testing.T) {
	dir := t.TempDir()
	path := dir + "\\atrest.key"
	ks1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := ks1.Cipher().EncryptBytes([]byte("dpapi-footage"))

	ks2, err := LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorDPAPI})
	if err != nil {
		t.Fatalf("migrate to dpapi: %v", err)
	}
	if dec, err := ks2.Cipher().DecryptBytes(blob); err != nil || string(dec) != "dpapi-footage" {
		t.Fatalf("dpapi keystore cannot read pre-migration data: %v", err)
	}
	// Reopen (no migration) must still work with no operator input — the point of DPAPI.
	ks3, err := LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorDPAPI})
	if err != nil {
		t.Fatal(err)
	}
	if dec, err := ks3.Cipher().DecryptBytes(blob); err != nil || string(dec) != "dpapi-footage" {
		t.Fatalf("reopened dpapi keystore cannot read data: %v", err)
	}
}
