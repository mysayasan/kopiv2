package atrest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenForStartupFirstRunThenLoad(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secret", "atrest.key")

	// First run: no key, no marker → created.
	o1, err := OpenForStartup(keyPath, "", ProtectorConfig{Name: protectorFile})
	if err != nil {
		t.Fatal(err)
	}
	if o1.Mode != ModeCreated || o1.KeyStore == nil || o1.KeyId == "" {
		t.Fatalf("first run: mode=%q ks=%v id=%q", o1.Mode, o1.KeyStore != nil, o1.KeyId)
	}
	blob, _ := o1.KeyStore.Cipher().EncryptBytes([]byte("data"))

	// Second boot: key present → loaded, same keyId, same key.
	o2, err := OpenForStartup(keyPath, "", ProtectorConfig{Name: protectorFile})
	if err != nil {
		t.Fatal(err)
	}
	if o2.Mode != ModeLoaded || o2.KeyId != o1.KeyId {
		t.Fatalf("second boot: mode=%q id=%q (want loaded/%s)", o2.Mode, o2.KeyId, o1.KeyId)
	}
	if dec, err := o2.KeyStore.Cipher().DecryptBytes(blob); err != nil || string(dec) != "data" {
		t.Fatalf("loaded key differs: %v", err)
	}
}

func TestOpenForStartupRecoveryPendingWhenKeyLostButMarkerRemains(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secret", "atrest.key")

	o1, err := OpenForStartup(keyPath, "", ProtectorConfig{Name: protectorFile})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the key file going missing (dead host-bound key / lost file) — marker stays.
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	o2, err := OpenForStartup(keyPath, "", ProtectorConfig{Name: protectorFile})
	if err != nil {
		t.Fatal(err)
	}
	if o2.Mode != ModeRecoveryPending {
		t.Fatalf("expected recovery-pending, got %q", o2.Mode)
	}
	if o2.KeyStore != nil {
		t.Fatal("recovery-pending must not carry a keystore (no new key generated)")
	}
	if o2.KeyId != o1.KeyId {
		t.Fatalf("gate keyId %q != original %q", o2.KeyId, o1.KeyId)
	}
	// Critically, no new key file was created.
	if fileExists(keyPath) {
		t.Fatal("recovery-pending must not create a new key file")
	}
}

func TestOpenForStartupConfigRecoveryFirst(t *testing.T) {
	// Build an escrow from an original key.
	src := t.TempDir()
	orig, err := LoadOrCreate(filepath.Join(src, "atrest.key"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := orig.Cipher().EncryptBytes([]byte("original"))
	escrow, err := orig.ExportEscrow("cfg-pass-1234")
	if err != nil {
		t.Fatal(err)
	}

	// Fresh host: no key, but recovery file + passphrase in config → restored, no gate.
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secret", "atrest.key")
	recovery := filepath.Join(dir, "secret", "recovery.atrestkey")
	if err := os.MkdirAll(filepath.Dir(recovery), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recovery, escrow, 0o600); err != nil {
		t.Fatal(err)
	}
	o, err := OpenForStartup(keyPath, recovery, ProtectorConfig{Name: protectorFile, Passphrase: "cfg-pass-1234"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Mode != ModeRestored {
		t.Fatalf("expected restored, got %q", o.Mode)
	}
	if dec, err := o.KeyStore.Cipher().DecryptBytes(data); err != nil || string(dec) != "original" {
		t.Fatalf("config-restored key cannot read original data: %v", err)
	}
}

func TestRestoreFromEscrowInstallsKey(t *testing.T) {
	src := t.TempDir()
	orig, _ := LoadOrCreate(filepath.Join(src, "atrest.key"))
	data, _ := orig.Cipher().EncryptBytes([]byte("payload"))
	escrow, _ := orig.ExportEscrow("gate-pass-1234")

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "atrest.key")
	// Wrong passphrase writes nothing.
	if err := RestoreFromEscrow(keyPath, escrow, "nope", ProtectorConfig{Name: protectorFile}); err == nil {
		t.Fatal("expected wrong passphrase to fail")
	}
	if fileExists(keyPath) {
		t.Fatal("failed restore must not write a key")
	}
	// Correct passphrase installs a working key.
	if err := RestoreFromEscrow(keyPath, escrow, "gate-pass-1234", ProtectorConfig{Name: protectorFile}); err != nil {
		t.Fatal(err)
	}
	ks, err := LoadOrCreate(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if dec, err := ks.Cipher().DecryptBytes(data); err != nil || string(dec) != "payload" {
		t.Fatalf("restored key cannot read original data: %v", err)
	}
}

func TestDestroyRemovesMarker(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "atrest.key")
	o, err := OpenForStartup(keyPath, "", ProtectorConfig{Name: protectorFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := readMarker(keyPath); !ok {
		t.Fatal("marker should exist after key creation")
	}
	if err := o.KeyStore.Destroy(); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMarker(keyPath); ok {
		t.Fatal("Destroy must remove the marker (post-wipe = clean first run)")
	}
	// With the marker gone, the next boot is a first run, not a recovery gate.
	o2, err := OpenForStartup(keyPath, "", ProtectorConfig{Name: protectorFile})
	if err != nil {
		t.Fatal(err)
	}
	if o2.Mode != ModeCreated {
		t.Fatalf("post-destroy boot should be created, got %q", o2.Mode)
	}
}
