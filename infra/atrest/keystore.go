package atrest

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// KeyStore owns the master key file and the Cipher built from it. The master key (DEK) may
// be stored plaintext (the "file" protector) or wrapped by a KeyProtector (DPAPI on
// Windows, systemd-creds on Linux, or a passphrase for Docker/portable). Destroying the
// key file (Destroy) crypto-erases every blob/file/stream ever encrypted with it,
// regardless of protector — the wrapped blob is useless once shredded.
type KeyStore struct {
	path      string
	cipher    *Cipher
	protector string
}

// LoadOrCreate loads the plaintext master key from path, or generates one when absent. It
// is the backward-compatible entry point (the "file" protector); use LoadOrCreateWithConfig
// to wrap the key with an OS keystore or passphrase.
func LoadOrCreate(path string) (*KeyStore, error) {
	return LoadOrCreateWithConfig(path, ProtectorConfig{Name: protectorFile})
}

// LoadOrCreateWithRecovery behaves like LoadOrCreateWithConfig, but bootstraps from a
// recovery escrow when no key exists yet. This is the disaster-recovery path: on fresh or
// reimaged hardware where the old (possibly host-bound) key is gone, drop the exported
// recovery file at recoveryPath and provide its passphrase in cfg — on first start the app
// installs it as the key, unwraps it with the passphrase to recover the ORIGINAL master
// key, and migrates to the configured protector, so already-encrypted recordings decrypt.
//
// It only triggers when keyPath is absent and recoveryPath is present; otherwise it is a
// plain LoadOrCreateWithConfig. On success the recovery file is renamed to *.applied so a
// later deletion of the key can't silently re-restore a stale key. Returns whether a
// restore happened so the caller can log it.
func LoadOrCreateWithRecovery(keyPath, recoveryPath string, cfg ProtectorConfig) (ks *KeyStore, restored bool, err error) {
	if recoveryPath == "" || fileExists(keyPath) || !fileExists(recoveryPath) {
		ks, err = LoadOrCreateWithConfig(keyPath, cfg)
		return ks, false, err
	}

	data, rerr := os.ReadFile(recoveryPath)
	if rerr != nil {
		return nil, false, rerr
	}
	// Validate the file is a passphrase recovery escrow before we touch the key path, so a
	// stray/garbage file never clobbers key placement.
	name, _, isRaw, derr := decodeKeyFile(data)
	if derr != nil {
		return nil, false, fmt.Errorf("atrest: recovery file %s: %w", recoveryPath, derr)
	}
	if isRaw || name != protectorPassphrase {
		return nil, false, fmt.Errorf("atrest: recovery file %s is not a passphrase recovery escrow", recoveryPath)
	}
	if merr := os.MkdirAll(filepath.Dir(keyPath), 0o700); merr != nil {
		return nil, false, merr
	}
	if werr := os.WriteFile(keyPath, data, 0o600); werr != nil {
		return nil, false, werr
	}
	ks, err = LoadOrCreateWithConfig(keyPath, cfg)
	if err != nil {
		// Unwrap failed (missing/wrong passphrase); don't leave an unusable key file that
		// would break the next boot too.
		_ = os.Remove(keyPath)
		return nil, false, fmt.Errorf("atrest: restoring from recovery file %s: %w", recoveryPath, err)
	}
	_ = os.Rename(recoveryPath, recoveryPath+".applied")
	return ks, true, nil
}

// fileExists reports whether path is an existing regular file (not a directory).
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// LoadOrCreateWithConfig loads the master key from path, unwrapping it with whatever
// protector produced the on-disk file, and — if cfg selects a different protector —
// re-wraps the same key with it (a lossless migration, since the DEK is unchanged so all
// existing ciphertext stays readable). When no key file exists yet, a fresh random key is
// generated and wrapped with the configured protector. File 0600, parent dir 0700. A
// present-but-unreadable key file is an error rather than silently regenerated, since
// regenerating would orphan all existing ciphertext.
func LoadOrCreateWithConfig(path string, cfg ProtectorConfig) (*KeyStore, error) {
	desired, err := NewProtector(cfg)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return createKey(path, desired)
	case err != nil:
		return nil, err
	}

	currentName, blob, isRaw, derr := decodeKeyFile(data)
	if derr != nil {
		return nil, fmt.Errorf("atrest: key file %s: %w", path, derr)
	}

	// Build the protector that produced the on-disk file so we can unwrap it.
	source := desired
	if isRaw || currentName == protectorFile {
		source = fileProtector{}
	} else if currentName != desired.Name() {
		src, serr := NewProtector(ProtectorConfig{
			Name:           currentName,
			Passphrase:     cfg.Passphrase,
			PassphraseFile: cfg.PassphraseFile,
			PassphraseEnv:  cfg.PassphraseEnv,
		})
		if serr != nil {
			return nil, fmt.Errorf("atrest: key file %s was wrapped with %q which cannot be built here: %w", path, currentName, serr)
		}
		source = src
	}

	key, uerr := source.Unwrap(blob)
	if uerr != nil {
		return nil, fmt.Errorf("atrest: key file %s: %w", path, uerr)
	}
	c, cerr := NewCipher(key)
	if cerr != nil {
		return nil, cerr
	}

	// Migrate to the configured protector if it differs from what's on disk.
	if source.Name() != desired.Name() {
		if werr := writeKeyFile(path, desired, key); werr != nil {
			return nil, fmt.Errorf("atrest: migrating key file %s from %q to %q: %w", path, source.Name(), desired.Name(), werr)
		}
		log.Printf("encryption-at-rest: migrated key protection %q -> %q (same key, existing data unaffected)", source.Name(), desired.Name())
	}
	return &KeyStore{path: path, cipher: c, protector: desired.Name()}, nil
}

