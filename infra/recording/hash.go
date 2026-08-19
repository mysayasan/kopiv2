package recording

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// HashPlaintextFile returns the lowercase hex SHA-256 of a file's bytes.
//
// It is called at finalize on the PLAINTEXT mp4, in the window after the duration and
// codec probes and before at-rest encryption. That ordering is the whole point:
//
//   - Hashing the plaintext makes the digest stable across an at-rest key change, a
//     backup and restore, and a move between hosts. A ciphertext hash would change on
//     every one of those and prove nothing about the video.
//   - Hashing at FINALIZE rather than at export is what upgrades an evidence bundle's
//     claim from "this export is internally consistent" to "this footage was not altered
//     between recording and export". A hash computed at export time only proves the file
//     has not changed since the export, which is a much weaker statement and must never
//     be presented as the stronger one — see the manifest's hashOrigin field.
//
// Streamed rather than read whole: segments are tens or hundreds of megabytes and there
// is one of these per camera per segment interval.
func HashPlaintextFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
