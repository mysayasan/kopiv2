package services

import (
	"encoding/base64"

	"github.com/mysayasan/kopiv2/infra/atrest"
)

// This file wraps at-rest encryption for the handful of SECRET control-plane
// settings — the fleet CA private key, the parent leaf private key, and the fleet
// PSK — that would otherwise sit in plaintext in the control-plane database. With a
// cipher configured they are stored AES-256-GCM encrypted (see infra/atrest); reads
// transparently pass through legacy plaintext written before encryption was enabled,
// so turning the feature on needs no migration.
//
// The AEAD blob is binary, but ControlSetting.Value is a TEXT column shared across
// sqlite/postgres/mariadb, so the ciphertext is base64-wrapped for safe storage.

// encodeSecret returns the storage form of a secret value: base64(atrest ciphertext)
// when a cipher is set, otherwise the plaintext unchanged (encryption disabled).
func encodeSecret(cipher *atrest.Cipher, plaintext string) (string, error) {
	if cipher == nil || plaintext == "" {
		return plaintext, nil
	}
	enc, err := cipher.EncryptBytes([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// decodeSecret reverses encodeSecret. A value that is not base64-wrapped atrest
// ciphertext is returned unchanged (legacy plaintext), so a mixed database — some
// rows written before encryption, some after — reads correctly either way.
func decodeSecret(cipher *atrest.Cipher, stored string) string {
	if stored == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || !atrest.IsEncrypted(raw) {
		return stored // legacy plaintext (PEM, raw PSK, …) — never atrest-wrapped
	}
	if cipher == nil {
		// Encrypted at rest but no key available: return as-is. The caller will fail to
		// use it (e.g. the CA won't load and is regenerated) rather than get silent junk.
		return stored
	}
	pt, err := cipher.DecryptBytes(raw)
	if err != nil {
		return stored
	}
	return string(pt)
}
