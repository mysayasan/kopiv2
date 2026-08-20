// Command benchseal prepares recorded segments for a bench the way the recorder does:
// hash the PLAINTEXT mp4, then encrypt it at rest.
//
// It exists so the evidence-export bench exercises the real decrypt → concat → verify path
// against real video, instead of a stand-in. Reimplementing the at-rest format in the
// bench script would have tested my reimplementation, not the product; this uses
// infra/atrest, which is what the recorder itself calls.
//
// Usage: benchseal <keyPath> <file.mp4>...
// Prints one "path<TAB>sha256<TAB>ciphertextBytes" line per file.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/mysayasan/kopiv2/infra/atrest"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: benchseal <keyPath> <file.mp4>...")
		os.Exit(2)
	}
	keyPath := os.Args[1]

	out, err := atrest.OpenForStartup(keyPath, keyPath+".recovery", atrest.ProtectorConfig{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "open keystore:", err)
		os.Exit(1)
	}
	cipher := out.KeyStore.Cipher()
	if cipher == nil {
		fmt.Fprintln(os.Stderr, "keystore has no cipher (encryption disabled?)")
		os.Exit(1)
	}

	for _, path := range os.Args[2:] {
		// Hash the plaintext first — that ordering is the whole point of the feature.
		sum, err := hashFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hash:", err)
			os.Exit(1)
		}
		if err := cipher.EncryptFileInPlace(path); err != nil {
			fmt.Fprintln(os.Stderr, "encrypt:", err)
			os.Exit(1)
		}
		fi, err := os.Stat(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stat:", err)
			os.Exit(1)
		}
		fmt.Printf("%s\t%s\t%d\n", path, sum, fi.Size())
	}
}

func hashFile(path string) (string, error) {
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
