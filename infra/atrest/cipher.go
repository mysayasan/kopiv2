// Package atrest provides generic authenticated encryption at rest for any data type
// (byte blobs, files, and streams). It exists so data can be crypto-erased: encrypt it
// with a key, and to securely wipe it, destroy the key (see KeyStore.Destroy) — the
// ciphertext is then unrecoverable on any device/OS, instantly, regardless of size.
//
// The on-disk format is a small header followed by a sequence of AES-256-GCM chunks, so
// arbitrarily large files (e.g. video) stream without buffering the whole payload. A
// random per-file ID derives a per-file subkey (HKDF), so the global key is never used
// with a bare counter nonce; each chunk's position and finality are bound into the AEAD
// additional data to defeat truncation, reordering, and extension.
package atrest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize is the master key length (AES-256).
	KeySize = 32

	magic       = "ATR1"
	headerLen   = len(magic) + 1 + fileIDLen // magic + version + fileID
	version     = 1
	fileIDLen   = 16
	chunkSize   = 64 * 1024 // plaintext bytes per chunk
	tagSize     = 16        // GCM tag
	nonceSize   = 12        // GCM nonce
	hkdfInfo    = "kopiv2-atrest-file-v1"
	// fingerprintInfo domain-separates the public key fingerprint from every other
	// subkey derived from the master key, so publishing the fingerprint tells an
	// attacker nothing about any per-file subkey.
	fingerprintInfo = "kopiv2-atrest-fingerprint-v1"
	// secretFingerprintInfo domain-separates FingerprintSecret's output from the master
	// key's fingerprint, so the two can never collide even if the same bytes were ever
	// passed to both.
	secretFingerprintInfo = "kopiv2-secret-fingerprint-v1"
	// fingerprintLen is the derived fingerprint's byte length before hex encoding.
	// 8 bytes is far too short to attack a 256-bit key through and long enough that
	// two independently generated keys will not collide in any real fleet.
	fingerprintLen = 8
)

// Cipher encrypts and decrypts with a master key. Safe for concurrent use.
type Cipher struct {
	key []byte
}

// NewCipher builds a Cipher from a 32-byte master key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("atrest: key must be %d bytes, got %d", KeySize, len(key))
	}
	c := &Cipher{key: make([]byte, KeySize)}
	copy(c.key, key)
	return c, nil
}

// IsEncrypted reports whether b begins with the atrest header magic. Read paths use it
// to transparently pass through legacy plaintext written before encryption was enabled.
func IsEncrypted(b []byte) bool {
	return len(b) >= len(magic) && string(b[:len(magic)]) == magic
}

// HeaderLen is the number of leading bytes IsEncrypted needs.
const HeaderLen = headerLen

// Fingerprint returns a short, non-secret identifier derived FROM THE KEY MATERIAL,
// for answering one question: are two processes holding the same master key?
//
// That question has no other answer in the product, and getting it wrong is silent.
// A second instance that cannot decrypt the first one's sealed columns looks entirely
// healthy — for myseliasan those columns are the fleet CA key and PSK, i.e. the whole
// fleet's trust — until something reads one.
//
// Deliberately NOT the install marker's KeyId (see startup.go): that is a random value
// minted per key FILE location, so an operator who correctly copied atrest.key to a
// second host without its .init marker would see two different ids for one key and
// conclude they had a problem they do not have. This is a function of the key itself,
// so identical keys always agree and different keys never do.
//
// Safe to display and to log: it is an HKDF output under a dedicated info label,
// truncated, and the derivation is one-way.
func (c *Cipher) Fingerprint() string {
	if c == nil || len(c.key) == 0 {
		return ""
	}
	out, err := hkdf.Key(sha256.New, c.key, nil, fingerprintInfo, fingerprintLen)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(out)
}

