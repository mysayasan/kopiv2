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
