package recording

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/atrest"
)

// These tests drive the REAL finalize path with a REAL ffmpeg: they generate an actual
// MPEG-TS segment, remux it, and assert the resulting MP4 is complete, encrypted, and
// playable. The pure-logic tests in segment_finalize_test.go cover which branch runs;
// these cover whether the bytes on disk are actually right.
//
// They skip when no ffmpeg can be found, so a CGO-free / toolchain-only CI stays green.

// findFFmpeg locates an ffmpeg to test with: an explicit override, then PATH, then the
// repo-local copy that mymatasan's in-app installer downloads into .tools/.
func findFFmpeg(t *testing.T) string {
	t.Helper()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	if p := strings.TrimSpace(os.Getenv("KOPIV2_TEST_FFMPEG")); p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	// Repo-local install (infra/recording -> repo root is two levels up).
	matches, _ := filepath.Glob(filepath.Join("..", "..", ".tools", "ffmpeg", "*", "bin", "ffmpeg"+exe))
	if len(matches) > 0 {
		if abs, err := filepath.Abs(matches[0]); err == nil {
			return abs
		}
	}
	t.Skip("ffmpeg not found (set KOPIV2_TEST_FFMPEG to run the end-to-end finalize tests)")
	return ""
}

// makeTS writes a real, valid MPEG-TS segment of the given duration.
func makeTS(t *testing.T, ffmpeg, path string, seconds int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10",
		"-t", strconv.Itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-f", "mpegts", "-y", filepath.ToSlash(path),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make test .ts: %v: %s", err, out)
	}
}

// assertPlayableMP4 decrypts (if needed) and probes the file, failing unless ffprobe can
// read a real video stream out of it — i.e. it has a moov atom and is not truncated.
func assertPlayableMP4(t *testing.T, r *rtspRecorder, ffmpeg, path string) {
	t.Helper()
	probePath := path
	if r.cfg.Cipher != nil && fileIsEncrypted(path) {
		tmp, cleanup, err := r.cfg.Cipher.DecryptToTempFile(path)
		if err != nil {
			t.Fatalf("decrypt segment for probe: %v", err)
		}
		defer cleanup()
		probePath = tmp
	}
	codec := probeVideoCodec(ffmpeg, probePath)
	if codec == "" {
		t.Fatalf("ffprobe could not read a video stream from %s — the MP4 is truncated or corrupt", path)
	}
	if codec != "h264" {
		t.Fatalf("unexpected codec %q, want h264", codec)
	}
}

func newFFmpegRecorder(t *testing.T, ffmpeg string, sink SegmentSink, cipher *atrest.Cipher) *rtspRecorder {
	t.Helper()
	r := newTestRecorder(t, sink, cipher)
	r.cfg.FFmpegPath = ffmpeg
	return r
}

