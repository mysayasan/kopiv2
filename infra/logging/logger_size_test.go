package logging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sizedLogger(t *testing.T, maxMb int) (*fileLogger, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	lg, err := NewFileLogger(Config{Enabled: true, Path: path, MaxFileSizeMb: maxMb})
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	fl, ok := lg.(*fileLogger)
	if !ok {
		t.Fatalf("expected *fileLogger, got %T", lg)
	}
	t.Cleanup(func() { _ = fl.Close() })
	return fl, dir
}

func logFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			out = append(out, e.Name())
		}
	}
	// Sort by SEQUENCE, not alphabetically: '1' (0x31) sorts before 'l' (0x6c), so a
	// plain sort puts "app-<day>.1.log" ahead of "app-<day>.log" and every index-based
	// assertion below would be off by one for a reason that has nothing to do with the
	// code under test.
	sort.Slice(out, func(i, j int) bool { return seqOf(out[i]) < seqOf(out[j]) })
	return out
}

// seqOf extracts the rotation sequence from a log filename; the plain file is 0.
func seqOf(name string) int {
	trimmed := strings.TrimSuffix(name, ".log")
	dot := strings.LastIndexByte(trimmed, '.')
	if dot < 0 {
		return 0
	}
	n, err := strconv.Atoi(trimmed[dot+1:])
	if err != nil {
		return 0
	}
	return n
}

// Rotation was previously by calendar day only, so one chatty or hostile day could fill
// the disk long before the retention window — which deletes whole days — became relevant.
func TestSizeCapRollsToASequencedFile(t *testing.T) {
	fl, dir := sizedLogger(t, 1) // 1 MiB
	line := strings.Repeat("x", 4096)
	for i := 0; i < 400; i++ { // ~1.6 MiB of payload
		fl.writeEntry(Entry{Level: "info", Source: "test", Message: line})
	}

	files := logFilesIn(t, dir)
	if len(files) < 2 {
		t.Fatalf("expected the cap to roll to a second file, got %v", files)
	}
	day := time.Now().UTC().Format("2006-01-02")
	if files[0] != "app-"+day+".log" {
		t.Fatalf("first file should keep the historic name, got %q", files[0])
	}
	if files[1] != "app-"+day+".1.log" {
		t.Fatalf("second file should be sequence 1, got %q", files[1])
	}

	// The cap is approximate by design (a line is never split), but it must be close.
	info, err := os.Stat(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() > 1024*1024+8192 {
		t.Fatalf("first file is %d bytes, well past the 1 MiB cap", info.Size())
	}
}

// THE ONE THAT MATTERS. Retention finds files through logFiles() and dateValueFromPath().
// A rotation that produces a name those two cannot recognise creates files NOTHING will
// ever delete — causing exactly the disk exhaustion the size cap was added to prevent.
// This is the test that makes the writer and the parsers stay in lockstep.
func TestSizeRotatedFilesAreStillPruned(t *testing.T) {
	fl, dir := sizedLogger(t, 1)

	// Hand-place aged files in BOTH shapes, dated well before any cutoff.
	old := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	for _, name := range []string{
		"app-" + old + ".log",
		"app-" + old + ".1.log",
		"app-" + old + ".2.log",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	// logFiles() must SEE the sequenced ones.
	found, err := fl.logFiles()
	if err != nil {
		t.Fatalf("logFiles: %v", err)
	}
	// Count only files carrying a numeric sequence. A `Contains(old+".")` check would
	// also match the plain `app-<old>.log` and report 3 — a test failing for its own
	// reason rather than the code's.
	var seenSeq int
	for _, p := range found {
		base := filepath.Base(p)
		if strings.HasPrefix(base, "app-"+old+".") && seqOf(base) > 0 {
			seenSeq++
		}
	}
	if seenSeq != 2 {
		t.Fatalf("logFiles() saw %d sequenced files, want 2 — they would be undeletable: %v", seenSeq, found)
	}

	// ...and DeleteOlderThan must actually remove them.
	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	deleted, err := fl.DeleteOlderThan(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted %d files, want 3 (the plain file plus BOTH sequenced ones)", deleted)
	}
	for _, name := range []string{"app-" + old + ".1.log", "app-" + old + ".2.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived retention — this is the undeletable-file bug", name)
		}
	}
}