func createKey(path string, p KeyProtector) (*KeyStore, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := writeKeyFile(path, p, key); err != nil {
		return nil, err
	}
	c, err := NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &KeyStore{path: path, cipher: c, protector: p.Name()}, nil
}

// writeKeyFile persists the DEK wrapped by p, atomically (temp + rename), 0600. The "file"
// protector writes the bare KeySize key for backward compatibility; every other protector
// writes the magic-framed wrapped blob.
func writeKeyFile(path string, p KeyProtector, key []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	blob, err := p.Wrap(key)
	if err != nil {
		return err
	}
	var data []byte
	if p.Name() == protectorFile {
		data = blob // bare key, legacy format
	} else {
		data = encodeKeyFile(p.Name(), blob)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Cipher returns the encryptor/decryptor, or nil when the store is nil.
func (k *KeyStore) Cipher() *Cipher {
	if k == nil {
		return nil
	}
	return k.cipher
}

// Enabled reports whether a usable key is present.
func (k *KeyStore) Enabled() bool { return k != nil && k.cipher != nil }

// Fingerprint returns the non-secret identifier of the key this store holds, for
// comparing two instances of an app that must share one key. Empty when no key is
// present. See Cipher.Fingerprint for why this is not the install marker's KeyId.
func (k *KeyStore) Fingerprint() string {
	if k == nil {
		return ""
	}
	return k.cipher.Fingerprint()
}

// KeyPath returns the on-disk key file path.
func (k *KeyStore) KeyPath() string {
	if k == nil {
		return ""
	}
	return k.path
}

// Protector returns the name of the protector that currently wraps the key on disk
// (file/passphrase/dpapi/systemd-creds).
func (k *KeyStore) Protector() string {
	if k == nil {
		return ""
	}
	if k.protector == "" {
		return protectorFile
	}
	return k.protector
}

// ExportEscrow wraps the current master key with the given passphrase (Argon2id) and
// returns a portable recovery blob (the "ATRK"+passphrase framing). Stored offline with
// the passphrase, it can re-establish the key on a new machine even when the primary
// protector is host-bound (DPAPI/systemd-creds), which is otherwise unrecoverable off-box.
// The passphrase is the only thing protecting the key here — it must be strong.
func (k *KeyStore) ExportEscrow(passphrase string) ([]byte, error) {
	if k == nil || k.cipher == nil {
		return nil, errors.New("atrest: no key available to export")
	}
	p, err := newPassphraseProtector(ProtectorConfig{Passphrase: passphrase})
	if err != nil {
		return nil, err
	}
	blob, err := p.Wrap(k.cipher.key)
	if err != nil {
		return nil, err
	}
	return encodeKeyFile(protectorPassphrase, blob), nil
}

// VerifyEscrow checks that data is a passphrase recovery file that unwraps with passphrase,
// and reports whether the recovered key matches the current master key. Operators run it to
// confirm a saved recovery file actually works — and protects the right key — before
// relying on it. It never mutates anything.
func (k *KeyStore) VerifyEscrow(passphrase string, data []byte) (matchesCurrent bool, err error) {
	name, blob, isRaw, derr := decodeKeyFile(data)
	if derr != nil {
		return false, derr
	}
	if isRaw || name != protectorPassphrase {
		return false, errors.New("atrest: not a passphrase recovery file")
	}
	p, err := newPassphraseProtector(ProtectorConfig{Passphrase: passphrase})
	if err != nil {
		return false, err
	}
	key, err := p.Unwrap(blob)
	if err != nil {
		return false, err
	}
	if k == nil || k.cipher == nil {
		return false, nil
	}
	return subtle.ConstantTimeCompare(key, k.cipher.key) == 1, nil
}

// Destroy crypto-erases by securely deleting the key file: it overwrites the (tiny)
// file with random data a few times, syncs, truncates, and removes it. All data
// encrypted with this key is then unrecoverable. A caller may additionally TRIM the
// volume. A missing key file is treated as already-destroyed (nil).
func (k *KeyStore) Destroy() error {
	if k == nil || k.path == "" {
		return nil
	}
	info, err := os.Stat(k.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	size := info.Size()
	if size <= 0 {
		size = KeySize
	}
	if f, ferr := os.OpenFile(k.path, os.O_WRONLY, 0); ferr == nil {
		buf := make([]byte, size)
		for pass := 0; pass < 3; pass++ {
			if _, rerr := rand.Read(buf); rerr != nil {
				break
			}
			if _, serr := f.Seek(0, io.SeekStart); serr != nil {
				break
			}
			if _, werr := f.Write(buf); werr != nil {
				break
			}
			_ = f.Sync()
		}
		_ = f.Truncate(0)
		_ = f.Sync()
		_ = f.Close()
	}
	// Drop the in-memory cipher too, so nothing can keep encrypting with the dead key.
	k.cipher = nil
	// Remove the init marker as well, so a post-wipe boot reads as a clean first run
	// (generate a new key) rather than a false "key missing → recover" gate.
	removeMarker(k.path)
	return os.Remove(k.path)
}
