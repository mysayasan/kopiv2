package atrest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testDEK() []byte {
	k := make([]byte, KeySize)
	for i := range k {
		k[i] = byte(i * 3)
	}
	return k
}

func TestPassphraseProtectorRoundTrip(t *testing.T) {
	p, err := newPassphraseProtector(ProtectorConfig{Passphrase: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	dek := testDEK()
	blob, err := p.Wrap(dek)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(blob, dek) {
		t.Fatal("wrapped blob equals plaintext DEK — not encrypted")
	}
	got, err := p.Unwrap(blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("unwrapped DEK mismatch")
	}
}

func TestPassphraseWrongSecretFails(t *testing.T) {
	good, _ := newPassphraseProtector(ProtectorConfig{Passphrase: "right"})
	bad, _ := newPassphraseProtector(ProtectorConfig{Passphrase: "wrong"})
	blob, err := good.Wrap(testDEK())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Unwrap(blob); err == nil {
		t.Fatal("expected unwrap with wrong passphrase to fail")
	}
}

func TestPassphraseRequiresSecret(t *testing.T) {
	t.Setenv("ATREST_PASSPHRASE", "")
	if _, err := newPassphraseProtector(ProtectorConfig{}); err == nil {
		t.Fatal("expected error when no passphrase is provided")
	}
}

func TestPassphraseEnvFallback(t *testing.T) {
	t.Setenv("ATREST_PASSPHRASE", "from-env")
	p, err := newPassphraseProtector(ProtectorConfig{})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := p.Wrap(testDEK())
	if _, err := p.Unwrap(blob); err != nil {
		t.Fatal(err)
	}
}

func TestKeyFileFraming(t *testing.T) {
	blob := []byte("wrapped-bytes-here")
	enc := encodeKeyFile(protectorPassphrase, blob)
	name, got, isRaw, err := decodeKeyFile(enc)
	if err != nil {
		t.Fatal(err)
	}
	if isRaw || name != protectorPassphrase || !bytes.Equal(got, blob) {
		t.Fatalf("decode mismatch: name=%q isRaw=%v", name, isRaw)
	}
}

func TestLegacyRawKeyDetected(t *testing.T) {
	name, blob, isRaw, err := decodeKeyFile(testDEK())
	if err != nil {
		t.Fatal(err)
	}
	if !isRaw || name != protectorFile || !bytes.Equal(blob, testDEK()) {
		t.Fatalf("raw key not detected as file protector: name=%q isRaw=%v", name, isRaw)
	}
}

// TestLegacyKeyFileLoads ensures a pre-wrapping bare 32-byte key file still loads.
func TestLegacyKeyFileLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret", "atrest.key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	dek := testDEK()
	if err := os.WriteFile(path, dek, 0o600); err != nil {
		t.Fatal(err)
	}
	ks, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ks.Enabled() {
		t.Fatal("keystore not enabled")
	}
	// The cipher must decrypt data as before — same DEK.
	c := ks.Cipher()
	enc, _ := c.EncryptBytes([]byte("hello"))
	direct, _ := NewCipher(dek)
	dec, err := direct.DecryptBytes(enc)
	if err != nil || string(dec) != "hello" {
		t.Fatalf("legacy key produced a different cipher: %v", err)
	}
}

// TestMigrationPreservesKey is the core safety property: switching protectors re-wraps the
// same DEK, so a cipher built before migration still decrypts data written after it.
func TestMigrationPreservesKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atrest.key")

	// Start plaintext (file protector).
	ks1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := ks1.Cipher().EncryptBytes([]byte("secret-footage"))

	// Migrate file -> passphrase.
	ks2, err := LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorPassphrase, Passphrase: "pw"})
	if err != nil {
		t.Fatalf("migrate to passphrase: %v", err)
	}
	// On-disk file must now be wrapped (not the bare key, not plaintext).
	data, _ := os.ReadFile(path)
	if len(data) == KeySize || string(data[:len(keyMagic)]) != keyMagic {
		t.Fatalf("key file was not wrapped after migration (%d bytes)", len(data))
	}
	// Data written before migration must still decrypt.
	dec, err := ks2.Cipher().DecryptBytes(blob)
	if err != nil || string(dec) != "secret-footage" {
		t.Fatalf("post-migration cipher cannot read pre-migration data: %v", err)
	}

	// Reopen with the passphrase — no migration, same key.
	ks3, err := LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorPassphrase, Passphrase: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	dec2, err := ks3.Cipher().DecryptBytes(blob)
	if err != nil || string(dec2) != "secret-footage" {
		t.Fatalf("reopened passphrase keystore cannot read data: %v", err)
	}

	// Wrong passphrase must refuse to open.
	if _, err := LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorPassphrase, Passphrase: "nope"}); err == nil {
		t.Fatal("expected reopen with wrong passphrase to fail")
	}

	// Migrate back passphrase -> file, then confirm bare key restored and data readable.
	ks4, err := LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorFile, Passphrase: "pw"})
	if err != nil {
		t.Fatalf("migrate back to file: %v", err)
	}
	if data, _ := os.ReadFile(path); len(data) != KeySize {
		t.Fatalf("file protector should write bare key, got %d bytes", len(data))
	}
	if dec, err := ks4.Cipher().DecryptBytes(blob); err != nil || string(dec) != "secret-footage" {
		t.Fatalf("cannot read data after migrating back to file: %v", err)
	}
}

