package atrest

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// EncryptBytes returns the atrest-encrypted form of b. Suitable for small blobs
// (snapshots, alert images, config values).
func (c *Cipher) EncryptBytes(b []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := c.EncryptStream(&out, bytes.NewReader(b)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// DecryptBytes is the inverse of EncryptBytes. If b is not atrest-encrypted (no magic)
// it is returned unchanged, so callers transparently handle legacy plaintext.
func (c *Cipher) DecryptBytes(b []byte) ([]byte, error) {
	if !IsEncrypted(b) {
		return b, nil
	}
	var out bytes.Buffer
	if err := c.DecryptStream(&out, bytes.NewReader(b)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// EncryptFile writes the encrypted form of srcPath to dstPath.
func (c *Cipher) EncryptFile(dstPath, srcPath string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := c.EncryptStream(out, in); err != nil {
		out.Close()
		os.Remove(dstPath)
		return err
	}
	return out.Close()
}

// EncryptFileInPlace encrypts path atomically: it writes to a sibling temp file then
// renames over the original, so a crash never leaves a half-written file.
func (c *Cipher) EncryptFileInPlace(path string) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".enc.tmp")
	if err := c.EncryptFile(tmp, path); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DecryptFile writes the decrypted form of srcPath to dstPath. A non-encrypted source
// is copied through unchanged (legacy plaintext).
func (c *Cipher) DecryptFile(dstPath, srcPath string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	r, err := c.MaybeDecryptingReader(in)
	if err != nil {
		out.Close()
		os.Remove(dstPath)
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(dstPath)
		return err
	}
	return out.Close()
}

// DecryptToTempFile decrypts srcPath into a new temp file and returns its path plus a
// cleanup func. Used to hand a plaintext file to an external tool (ffmpeg, exporters)
// that needs a real path. The caller must call cleanup.
func (c *Cipher) DecryptToTempFile(srcPath string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "atrest-*.tmp")
	if err != nil {
		return "", func() {}, err
	}
	tmp.Close()
	if err := c.DecryptFile(tmp.Name(), srcPath); err != nil {
		os.Remove(tmp.Name())
		return "", func() {}, err
	}
	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

// MaybeDecryptingReader returns a reader of the plaintext for src. If src begins with
// the atrest header it decrypts on the fly; otherwise it returns src's bytes unchanged
// (legacy plaintext). Ideal for HTTP serving — no whole-file buffering, no range needed.
func (c *Cipher) MaybeDecryptingReader(src io.Reader) (io.Reader, error) {
	br := bufio.NewReaderSize(src, HeaderLen+64)
	head, _ := br.Peek(HeaderLen)
	if !IsEncrypted(head) {
		return br, nil // legacy plaintext: pass through
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(c.DecryptStream(pw, br))
	}()
	return pr, nil
}