// The happy path, for real: a complete .ts becomes a complete, encrypted, playable .mp4,
// the source .ts is removed, no .part is left behind, and the sink records the ciphertext
// size and the probed codec.
func TestRemux_EndToEnd_ProducesEncryptedPlayableMP4(t *testing.T) {
	ffmpeg := findFFmpeg(t)
	sink := &fakeSink{}
	cipher := testCipher(t)
	r := newFFmpegRecorder(t, ffmpeg, sink, cipher)

	makeTS(t, ffmpeg, filepath.Join(r.liveDir, "20260101_000000.ts"), 2)
	// The newest file is treated as still being written, so it must exist but is skipped.
	makeTS(t, ffmpeg, filepath.Join(r.liveDir, "20260101_001500.ts"), 1)

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	mp4Path := filepath.Join(r.liveDir, "20260101_000000.mp4")
	if _, err := os.Stat(mp4Path); err != nil {
		t.Fatalf("finalized .mp4 was not published: %v", err)
	}
	if !fileIsEncrypted(mp4Path) {
		t.Fatal("finalized segment is not encrypted at rest")
	}
	assertPlayableMP4(t, r, ffmpeg, mp4Path)

	if _, err := os.Stat(filepath.Join(r.liveDir, "20260101_000000.ts")); !os.IsNotExist(err) {
		t.Fatal("source .ts should be removed once the .mp4 is published")
	}
	if _, err := os.Stat(mp4Path + partSuffix); !os.IsNotExist(err) {
		t.Fatal(".part file left behind after a successful remux")
	}

	saved := sink.saved()
	if len(saved) != 1 {
		t.Fatalf("expected exactly 1 persisted segment, got %d", len(saved))
	}
	fi, err := os.Stat(mp4Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if saved[0].FilePath != mp4Path {
		t.Fatalf("persisted path = %q, want %q", saved[0].FilePath, mp4Path)
	}
	if saved[0].FileSize != fi.Size() {
		t.Fatalf("persisted size %d != on-disk ciphertext size %d", saved[0].FileSize, fi.Size())
	}
	if saved[0].Codec != "h264" {
		t.Fatalf("persisted codec = %q, want h264", saved[0].Codec)
	}
	// The probed duration must win over the inferred one.
	if d := saved[0].EndedAt - saved[0].StartedAt; d < 1 || d > 4 {
		t.Fatalf("segment duration %ds is not the real ~2s media duration", d)
	}
}

// THE regression test, end-to-end.
//
// Simulates exactly what a crash mid-remux leaves behind: an intact .ts plus a truncated
// .mp4. The old code adopted the truncated .mp4 as canonical footage (unencrypted) and
// abandoned the .ts to the retention purge. The good footage must win: the .mp4 on disk
// afterwards must be a real, playable, encrypted remux OF THE TS — not the corrupt file.
func TestRemux_CrashRecovery_TruncatedMP4IsReplacedFromTheTS(t *testing.T) {
	ffmpeg := findFFmpeg(t)
	sink := &fakeSink{}
	cipher := testCipher(t)
	r := newFFmpegRecorder(t, ffmpeg, sink, cipher)

	tsPath := filepath.Join(r.liveDir, "20260101_000000.ts")
	mp4Path := filepath.Join(r.liveDir, "20260101_000000.mp4")
	makeTS(t, ffmpeg, tsPath, 2)
	makeTS(t, ffmpeg, filepath.Join(r.liveDir, "20260101_001500.ts"), 1)

	// Build a real MP4 then truncate it — a faithful stand-in for an ffmpeg killed
	// mid-write. Non-empty, so the old `Size() > 0` guard would have happily adopted it.
	reference := filepath.Join(t.TempDir(), "reference.mp4")
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-i", filepath.ToSlash(tsPath), "-c", "copy", "-movflags", "+faststart",
		"-y", filepath.ToSlash(reference))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build reference mp4: %v: %s", err, out)
	}
	full, err := os.ReadFile(reference)
	if err != nil {
		t.Fatalf("read reference mp4: %v", err)
	}
	if err := os.WriteFile(mp4Path, full[:len(full)/2], 0644); err != nil {
		t.Fatalf("write truncated mp4: %v", err)
	}
	if fi, _ := os.Stat(mp4Path); fi == nil || fi.Size() == 0 {
		t.Fatal("test setup: truncated mp4 must be non-empty to reproduce the bug")
	}

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	// The corrupt file must have been replaced by a real remux of the .ts.
	assertPlayableMP4(t, r, ffmpeg, mp4Path)
	if !fileIsEncrypted(mp4Path) {
		t.Fatal("recovered segment is not encrypted at rest — a crypto-erase would not shred it")
	}
	if _, err := os.Stat(tsPath); !os.IsNotExist(err) {
		t.Fatal("source .ts should be gone once its footage is safely published")
	}
	if _, err := os.Stat(mp4Path + partSuffix); !os.IsNotExist(err) {
		t.Fatal(".part left behind")
	}

	saved := sink.saved()
	if len(saved) != 1 {
		t.Fatalf("expected exactly 1 persisted segment, got %d", len(saved))
	}
	if d := saved[0].EndedAt - saved[0].StartedAt; d < 1 || d > 4 {
		t.Fatalf("persisted duration %ds — the truncated file was adopted instead of the real footage", d)
	}
}

// The crash-safety invariant itself: if the remux is interrupted, NO bare .mp4 may appear
// on disk (only a .part, which is garbage-collected), and the source .ts must survive so
// the next run can retry. This is what makes "a bare .mp4 is always complete" true.
func TestRemux_InterruptedNeverPublishesAPartialMP4(t *testing.T) {
	ffmpeg := findFFmpeg(t)
	sink := &fakeSink{}
	r := newFFmpegRecorder(t, ffmpeg, sink, testCipher(t))

	tsPath := filepath.Join(r.liveDir, "20260101_000000.ts")
	mp4Path := filepath.Join(r.liveDir, "20260101_000000.mp4")
	makeTS(t, ffmpeg, tsPath, 2)
	makeTS(t, ffmpeg, filepath.Join(r.liveDir, "20260101_001500.ts"), 1)

	// A cancelled context is what a shutdown or a reconfigure delivers to the remux.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.saveCompletedSegments(ctx)
	r.remuxWG.Wait()

	if _, err := os.Stat(mp4Path); !os.IsNotExist(err) {
		t.Fatal("an interrupted remux published a bare .mp4 — the crash-safety invariant is broken")
	}
	if _, err := os.Stat(tsPath); err != nil {
		t.Fatalf("source .ts must survive an interrupted remux so it can be retried: %v", err)
	}
	if len(sink.saved()) != 0 {
		t.Fatalf("an interrupted remux persisted a segment: %+v", sink.saved())
	}

	// And the retry, on a live context, must now succeed and publish real footage.
	if !r.claimStem("20260101_000000") {
		t.Fatal("stem must be retryable after an interrupted remux")
	}
	r.finishStem(context.Background(), liveSegInfo{stem: "20260101_000000", tsPath: tsPath},
		r.remuxSegment(context.Background(), liveSegInfo{stem: "20260101_000000", tsPath: tsPath, startedAt: 1767225600}, 1767225602))

	assertPlayableMP4(t, r, ffmpeg, mp4Path)
	if len(sink.saved()) != 1 {
		t.Fatalf("retry should have persisted the segment, got %d", len(sink.saved()))
	}
	// Stale partials must never survive a recorder restart.
	r.cleanStaleParts()
	if _, err := os.Stat(mp4Path + partSuffix); !os.IsNotExist(err) {
		t.Fatal(".part survived cleanStaleParts")
	}
}