// The loose glob deliberately matches more than it should, so the date parser has to be
// the strict gate. A file that merely looks similar must NOT be deleted.
func TestNonSequenceSuffixIsNotTreatedAsALogFile(t *testing.T) {
	fl, dir := sizedLogger(t, 1)
	old := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")

	keep := filepath.Join(dir, "app-"+old+".bak.log")
	if err := os.WriteFile(keep, []byte("precious\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := fl.dateValueFromPath(keep); ok {
		t.Fatal("a non-numeric suffix must not parse as a log file, or retention would delete it")
	}

	if _, err := fl.DeleteOlderThan(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a .bak.log file was deleted by retention: %v", err)
	}
}

// An uncapped logger must behave exactly as before: same single file, same historic name.
// Anyone who never sets the cap should see no change in their log directory at all.
func TestNoCapKeepsTheHistoricSingleFileBehaviour(t *testing.T) {
	fl, dir := sizedLogger(t, 0)
	line := strings.Repeat("y", 4096)
	for i := 0; i < 400; i++ {
		fl.writeEntry(Entry{Level: "info", Source: "test", Message: line})
	}

	files := logFilesIn(t, dir)
	if len(files) != 1 {
		t.Fatalf("an uncapped logger must write one file per day, got %v", files)
	}
	if want := "app-" + time.Now().UTC().Format("2006-01-02") + ".log"; files[0] != want {
		t.Fatalf("filename changed for uncapped logging: got %q want %q", files[0], want)
	}
}

// A restart must not reopen a file that is already full and append past the cap.
func TestRestartResumesAtTheNextSequence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	day := time.Now().UTC().Format("2006-01-02")

	// A full sequence-0 file, as a previous process would have left it.
	full := make([]byte, 1024*1024+16)
	for i := range full {
		full[i] = 'z'
	}
	if err := os.WriteFile(filepath.Join(dir, "app-"+day+".log"), full, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	lg, err := NewFileLogger(Config{Enabled: true, Path: path, MaxFileSizeMb: 1})
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	fl := lg.(*fileLogger)
	defer fl.Close()
	fl.writeEntry(Entry{Level: "info", Source: "test", Message: "after restart"})

	if got := filepath.Base(fl.activePath); got != "app-"+day+".1.log" {
		t.Fatalf("restart reopened %q; it must skip the full file and continue at .1.log", got)
	}
	info, err := os.Stat(filepath.Join(dir, "app-"+day+".log"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != int64(len(full)) {
		t.Fatalf("the already-full file was appended to: %d bytes", info.Size())
	}
}

// Sequences must keep climbing rather than wrapping back to an earlier file.
func TestSequencesAdvanceMonotonically(t *testing.T) {
	fl, dir := sizedLogger(t, 1)
	line := strings.Repeat("q", 4096)
	for i := 0; i < 900; i++ { // enough for ~3 files
		fl.writeEntry(Entry{Level: "info", Source: "test", Message: line})
	}

	files := logFilesIn(t, dir)
	day := time.Now().UTC().Format("2006-01-02")
	for i, name := range files {
		want := fmt.Sprintf("app-%s.%d.log", day, i)
		if i == 0 {
			want = "app-" + day + ".log"
		}
		if name != want {
			t.Fatalf("file %d is %q want %q (sequence gap or wrap): %v", i, name, want, files)
		}
	}
	if len(files) < 3 {
		t.Fatalf("expected at least 3 files, got %v", files)
	}
}
