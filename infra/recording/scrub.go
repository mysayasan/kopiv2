package recording

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
	"github.com/shirou/gopsutil/v3/disk"
)

// TrimVolume asks the OS to TRIM/discard the free space on the volume containing path,
// so SSD/NVMe controllers physically erase the cells of files just deleted from it.
// This is the device-appropriate secure-erase for flash storage — overwriting can't
// reliably reach the original cells through wear-levelling/over-provisioning, but TRIM
// lets the controller reclaim+zero the freed blocks. Best-effort and OS-specific:
// Windows uses Optimize-Volume -ReTrim, Linux uses fstrim, macOS is a no-op (TRIM is
// automatic), and it's harmless on HDDs (the FS/driver ignores discard). It typically
// needs admin/root, so failures are returned for an advisory warning, not treated as
// fatal.
func TrimVolume(ctx context.Context, path string) error {
	switch runtime.GOOS {
	case "windows":
		letter := strings.TrimSuffix(strings.TrimSpace(filepath.VolumeName(path)), ":")
		if letter == "" {
			return fmt.Errorf("could not determine a drive letter for %q", path)
		}
		trim := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive",
			"-Command", fmt.Sprintf("Optimize-Volume -DriveLetter %s -ReTrim -ErrorAction Stop", letter))
		procutil.HideWindow(trim)
		out, err := trim.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Optimize-Volume: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	case "linux":
		out, err := exec.CommandContext(ctx, "fstrim", path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("fstrim: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return nil // darwin: automatic TRIM; other OSes: no portable command, skip
	}
}

// scrubBufferSize is the chunk written when filling free space.
const scrubBufferSize = 4 << 20 // 4 MiB

// ScrubFreeSpace overwrites the free space on dir's volume by writing random data into
// a temp file under dir until the volume is near-full (leaving minFreeBytes headroom)
// or the deadline passes, then deleting it. This scrubs the residual blocks of files
// that were just unlinked from that volume — the "secure overwrite" half of a
// delete-first secure wipe. Repeats for `passes`. Returns total bytes written.
//
// Best-effort only and NOT a guarantee on SSDs / copy-on-write or TRIM-backed
// filesystems (the controller may retain the original blocks) — see the ShredFile note.
// The real protections are the instant unlink that precedes this and full-disk
// encryption. minFreeBytes (0 = 1 GiB default) keeps the volume from wedging fully.
func ScrubFreeSpace(dir string, passes int, deadline time.Time, minFreeBytes uint64) (int64, error) {
	if passes < 1 {
		passes = 1
	}
	if minFreeBytes == 0 {
		minFreeBytes = 1 << 30 // 1 GiB headroom so we never fully wedge a volume
	}
	tmpDir := filepath.Join(dir, ".wipe_scrub")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	buf := make([]byte, scrubBufferSize)
	if _, err := rand.Read(buf); err != nil {
		return 0, err
	}

	var total int64
	for p := 0; p < passes; p++ {
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}
		n, err := scrubPass(filepath.Join(tmpDir, fmt.Sprintf("fill_%d.bin", p)), dir, buf, deadline, minFreeBytes)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// scrubPass fills one file with random data until the volume nears full (minFreeBytes
// headroom) or the deadline passes, then deletes it. A write error (e.g. ENOSPC) ends
// the pass normally — the goal is to consume free space, so running out is expected.
func scrubPass(path, volDir string, buf []byte, deadline time.Time, minFreeBytes uint64) (int64, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		f.Close()
		os.Remove(path)
	}()

	var written, sinceSync, sinceFreeCheck int64
	first := true
	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}
		if first || sinceFreeCheck >= 64<<20 {
			if u, uerr := disk.Usage(volDir); uerr == nil && u.Free <= minFreeBytes {
				break
			}
			sinceFreeCheck = 0
			first = false
		}
		n, werr := f.Write(buf)
		written += int64(n)
		sinceSync += int64(n)
		sinceFreeCheck += int64(n)
		if werr != nil { // disk full or other write error — pass is done
			_ = f.Sync()
			return written, nil
		}
		if sinceSync >= 64<<20 {
			_ = f.Sync()
			sinceSync = 0
		}
	}
	_ = f.Sync()
	return written, nil
}
