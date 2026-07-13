package recording

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/atrest"
)

// fakeSink records every segment persisted, so tests can assert exactly what was
// (and crucially, what was NOT) written to the DB.
type fakeSink struct {
	mu   sync.Mutex
	got  []SegmentResult
	fail error
}

func (s *fakeSink) SaveSegment(_ context.Context, seg SegmentResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.got = append(s.got, seg)
	return nil
}

func (s *fakeSink) saved() []SegmentResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SegmentResult, len(s.got))
	copy(out, s.got)
	return out
}

// newTestRecorder builds a recorder rooted at a temp dir with a deliberately
// unresolvable ffmpeg, so remuxSegment fails at the resolve step and adoptSegment
// skips probing. That makes the finalization decisions — which is what these tests are
// about — deterministic and independent of whether ffmpeg is installed.
func newTestRecorder(t *testing.T, sink SegmentSink, cipher *atrest.Cipher) *rtspRecorder {
	t.Helper()
	root := t.TempDir()
	r := newRTSPRecorder(RecorderConfig{
		CameraId:      1,
		Enabled:       true,
		StoragePath:   root,
		FFmpegPath:    "definitely-not-a-real-ffmpeg-binary",
		RetentionDays: 7,
		Cipher:        cipher,
	}, sink)
	r.liveDir = filepath.Join(root, "cam1", "live")
	r.clipDir = filepath.Join(root, "cam1", "clips")
	if err := os.MkdirAll(r.liveDir, 0755); err != nil {
		t.Fatalf("mkdir liveDir: %v", err)
	}
	return r
}

func writeSeg(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func testCipher(t *testing.T) *atrest.Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := atrest.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

// The regression test for the footage-loss bug.
//
// An interrupted remux used to leave a truncated .mp4 (mdat, no moov) beside the
// intact .ts. listLiveSegments prefers .mp4, so on restart the corrupt file was adopted
// as the canonical footage for that window — persisted to the DB, and never encrypted —
// while the good .ts was abandoned and later shredded by the retention purge.
//
// A .ts sibling must now always win: the stem is treated as unfinalized and re-remuxed.
func TestSaveCompletedSegments_TruncatedMP4WithLiveTSIsNeverAdopted(t *testing.T) {
	sink := &fakeSink{}
	r := newTestRecorder(t, sink, nil)

	// Stem 1: an interrupted remux — intact .ts plus a truncated, non-empty .mp4.
	tsPath := writeSeg(t, r.liveDir, "20260101_000000.ts", []byte("intact transport stream payload"))
	mp4Path := writeSeg(t, r.liveDir, "20260101_000000.mp4", []byte("truncated mp4, no moov atom"))
	// Stem 2 is the segment currently being written, so stem 1 counts as complete.
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	// The truncated MP4 must never reach the sink. (The remux itself fails here — no
	// ffmpeg — which is the point: a failed finalize must persist NOTHING rather than
	// fall back to adopting the corrupt file.)
	for _, seg := range sink.saved() {
		if seg.FilePath == mp4Path {
			t.Fatalf("truncated mp4 was adopted as canonical footage: %+v", seg)
		}
	}

	// The good TS must survive so the next attempt can re-remux it.
	if _, err := os.Stat(tsPath); err != nil {
		t.Fatalf("source .ts was destroyed after a failed finalize: %v", err)
	}
}

// A failed finalize must leave the stem retryable. Previously the stem was marked saved
// BEFORE the remux was attempted, so any failure stranded it permanently — no retry, no
// DB row, and retention eventually shredded the file.
func TestFinishStem_FailureLeavesStemRetryable(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)
	f := liveSegInfo{stem: "20260101_000000", tsPath: filepath.Join(r.liveDir, "20260101_000000.ts")}

	if !r.claimStem(f.stem) {
		t.Fatal("first claim should succeed")
	}
	r.finishStem(context.Background(), f, remuxFailed)

	if !r.claimStem(f.stem) {
		t.Fatal("a failed stem must be retryable on the next watch tick")
	}
}

// A stem that keeps failing must stop respawning ffmpeg every 5 s, and its source TS
// must be preserved rather than left in live/ for the retention purge to shred.
func TestFinishStem_QuarantinesAfterMaxAttempts(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)
	tsPath := writeSeg(t, r.liveDir, "20260101_000000.ts", []byte("unreadable"))
	f := liveSegInfo{stem: "20260101_000000", tsPath: tsPath}

	for i := 0; i < maxRemuxAttempts; i++ {
		if !r.claimStem(f.stem) {
			t.Fatalf("claim %d should succeed", i)
		}
		r.finishStem(context.Background(), f, remuxFailed)
	}

	if r.claimStem(f.stem) {
		t.Fatal("stem should be given up on after maxRemuxAttempts, not retried forever")
	}
	if _, err := os.Stat(tsPath); !os.IsNotExist(err) {
		t.Fatal("source .ts should have been moved out of live/")
	}
	quarantined := filepath.Join(filepath.Dir(r.liveDir), "quarantine", "20260101_000000.ts")
	if _, err := os.Stat(quarantined); err != nil {
		t.Fatalf("footage was not preserved in quarantine: %v", err)
	}
}