func TestEscrowExportVerify(t *testing.T) {
	dir := t.TempDir()
	ks, err := LoadOrCreate(filepath.Join(dir, "a.key"))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := ks.ExportEscrow("super-secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	// The escrow must be the passphrase framing, not the bare key.
	if len(blob) == KeySize || string(blob[:len(keyMagic)]) != keyMagic {
		t.Fatal("escrow is not a wrapped passphrase blob")
	}
	// Correct passphrase verifies and matches the current key.
	matches, err := ks.VerifyEscrow("super-secret-pass", blob)
	if err != nil || !matches {
		t.Fatalf("verify with right passphrase: matches=%v err=%v", matches, err)
	}
	// Wrong passphrase is rejected.
	if _, err := ks.VerifyEscrow("nope", blob); err == nil {
		t.Fatal("expected verify with wrong passphrase to fail")
	}
	// A different keystore: escrow opens (valid) but does not match its key.
	other, err := LoadOrCreate(filepath.Join(dir, "b.key"))
	if err != nil {
		t.Fatal(err)
	}
	matches, err = other.VerifyEscrow("super-secret-pass", blob)
	if err != nil {
		t.Fatalf("verify against other keystore errored: %v", err)
	}
	if matches {
		t.Fatal("escrow should not match a different key")
	}
	// Short passphrase escrow still round-trips at the module level (length policy is the
	// API layer's job); ensure the escrow of a migrated keystore matches too.
	if _, err := ks.VerifyEscrow("super-secret-pass", []byte("garbage")); err == nil {
		t.Fatal("expected corrupt escrow to fail verify")
	}
}

// TestRecoveryBootstrap is the end-to-end disaster-recovery path: a machine writes data,
// its key is exported as an escrow, then on a fresh key path the recovery file + passphrase
// rebuild the SAME key so the old data decrypts.
func TestRecoveryBootstrap(t *testing.T) {
	oldDir := t.TempDir()
	oldPath := filepath.Join(oldDir, "atrest.key")
	orig, err := LoadOrCreate(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := orig.Cipher().EncryptBytes([]byte("old-recordings"))
	escrow, err := orig.ExportEscrow("recovery-pass-1234")
	if err != nil {
		t.Fatal(err)
	}

	// Fresh machine: no key yet, recovery escrow present, migrate to passphrase protector.
	newDir := t.TempDir()
	newKey := filepath.Join(newDir, "secret", "atrest.key")
	recovery := filepath.Join(newDir, "secret", "recovery.atrestkey")
	if err := os.MkdirAll(filepath.Dir(recovery), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recovery, escrow, 0o600); err != nil {
		t.Fatal(err)
	}

	ks, restored, err := LoadOrCreateWithRecovery(newKey, recovery, ProtectorConfig{Name: protectorFile, Passphrase: "recovery-pass-1234"})
	if err != nil {
		t.Fatalf("recovery bootstrap: %v", err)
	}
	if !restored {
		t.Fatal("expected restored=true")
	}
	// The restored key must decrypt data written on the original machine.
	dec, err := ks.Cipher().DecryptBytes(blob)
	if err != nil || string(dec) != "old-recordings" {
		t.Fatalf("restored key cannot read original data: %v", err)
	}
	// The recovery file must be consumed (renamed) so a later key loss can't silently reuse it.
	if fileExists(recovery) {
		t.Fatal("recovery file should have been renamed to .applied")
	}
	if !fileExists(recovery + ".applied") {
		t.Fatal("expected recovery.applied marker")
	}

	// Second boot: key now exists, recovery ignored, no restore, same data readable.
	ks2, restored2, err := LoadOrCreateWithRecovery(newKey, recovery, ProtectorConfig{Name: protectorFile})
	if err != nil || restored2 {
		t.Fatalf("second boot should not restore: restored=%v err=%v", restored2, err)
	}
	if dec, err := ks2.Cipher().DecryptBytes(blob); err != nil || string(dec) != "old-recordings" {
		t.Fatalf("second boot lost the key: %v", err)
	}
}

func TestRecoveryWrongPassphraseLeavesNoKey(t *testing.T) {
	dir := t.TempDir()
	orig, _ := LoadOrCreate(filepath.Join(t.TempDir(), "k"))
	escrow, _ := orig.ExportEscrow("right-pass-1234")
	key := filepath.Join(dir, "atrest.key")
	recovery := filepath.Join(dir, "recovery.atrestkey")
	if err := os.WriteFile(recovery, escrow, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadOrCreateWithRecovery(key, recovery, ProtectorConfig{Name: protectorFile, Passphrase: "wrong-pass"})
	if err == nil {
		t.Fatal("expected restore with wrong passphrase to fail")
	}
	// Must not leave a half-written, unusable key file behind.
	if fileExists(key) {
		t.Fatal("failed restore should not leave a key file")
	}
}

func TestUnsupportedProtectorErrors(t *testing.T) {
	// A protector name that no platform build provides must error, never downgrade.
	if _, err := NewProtector(ProtectorConfig{Name: "does-not-exist"}); err == nil {
		t.Fatal("expected unknown protector to error")
	}
}
