package atrest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Startup modes reported by OpenForStartup.
const (
	ModeLoaded          = "loaded"           // existing key loaded normally
	ModeRestored        = "restored"         // key rebuilt from a config-configured recovery escrow
	ModeCreated         = "created"          // no key existed anywhere → a fresh one was generated
	ModeRecoveryPending = "recovery-pending" // a key existed here before but is now missing → gate
)

// Outcome reports how the master key was resolved at boot (or that recovery is required).
type Outcome struct {
	Mode     string
	KeyStore *KeyStore // nil when Mode == ModeRecoveryPending
	KeyId    string    // non-secret install identifier from the marker, for display
}

// initMarker is a small non-secret file written beside the key the first time a key is
// established here. Its presence lets a later boot distinguish "a key existed here and is
// now missing" (→ offer recovery) from "brand-new install" (→ generate a key). It never
// holds key material — keyId is a random install identifier, not derived from the key.
type initMarker struct {
	KeyId     string `json:"keyId"`
	CreatedAt int64  `json:"createdAt"`
	Protector string `json:"protector"`
}

func markerPath(keyPath string) string { return keyPath + ".init" }

func newKeyId() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func readMarker(keyPath string) (initMarker, bool) {
	var m initMarker
	data, err := os.ReadFile(markerPath(keyPath))
	if err != nil {
		return m, false
	}
	if json.Unmarshal(data, &m) != nil || m.KeyId == "" {
		return m, false
	}
	return m, true
}

func writeMarker(keyPath string, m initMarker) error {
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := markerPath(keyPath) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, markerPath(keyPath))
}

// ensureMarker returns the existing marker's keyId, or writes a fresh marker (new keyId)
// when none exists yet — so installs predating this feature also become recoverable.
func ensureMarker(keyPath, protector string) string {
	if m, ok := readMarker(keyPath); ok {
		return m.KeyId
	}
	m := initMarker{KeyId: newKeyId(), CreatedAt: time.Now().Unix(), Protector: protector}
	if err := writeMarker(keyPath, m); err != nil {
		log.Printf("atrest: could not write key marker %s: %v", markerPath(keyPath), err)
		return m.KeyId
	}
	return m.KeyId
}

// removeMarker deletes the marker (used by crypto-erase so a wiped host reads as first-run).
func removeMarker(keyPath string) { _ = os.Remove(markerPath(keyPath)) }

func passphraseAvailable(cfg ProtectorConfig) bool {
	_, err := resolvePassphrase(cfg)
	return err == nil
}

// OpenForStartup resolves the master key at boot in strict precedence:
//  1. key file present                                        → ModeLoaded
//  2. key absent + config recovery configured and unwrappable → ModeRestored (no prompt)
//  3. key absent + a key existed here before (marker present)  → ModeRecoveryPending (no key made)
//  4. key absent + genuine first run                          → ModeCreated
//
// It never silently generates a new key when a marker shows one existed, so already-
// encrypted data is not orphaned — recovery is offered instead.
func OpenForStartup(keyPath, recoveryPath string, cfg ProtectorConfig) (Outcome, error) {
	if fileExists(keyPath) {
		ks, err := LoadOrCreateWithConfig(keyPath, cfg)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Mode: ModeLoaded, KeyStore: ks, KeyId: ensureMarker(keyPath, ks.Protector())}, nil
	}

	// Key absent — try the config-driven recovery escrow FIRST (no user prompt).
	if recoveryPath != "" && fileExists(recoveryPath) && passphraseAvailable(cfg) {
		ks, _, err := LoadOrCreateWithRecovery(keyPath, recoveryPath, cfg)
		if err == nil {
			return Outcome{Mode: ModeRestored, KeyStore: ks, KeyId: ensureMarker(keyPath, ks.Protector())}, nil
		}
		// Configured but couldn't restore (wrong passphrase, corrupt file). Don't hard-fail
		// the boot — fall through to the interactive gate (or first-run) below.
		log.Printf("atrest: config-driven recovery from %s failed (%v); falling back to the recovery gate", recoveryPath, err)
	}

	// A key existed here before but is now gone → require recovery rather than minting a new key.
	if m, ok := readMarker(keyPath); ok {
		return Outcome{Mode: ModeRecoveryPending, KeyId: m.KeyId}, nil
	}

	// Genuine first run → generate a fresh key.
	ks, err := LoadOrCreateWithConfig(keyPath, cfg)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Mode: ModeCreated, KeyStore: ks, KeyId: ensureMarker(keyPath, ks.Protector())}, nil
}

// RestoreFromEscrow installs a master key from an exported recovery escrow: it unwraps the
// escrow bytes with passphrase and writes the key at keyPath, wrapped by the protector
// selected in cfg. This is the interactive recovery-gate path. It refuses anything that is
// not a passphrase escrow or that the passphrase cannot open, and writes the key atomically
// so a failed attempt never leaves a broken key behind.
func RestoreFromEscrow(keyPath string, escrow []byte, passphrase string, cfg ProtectorConfig) error {
	name, blob, isRaw, err := decodeKeyFile(escrow)
	if err != nil {
		return err
	}
	if isRaw || name != protectorPassphrase {
		return errors.New("atrest: file is not a passphrase recovery key")
	}
	p, perr := newPassphraseProtector(ProtectorConfig{Passphrase: passphrase})
	if perr != nil {
		return perr
	}
	key, uerr := p.Unwrap(blob)
	if uerr != nil {
		return fmt.Errorf("atrest: wrong passphrase or corrupt recovery key")
	}
	desired, derr := NewProtector(cfg)
	if derr != nil {
		return derr
	}
	return writeKeyFile(keyPath, desired, key)
}
