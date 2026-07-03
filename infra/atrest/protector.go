package atrest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/argon2"
)

// KeyProtector wraps (encrypts) and unwraps the 32-byte master key — the "DEK" — with a
// platform- or operator-provided key-encryption key ("KEK"), so the key file on disk is
// ciphertext rather than the bare key. The DEK itself never changes, so switching
// protectors only re-wraps it and leaves all existing at-rest ciphertext readable.
//
// Wrap output is opaque and self-describing per protector: Unwrap must recover the exact
// bytes Wrap was given. Crypto-erase still works by shredding the wrapped blob on disk
// (KeyStore.Destroy) — without it the DEK is unrecoverable regardless of the KEK.
type KeyProtector interface {
	// Name is the stable identifier stored in the key-file header (e.g. "passphrase").
	Name() string
	// Wrap seals the DEK; the returned blob is what gets persisted.
	Wrap(dek []byte) ([]byte, error)
	// Unwrap recovers the DEK from a blob previously produced by Wrap on this machine.
	Unwrap(blob []byte) ([]byte, error)
}

// keyfile framing for wrapped keys. A legacy key file is exactly KeySize raw bytes (the
// "file" protector, written before wrapping existed); anything else starts with keyMagic.
const (
	keyMagic   = "ATRK"
	keyVersion = 1
)

// encodeKeyFile frames a wrapped blob as magic+version+name+blob for on-disk storage.
func encodeKeyFile(name string, blob []byte) []byte {
	out := make([]byte, 0, len(keyMagic)+2+len(name)+len(blob))
	out = append(out, keyMagic...)
	out = append(out, keyVersion, byte(len(name)))
	out = append(out, name...)
	out = append(out, blob...)
	return out
}

// decodeKeyFile splits a key file into its protector name and wrapped blob. It reports
// isRaw=true for a legacy bare-key file (exactly KeySize bytes, "file" protector).
func decodeKeyFile(data []byte) (name string, blob []byte, isRaw bool, err error) {
	if len(data) == KeySize {
		return protectorFile, data, true, nil
	}
	if len(data) < len(keyMagic)+2 || string(data[:len(keyMagic)]) != keyMagic {
		return "", nil, false, fmt.Errorf("atrest: key file is corrupt (%d bytes, not a raw key or wrapped key)", len(data))
	}
	if data[len(keyMagic)] != keyVersion {
		return "", nil, false, fmt.Errorf("atrest: unsupported key-file version %d", data[len(keyMagic)])
	}
	nameLen := int(data[len(keyMagic)+1])
	off := len(keyMagic) + 2
	if off+nameLen > len(data) {
		return "", nil, false, fmt.Errorf("atrest: key file is corrupt (truncated protector name)")
	}
	name = string(data[off : off+nameLen])
	blob = data[off+nameLen:]
	return name, blob, false, nil
}

// Protector names (values of security.keyProtector).
const (
	protectorAuto       = "auto"
	protectorFile       = "file"
	protectorPassphrase = "passphrase"
	protectorDPAPI      = "dpapi"
	protectorSystemd    = "systemd-creds"
)

// ProtectorConfig is the resolved key-protection configuration from security.*.
type ProtectorConfig struct {
	// Name selects the protector: "" or "auto" picks the platform default, otherwise one
	// of file/passphrase/dpapi/systemd-creds.
	Name string
	// Passphrase / PassphraseFile / PassphraseEnv source the KEK for the passphrase
	// protector (Docker/portable). Resolution order: inline, file, named env var, then
	// the ATREST_PASSPHRASE fallback env var. Only used by the passphrase protector.
	Passphrase     string
	PassphraseFile string
	PassphraseEnv  string
}

// NewProtector builds the protector selected by cfg, resolving "auto"/"" to the platform
// default. Requesting a protector that this OS/build cannot provide is an error rather
// than a silent downgrade, so misconfiguration never quietly writes a weaker key file.
func NewProtector(cfg ProtectorConfig) (KeyProtector, error) {
	name := strings.TrimSpace(strings.ToLower(cfg.Name))
	if name == "" || name == protectorAuto {
		name = platformDefaultProtectorName()
	}
	switch name {
	case protectorFile:
		return fileProtector{}, nil
	case protectorPassphrase:
		return newPassphraseProtector(cfg)
	default:
		return newPlatformProtector(name, cfg)
	}
}