// FingerprintSecret returns a short, non-secret identifier for an arbitrary secret
// string, so two processes can be compared on whether they hold the SAME one without
// either of them revealing what it is.
//
// It exists for the signing secret in a multi-instance deployment. Every instance must
// hold the same jwt.secret or a token minted by one is rejected by the next, and there is
// no way to tell from inside a single process: when none is configured the host generates
// one and writes it back to that instance's own config file, after which it is
// indistinguishable from a configured one. Comparing fingerprints is the only check
// available, and this keeps that check to material an operator already has (their logs)
// rather than putting it on an API.
//
// Same construction and the same one-way guarantee as Cipher.Fingerprint, under its own
// info label so the two can never collide. Empty in, empty out.
func FingerprintSecret(secret string) string {
	if secret == "" {
		return ""
	}
	out, err := hkdf.Key(sha256.New, []byte(secret), nil, secretFingerprintInfo, fingerprintLen)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(out)
}

// gcmForFile derives the per-file AEAD from the random file ID.
func (c *Cipher) gcmForFile(fileID []byte) (cipher.AEAD, error) {
	sub, err := hkdf.Key(sha256.New, c.key, fileID, hkdfInfo, KeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(sub)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(counter uint64) []byte {
	n := make([]byte, nonceSize)
	binary.BigEndian.PutUint64(n[nonceSize-8:], counter)
	return n
}

func chunkAAD(counter uint64, last bool) []byte {
	aad := make([]byte, 9)
	binary.BigEndian.PutUint64(aad[:8], counter)
	if last {
		aad[8] = 1
	}
	return aad
}

// EncryptStream reads plaintext from src and writes the atrest-encrypted form to dst.
func (c *Cipher) EncryptStream(dst io.Writer, src io.Reader) error {
	fileID := make([]byte, fileIDLen)
	if _, err := rand.Read(fileID); err != nil {
		return err
	}
	gcm, err := c.gcmForFile(fileID)
	if err != nil {
		return err
	}

	header := make([]byte, 0, headerLen)
	header = append(header, magic...)
	header = append(header, version)
	header = append(header, fileID...)
	if _, err := dst.Write(header); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)
	out := make([]byte, 0, chunkSize+tagSize)
	var counter uint64
	for {
		n, rerr := io.ReadFull(src, buf)
		last := rerr != nil // EOF or ErrUnexpectedEOF marks the final chunk
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return rerr
		}
		out = gcm.Seal(out[:0], chunkNonce(counter), buf[:n], chunkAAD(counter, last))
		if _, werr := dst.Write(out); werr != nil {
			return werr
		}
		if last {
			return nil
		}
		counter++
	}
}

// DecryptStream reads atrest ciphertext from src and writes plaintext to dst. It fails
// on a wrong key, tampering, truncation, or a non-atrest input.
func (c *Cipher) DecryptStream(dst io.Writer, src io.Reader) error {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(src, header); err != nil {
		return fmt.Errorf("atrest: read header: %w", err)
	}
	if string(header[:len(magic)]) != magic {
		return errors.New("atrest: not encrypted (bad magic)")
	}
	if header[len(magic)] != version {
		return fmt.Errorf("atrest: unsupported version %d", header[len(magic)])
	}
	fileID := header[len(magic)+1:]
	gcm, err := c.gcmForFile(fileID)
	if err != nil {
		return err
	}

	buf := make([]byte, chunkSize+tagSize)
	out := make([]byte, 0, chunkSize)
	var counter uint64
	for {
		n, rerr := io.ReadFull(src, buf)
		last := rerr != nil
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return rerr
		}
		if n < tagSize {
			// Every chunk is at least a 16-byte GCM tag, so anything shorter means the
			// ciphertext was truncated (or isn't atrest data past the header).
			return errors.New("atrest: truncated ciphertext")
		}
		pt, oerr := gcm.Open(out[:0], chunkNonce(counter), buf[:n], chunkAAD(counter, last))
		if oerr != nil {
			return fmt.Errorf("atrest: decrypt chunk %d: %w", counter, oerr)
		}
		out = pt
		if _, werr := dst.Write(pt); werr != nil {
			return werr
		}
		if last {
			return nil
		}
		counter++
	}
}
