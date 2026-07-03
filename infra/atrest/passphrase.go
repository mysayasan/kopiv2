package atrest

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

// EncryptWithPassphrase seals arbitrary plaintext under a user passphrase using
// Argon2id (key derivation) + AES-256-GCM. Unlike the key protectors it is not tied
// to the machine's master key, so the output is portable across hosts — it is the
// primitive behind the passphrase-protected settings backup, letting a backup made
// on one machine be restored on another. The passphrase is the only thing protecting
// the data, so it must be strong. The Argon2id parameters are stored in the blob so
// they can evolve without breaking older files.
func EncryptWithPassphrase(passphrase string, plaintext []byte) ([]byte, error) {
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("atrest: passphrase is required")
	}
	salt := make([]byte, argonSalt)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	kek := argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemKiB, argonThreads, KeySize)
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)

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

// DecryptWithPassphrase reverses EncryptWithPassphrase. A wrong passphrase or any
// tampering fails the AES-GCM authentication and returns an error rather than
// garbage plaintext.
func DecryptWithPassphrase(passphrase string, blob []byte) ([]byte, error) {
	if len(blob) < 1+4+4+1+1 || blob[0] != ppBlobVer {
		return nil, errors.New("atrest: encrypted blob is corrupt")
	}
	time := binary.BigEndian.Uint32(blob[1:5])
	mem := binary.BigEndian.Uint32(blob[5:9])
	threads := blob[9]
	saltLen := int(blob[10])
	off := 11
	if off+saltLen > len(blob) {
		return nil, errors.New("atrest: encrypted blob is corrupt (salt)")
	}
	salt := blob[off : off+saltLen]
	off += saltLen
	kek := argon2.IDKey([]byte(passphrase), salt, time, mem, threads, KeySize)
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	if off+gcm.NonceSize() > len(blob) {
		return nil, errors.New("atrest: encrypted blob is corrupt (nonce)")
	}
	nonce := blob[off : off+gcm.NonceSize()]
	ct := blob[off+gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errors.New("atrest: cannot decrypt — wrong passphrase or corrupt data")
	}
	return pt, nil
}
