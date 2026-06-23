package recording

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultShredPasses is the overwrite-pass count used when shredding is enabled but
// no explicit pass count is configured.
const DefaultShredPasses = 3

// shredBufferSize is the chunk size used when overwriting file contents. One random
// buffer is reused for the length of each pass — fast, and still random per pass.
const shredBufferSize = 1 << 20 // 1 MiB

// ShredFile securely deletes a single file: it overwrites the file's contents with
// random data `passes` times (syncing after each pass), truncates it, renames it to
// a random name to defeat filename recovery, then removes it. It is pure os/io so it
// behaves identically on Windows, Linux, and macOS.
//
// Best-effort only: on SSDs (wear-levelling remaps blocks), copy-on-write
// filesystems (ZFS/Btrfs/APFS), and snapshotted or journalled volumes, the original
// blocks may survive an overwrite. Full-disk encryption is the only real guarantee.
func ShredFile(path string, passes int) error {
	return shredFile(path, passes, time.Time{})
}

// shredFile is ShredFile with an optional overwrite deadline. When the deadline passes
// mid-shred (zero = no limit), the remaining overwrite is abandoned and the file is
// renamed+unlinked as-is — so a huge file or a slow/near-full disk can never make the
// shred run unbounded (used by the factory reset, which must always finish + restart).
func shredFile(path string, passes int, deadline time.Time) error {
	if passes < 1 {
		passes = 1
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("shred: %s is a directory", path)
	}
	size := info.Size()

	if err := overwrite(path, size, passes, deadline); err != nil {
		return err
	}

	// Rename to a random name in the same directory so the original filename can't
	// be recovered from the directory entry, then unlink.
	final := path
	if renamed := filepath.Join(filepath.Dir(path), "."+randomName()+".shred"); os.Rename(path, renamed) == nil {
		final = renamed
	}
	return os.Remove(final)
}

func overwrite(path string, size int64, passes int, deadline time.Time) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	expired := func() bool { return !deadline.IsZero() && time.Now().After(deadline) }

	buf := make([]byte, shredBufferSize)
	for p := 0; p < passes; p++ {
		if expired() {
			break
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		remaining := size
		for remaining > 0 {
			if expired() { // bound a single very large file mid-pass too
				break
			}
			n := int64(len(buf))
			if n > remaining {
				n = remaining
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			remaining -= n
		}
		if err := f.Sync(); err != nil {
			return err
		}
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	return f.Sync()
}

// SecureRemove deletes path, shredding it with the given pass count when passes > 0
// and otherwise removing it normally. A missing file is treated as success, so it is
// a safe drop-in for os.Remove on best-effort cleanup paths.
func SecureRemove(path string, passes int) error {
	return SecureRemoveDeadline(path, passes, time.Time{})
}

// SecureRemoveDeadline is SecureRemove with an overwrite deadline (zero = no limit).
// Once the deadline passes the secure overwrite is skipped and the file is simply
// unlinked, so a bulk wipe of large files on a slow/near-full disk always completes in
// bounded time rather than appearing to hang. The file is still deleted either way.
func SecureRemoveDeadline(path string, passes int, deadline time.Time) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	var err error
	if passes > 0 {
		err = shredFile(path, passes, deadline)
	} else {
		err = os.Remove(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func randomName() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "tmp"
	}
	return hex.EncodeToString(b[:])
}