// A cancelled remux (shutdown or reconfigure) is not the segment's fault and must not
// burn a retry attempt — otherwise a few restarts would quarantine healthy segments.
func TestFinishStem_CancellationDoesNotCountAsFailure(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)
	f := liveSegInfo{stem: "20260101_000000", tsPath: filepath.Join(r.liveDir, "20260101_000000.ts")}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for i := 0; i < maxRemuxAttempts+2; i++ {
		if !r.claimStem(f.stem) {
			t.Fatalf("cancelled attempts must stay retryable (iteration %d)", i)
		}
		r.finishStem(ctx, f, remuxFailed)
	}
	if r.segFails[f.stem] != 0 {
		t.Fatalf("cancellation counted as a segment failure: %d", r.segFails[f.stem])
	}
}

// The restart-adoption path never encrypted the segment it adopted, so a segment that
// went through it sat plaintext on disk and would survive a crypto-erase wipe.
func TestAdoptSegment_EncryptsPlaintextBeforeSaving(t *testing.T) {
	sink := &fakeSink{}
	cipher := testCipher(t)
	r := newTestRecorder(t, sink, cipher)

	// A bare, plaintext .mp4 with no .ts sibling: a segment finalized by an older build.
	mp4Path := writeSeg(t, r.liveDir, "20260101_000000.mp4", []byte("plaintext finalized segment"))
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	if !fileIsEncrypted(mp4Path) {
		t.Fatal("adopted segment was left unencrypted at rest — crypto-erase would not shred it")
	}
	saved := sink.saved()
	if len(saved) != 1 || saved[0].FilePath != mp4Path {
		t.Fatalf("expected the adopted segment to be persisted, got %+v", saved)
	}
	// The recorded size must be the ciphertext size, or disk accounting drifts.
	fi, err := os.Stat(mp4Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if saved[0].FileSize != fi.Size() {
		t.Fatalf("persisted size %d != on-disk ciphertext size %d", saved[0].FileSize, fi.Size())
	}
}

// An already-encrypted segment must be adopted as-is, not double-encrypted.
func TestAdoptSegment_AlreadyEncryptedIsNotReEncrypted(t *testing.T) {
	sink := &fakeSink{}
	cipher := testCipher(t)
	r := newTestRecorder(t, sink, cipher)

	mp4Path := writeSeg(t, r.liveDir, "20260101_000000.mp4", []byte("finalized segment"))
	if err := cipher.EncryptFileInPlace(mp4Path); err != nil {
		t.Fatalf("EncryptFileInPlace: %v", err)
	}
	before, err := os.ReadFile(mp4Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	after, err := os.ReadFile(mp4Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("an already-encrypted segment was encrypted a second time")
	}
	if len(sink.saved()) != 1 {
		t.Fatalf("expected the segment to be persisted once, got %d", len(sink.saved()))
	}
}

// A failed DB write must leave the file alone and stay retryable — the footage is on
// disk and complete, so the next tick should retry persistence, not re-encode or drop it.
func TestAdoptSegment_SinkFailureIsRetriedNotDropped(t *testing.T) {
	sink := &fakeSink{fail: context.DeadlineExceeded}
	r := newTestRecorder(t, sink, nil)

	mp4Path := writeSeg(t, r.liveDir, "20260101_000000.mp4", []byte("finalized segment"))
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	if _, err := os.Stat(mp4Path); err != nil {
		t.Fatalf("segment file was removed after a failed DB save: %v", err)
	}
	if !r.claimStem("20260101_000000") {
		t.Fatal("a segment whose DB save failed must remain retryable")
	}
}

// Partial MP4s from an interrupted remux are garbage (their .ts is still on disk and
// will be re-remuxed), and must not be mistaken for finalized segments.
func TestCleanStaleParts(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)
	part := writeSeg(t, r.liveDir, "20260101_000000.mp4"+partSuffix, []byte("partial"))
	keep := writeSeg(t, r.liveDir, "20260101_000000.ts", []byte("source"))

	r.cleanStaleParts()

	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("stale .part should have been removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("the source .ts must be kept — it is what the retry remuxes from")
	}
	// A .part must never be picked up as a segment in its own right.
	for _, f := range r.listLiveSegments() {
		if f.stem != "20260101_000000" {
			t.Fatalf("unexpected segment discovered: %q", f.stem)
		}
	}
}

// The claim is what stops two ffmpeg processes remuxing one stem onto the same output
// file: a remux can outlast the 5 s watch tick, and Manager.Configure can start a new
// recorder on the same live directory. Exactly one claimant may win.
func TestClaimStem_IsExclusiveUnderConcurrentTicks(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)

	const goroutines = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0

	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if r.claimStem("20260101_000000") {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if won != 1 {
		t.Fatalf("expected exactly one claimant, got %d — concurrent remuxes would race the same output file", won)
	}
}

// Quarantined footage must still age out, or a persistently failing camera fills the disk.
func TestPurgeQuarantine_HonoursRetention(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)
	qdir := filepath.Join(filepath.Dir(r.liveDir), "quarantine")
	if err := os.MkdirAll(qdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := writeSeg(t, qdir, "20200101_000000.ts", []byte("ancient"))
	recent := writeSeg(t, qdir, time.Now().Format(segTimeFormat)+".ts", []byte("fresh"))

	r.purgeQuarantine(time.Now().AddDate(0, 0, -7))

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired quarantined segment should have been purged")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatal("quarantined segment inside the retention window must be kept")
	}
}
