package services

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

func testCipher(t *testing.T) *atrest.Cipher {
	t.Helper()
	key := make([]byte, atrest.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := atrest.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestEncodeDecodeSecretRoundTrip(t *testing.T) {
	c := testCipher(t)
	plain := "-----BEGIN EC PRIVATE KEY-----\nsecret-material\n-----END EC PRIVATE KEY-----"
	stored, err := encodeSecret(c, plain)
	if err != nil {
		t.Fatalf("encodeSecret: %v", err)
	}
	// Stored form must NOT contain the plaintext and must be base64(atrest ciphertext).
	if strings.Contains(stored, "secret-material") || stored == plain {
		t.Fatal("stored secret still contains plaintext")
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || !atrest.IsEncrypted(raw) {
		t.Fatalf("stored form is not base64 atrest ciphertext (decodeErr=%v)", err)
	}
	if got := decodeSecret(c, stored); got != plain {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plain)
	}
}

func TestDecodeSecretPassesThroughLegacyPlaintext(t *testing.T) {
	c := testCipher(t)
	// Values written before encryption was enabled are plain PEM / raw PSK — never
	// base64 atrest — and must read back unchanged, with or without a cipher.
	for _, v := range []string{
		"-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----",
		"raw-fleet-key-abcdef123456",
		"",
	} {
		if got := decodeSecret(c, v); got != v {
			t.Fatalf("legacy passthrough (cipher) changed %q -> %q", v, got)
		}
		if got := decodeSecret(nil, v); got != v {
			t.Fatalf("legacy passthrough (no cipher) changed %q -> %q", v, got)
		}
	}
}

func TestEncodeSecretNilCipherIsPlaintext(t *testing.T) {
	if got, _ := encodeSecret(nil, "hello"); got != "hello" {
		t.Fatalf("nil cipher should not transform: got %q", got)
	}
}

// TestFleetKeyEncryptedAtRest proves the registry stores the PSK encrypted when a
// cipher is configured, yet FleetKey returns the plaintext.
func TestFleetKeyEncryptedAtRest(t *testing.T) {
	settings := &fakeSettingsRepo{}
	cfg := NodeRegistryConfig{
		MulticastAddr: pairing.DefaultMulticastAddr,
		ParentID:      "parent-1",
		SecretCipher:  testCipher(t),
	}
	reg := newNodeRegistry(&fakeNodesRepo{}, settings, cfg)
	ctx := context.Background()

	gen, err := reg.GenerateFleetKey(ctx)
	if err != nil || gen == "" {
		t.Fatalf("GenerateFleetKey: %v / %q", err, gen)
	}
	// The raw stored setting value must be encrypted (not equal to the returned key).
	var storedRaw string
	for _, row := range settings.rows {
		if row.Key == fleetKeySettingKey {
			storedRaw = row.Value
		}
	}
	if storedRaw == "" || storedRaw == gen {
		t.Fatalf("fleet key not encrypted at rest (storedRaw=%q, key=%q)", storedRaw, gen)
	}
	rawBytes, decErr := base64.StdEncoding.DecodeString(storedRaw)
	if decErr != nil || !atrest.IsEncrypted(rawBytes) {
		t.Fatal("stored fleet key is not base64 atrest ciphertext")
	}
	// FleetKey transparently decrypts it back.
	if got, _ := reg.FleetKey(ctx); got != gen {
		t.Fatalf("FleetKey decrypt mismatch: got %q want %q", got, gen)
	}
}

// TestFleetCAKeyEncryptedAtRest proves the CA PRIVATE key is stored encrypted while
// the CA public certificate stays plaintext.
func TestFleetCAKeyEncryptedAtRest(t *testing.T) {
	settings := &fakeSettingsRepo{}
	cfg := NodeRegistryConfig{ParentID: "parent-1", SecretCipher: testCipher(t)}
	reg := newNodeRegistry(&fakeNodesRepo{}, settings, cfg)
	// ensure() lazily creates + persists the CA on first use.
	if _, err := reg.ca.ensure(context.Background()); err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	rows := map[string]string{}
	for _, r := range settings.rows {
		rows[r.Key] = r.Value
	}
	caKey := rows[caKeyKey]
	if caKey == "" {
		t.Fatal("CA key not persisted")
	}
	raw, err := base64.StdEncoding.DecodeString(caKey)
	if err != nil || !atrest.IsEncrypted(raw) {
		t.Fatal("CA private key is not encrypted at rest")
	}
	// The public cert must remain plaintext PEM (not secret).
	if caCert := rows[caCertKey]; !strings.Contains(caCert, "BEGIN CERTIFICATE") {
		t.Fatalf("CA cert should be plaintext PEM, got %q", caCert)
	}
	// The CA still loads back through the cipher.
	if _, err := reg.ca.readSecret(context.Background(), caKeyKey); err != nil {
		t.Fatalf("readSecret CA key: %v", err)
	}
}

// TestFleetKeyLegacyPlaintextStillReadable proves a PSK stored as plaintext (before
// encryption was enabled) is still returned correctly by a cipher-enabled registry.
func TestFleetKeyLegacyPlaintextStillReadable(t *testing.T) {
	settings := &fakeSettingsRepo{}
	// Seed a legacy plaintext PSK row directly.
	_, _ = settings.Create(context.Background(), "", entities.ControlSetting{Key: fleetKeySettingKey, Value: "legacy-plaintext-psk-123456"})
	cfg := NodeRegistryConfig{ParentID: "parent-1", SecretCipher: testCipher(t)}
	reg := newNodeRegistry(&fakeNodesRepo{}, settings, cfg)

	got, err := reg.FleetKey(context.Background())
	if err != nil || got != "legacy-plaintext-psk-123456" {
		t.Fatalf("legacy plaintext PSK not read back: got %q err %v", got, err)
	}
}
