package atrest

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeyStore owns the master key file and the Cipher built from it. Destroying the key
// (Destroy) crypto-erases every blob/file/stream ever encrypted with it.
type KeyStore struct {
	path   string
	cipher *Cipher
}

// LoadOrCreate loads the 32-byte master key from path, or generates and persists a new
// random one (file 0600, parent dir 0700) when it does not yet exist. A present-but-
// wrong-sized key file is an error rather than silently regenerated, since regenerating
// would orphan all existing ciphertext.
func LoadOrCreate(path string) (*KeyStore, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(data) != KeySize {
			return nil, fmt.Errorf("atrest: key file %s is corrupt (%d bytes, want %d)", path, len(data), KeySize)
		}
		c, cerr := NewCipher(data)
		if cerr != nil {
			return nil, cerr
		}
		return &KeyStore{path: path, cipher: c}, nil
	case os.IsNotExist(err):
		return createKey(path)
	default:
		return nil, err
	}
}

func createKey(path string) (*KeyStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	c, err := NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &KeyStore{path: path, cipher: c}, nil
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

// KeyPath returns the on-disk key file path.
func (k *KeyStore) KeyPath() string {
	if k == nil {
		return ""
	}
	return k.path
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
	return os.Remove(k.path)
}