// fileProtector stores the DEK verbatim (the legacy, plaintext-key behavior). It exists so
// migration and header handling treat "no wrapping" uniformly; KeyStore special-cases it
// to write a bare KeySize file rather than a framed one.
type fileProtector struct{}

func (fileProtector) Name() string                   { return protectorFile }
func (fileProtector) Wrap(dek []byte) ([]byte, error) { return dek, nil }
func (fileProtector) Unwrap(b []byte) ([]byte, error) { return b, nil }

// passphraseProtector derives a KEK from a passphrase with Argon2id and wraps the DEK with
// AES-256-GCM. Portable across hosts (nothing is machine-bound), which makes it the right
// fit for Docker (KEK from a mounted secret/env) and the recovery escrow for the
// host-bound protectors. Parameters are stored in the blob so they can evolve.
type passphraseProtector struct {
	secret []byte
}

// Argon2id KEK-derivation parameters (stored per-blob so they can change later).
const (
	argonTime    = 3
	argonMemKiB  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonSalt    = 16
	ppBlobVer    = 1
)

func newPassphraseProtector(cfg ProtectorConfig) (KeyProtector, error) {
	secret, err := resolvePassphrase(cfg)
	if err != nil {
		return nil, err
	}
	return &passphraseProtector{secret: secret}, nil
}

func resolvePassphrase(cfg ProtectorConfig) ([]byte, error) {
	if s := strings.TrimSpace(cfg.Passphrase); s != "" {
		return []byte(s), nil
	}
	if p := strings.TrimSpace(cfg.PassphraseFile); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("atrest: reading passphrase file %s: %w", p, err)
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			return []byte(s), nil
		}
	}
	if env := strings.TrimSpace(cfg.PassphraseEnv); env != "" {
		if s := strings.TrimSpace(os.Getenv(env)); s != "" {
			return []byte(s), nil
		}
	}
	if s := strings.TrimSpace(os.Getenv("ATREST_PASSPHRASE")); s != "" {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("atrest: passphrase protector requires a passphrase (security.passphrase / passphraseFile / passphraseEnv or ATREST_PASSPHRASE)")
}

func (passphraseProtector) Name() string { return protectorPassphrase }

func (p *passphraseProtector) Wrap(dek []byte) ([]byte, error) {
	salt := make([]byte, argonSalt)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	kek := argon2.IDKey(p.secret, salt, argonTime, argonMemKiB, argonThreads, KeySize)
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, dek, nil)

	blob := make([]byte, 0, 1+4+4+1+1+argonSalt+len(nonce)+len(ct))
	blob = append(blob, ppBlobVer)
	blob = binary.BigEndian.AppendUint32(blob, argonTime)
	blob = binary.BigEndian.AppendUint32(blob, argonMemKiB)
	blob = append(blob, argonThreads, argonSalt)
	blob = append(blob, salt...)
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	return blob, nil
}

func (p *passphraseProtector) Unwrap(blob []byte) ([]byte, error) {
	if len(blob) < 1+4+4+1+1 || blob[0] != ppBlobVer {
		return nil, fmt.Errorf("atrest: passphrase key blob is corrupt")
	}
	time := binary.BigEndian.Uint32(blob[1:5])
	mem := binary.BigEndian.Uint32(blob[5:9])
	threads := blob[9]
	saltLen := int(blob[10])
	off := 11
	if off+saltLen > len(blob) {
		return nil, fmt.Errorf("atrest: passphrase key blob is corrupt (salt)")
	}
	salt := blob[off : off+saltLen]
	off += saltLen
	kek := argon2.IDKey(p.secret, salt, time, mem, threads, KeySize)
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	if off+gcm.NonceSize() > len(blob) {
		return nil, fmt.Errorf("atrest: passphrase key blob is corrupt (nonce)")
	}
	nonce := blob[off : off+gcm.NonceSize()]
	ct := blob[off+gcm.NonceSize():]
	dek, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("atrest: cannot unwrap key — wrong passphrase or corrupt key file")
	}
	if len(dek) != KeySize {
		return nil, fmt.Errorf("atrest: unwrapped key has wrong size")
	}
	return dek, nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte KEK (shared by the wrapping protectors).
func newGCM(kek []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
