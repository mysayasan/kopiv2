package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultMaxLineBytes = 8192

// Config controls runtime file logging.
type Config struct {
	Enabled      bool
	Path         string
	MaxLineBytes int
	// MaxFileSizeMb caps a single log FILE. Rotation was previously by calendar day
	// only, so one chatty or hostile day could fill the disk long before the retention
	// window (which deletes whole days) became relevant. Zero or negative means no cap,
	// which is the pre-existing behaviour and stays the default.
	MaxFileSizeMb int
}

// Entry is one structured runtime log line.
type Entry struct {
	Timestamp int64  `json:"timestamp"`
	Time      string `json:"time"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	OS        string `json:"os"`
}

// Logger writes service/runtime logs and can list persisted entries.
type Logger interface {
	io.Writer
	io.Closer
	Debugf(source string, format string, args ...any)
	Infof(source string, format string, args ...any)
	Warnf(source string, format string, args ...any)
	Errorf(source string, format string, args ...any)
	List(ctx context.Context, limit uint64, offset uint64) ([]Entry, uint64, error)
	DeleteByMonth(ctx context.Context, year int, month int) (uint64, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (uint64, error)
	Path() string
}

type fileLogger struct {
	mu           sync.Mutex
	file         *os.File
	path         string
	activePath   string
	enabled      bool
	maxLineBytes int
	// maxFileBytes is the per-file cap, 0 = uncapped.
	maxFileBytes int64
	// activeSize tracks bytes in the current file so the cap can be checked without a
	// stat() on every single line.
	activeSize int64
	// activeSeq is the sequence suffix of the current file: 0 means the plain
	// `<stem>-<date>.log`, N>0 means `<stem>-<date>.N.log`.
	activeSeq int
	// activeDay is the date the current file belongs to, so a day change is detected
	// without re-deriving it from the path.
	activeDay string
}

// NewFileLogger creates a cross-platform JSON-lines logger.
func NewFileLogger(cfg Config) (Logger, error) {
	maxLineBytes := cfg.MaxLineBytes
	if maxLineBytes <= 0 {
		maxLineBytes = defaultMaxLineBytes
	}

	l := &fileLogger{
		path:         filepath.Clean(cfg.Path),
		enabled:      cfg.Enabled,
		maxLineBytes: maxLineBytes,
		maxFileBytes: int64(cfg.MaxFileSizeMb) * 1024 * 1024,
	}
	if cfg.MaxFileSizeMb <= 0 {
		l.maxFileBytes = 0
	}

	if !cfg.Enabled {
		return l, nil
	}

	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf("logging path is required when logging is enabled")
	}

	if err := os.MkdirAll(filepath.Dir(l.path), 0750); err != nil {
		return nil, err
	}

	if err := l.ensureActiveFileLocked(time.Now().UTC()); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *fileLogger) Debugf(source string, format string, args ...any) {
	l.logf("debug", source, format, args...)
}

func (l *fileLogger) Infof(source string, format string, args ...any) {
	l.logf("info", source, format, args...)
}

func (l *fileLogger) Warnf(source string, format string, args ...any) {
	l.logf("warn", source, format, args...)
}

func (l *fileLogger) Errorf(source string, format string, args ...any) {
	l.logf("error", source, format, args...)
}

func (l *fileLogger) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message == "" {
		return len(p), nil
	}
	now := time.Now().UTC()
	l.writeEntry(Entry{
		Timestamp: now.Unix(),
		Time:      now.Format(time.RFC3339Nano),
		Level:     "info",
		Source:    "std",
		Message:   truncate(message, l.maxLineBytes),
		OS:        runtime.GOOS,
	})
	return len(p), nil
}

func (l *fileLogger) List(ctx context.Context, limit uint64, offset uint64) ([]Entry, uint64, error) {
	if !l.enabled || strings.TrimSpace(l.path) == "" {
		return []Entry{}, 0, nil
	}

	files, err := l.logFiles()
	if err != nil {
		return nil, 0, err
	}

	entries := []Entry{}
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, 0, err
		}

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1024), l.maxLineBytes+4096)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				file.Close()
				return nil, 0, err
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			entry := Entry{}
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				entry = Entry{
					Timestamp: 0,
					Time:      "",
					Level:     "info",
					Source:    "legacy",
					Message:   truncate(line, l.maxLineBytes),
					OS:        runtime.GOOS,
				}
			}
			entries = append(entries, entry)
		}
		if err := scanner.Err(); err != nil {
			file.Close()
			return nil, 0, err
		}
		file.Close()
	}

	// Newest first. Entries were appended in read order, which IS chronological: files are
	// globbed oldest-to-newest and lines within a file are append-only. So reversing gives
	// newest-first, and a STABLE sort on the second-resolution timestamp then preserves
	// that order for everything written inside the same second.
	//
	// The previous tiebreak compared the RFC3339 `Time` STRING, which fails two ways.
	// Go trims trailing zeros from the fraction, so ".1Z" vs ".1000001Z" compares as
	// ".1" > ".1000001" — backwards. And on Windows the clock is coarse enough
	// (~0.5–15 ms) that two adjacent writes get a byte-identical Time, leaving the tie
	// unresolved and the stable sort returning them OLDEST first. That is why
	// TestFileLoggerWritesAndListsNewestFirst failed ~90% of the time on Windows while
	// passing on Linux CI. Read order needs no clock and cannot tie.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})
	total := uint64(len(entries))
	if offset >= total {
		return []Entry{}, total, nil
	}

	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	return entries[int(offset):int(end)], total, nil
}

func (l *fileLogger) DeleteByMonth(ctx context.Context, year int, month int) (uint64, error) {
	if year < 1 {
		return 0, fmt.Errorf("year is required")
	}
	if month < 1 || month > 12 {
		return 0, fmt.Errorf("month must be between 1 and 12")
	}
	if !l.enabled || strings.TrimSpace(l.path) == "" {
		return 0, nil
	}

	files, err := l.logFiles()
	if err != nil {
		return 0, err
	}

	deleted := uint64(0)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}

		fileYear, fileMonth, ok := l.dateFromPath(path)
		if !ok || fileYear != year || fileMonth != month {
			continue
		}

		removed, err := l.removeLogFile(path)
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
		}
	}

	return deleted, nil
}

func (l *fileLogger) DeleteOlderThan(ctx context.Context, cutoff time.Time) (uint64, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("cutoff is required")
	}
	if !l.enabled || strings.TrimSpace(l.path) == "" {
		return 0, nil
	}

	files, err := l.logFiles()
	if err != nil {
		return 0, err
	}

	deleted := uint64(0)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}

		fileDate, ok := l.dateValueFromPath(path)
		if !ok || !fileDate.Before(cutoff) {
			continue
		}

		removed, err := l.removeLogFile(path)
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
		}
	}

	return deleted, nil
}

func (l *fileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *fileLogger) Path() string {
	return l.path
}

func (l *fileLogger) logf(level string, source string, format string, args ...any) {
	now := time.Now().UTC()
	l.writeEntry(Entry{
		Timestamp: now.Unix(),
		Time:      now.Format(time.RFC3339Nano),
		Level:     strings.ToLower(strings.TrimSpace(level)),
		Source:    strings.TrimSpace(source),
		Message:   truncate(fmt.Sprintf(format, args...), l.maxLineBytes),
		OS:        runtime.GOOS,
	})
}

func (l *fileLogger) writeEntry(entry Entry) {
	if entry.Source == "" {
		entry.Source = "app"
	}
	if entry.Level == "" {
		entry.Level = "info"
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintln(os.Stdout, string(line))
	if l.enabled {
		if err := l.ensureActiveFileLocked(time.Now().UTC()); err != nil {
			return
		}
		n, err := fmt.Fprintln(l.file, string(line))
		if err != nil {
			return
		}
		// Tracked rather than stat()ed per line: the cap only needs to be approximately
		// right, and a syscall on every log line would cost more than the cap saves. A
		// line is never split across files, so a file can exceed the cap by at most one
		// line — which is the correct trade for a size guard.
		l.activeSize += int64(n)
	}
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (l *fileLogger) ensureActiveFileLocked(now time.Time) error {
	day := now.Format("2006-01-02")

	// A new day always starts a fresh file at sequence 0, regardless of the cap.
	if l.file != nil && l.activeDay == day {
		if l.maxFileBytes <= 0 || l.activeSize < l.maxFileBytes {
			return nil
		}
		// Cap reached: advance to the next sequence for the SAME day.
		return l.openLocked(day, l.activeSeq+1)
	}

	// First open of this day (or of the process). With a cap configured, resume at the
	// highest existing sequence rather than at 0 — otherwise a restart would reopen and
	// keep appending to `<stem>-<day>.log`, blowing straight past the cap on a file that
	// is already full.
	seq := 0
	if l.maxFileBytes > 0 {
		seq = l.highestSeqForDay(day)
	}
	return l.openLocked(day, seq)
}

// openLocked closes the current file and opens `<stem>-<day>.<seq>.log`, seeding
// activeSize from whatever is already on disk so an append-reopen respects the cap.
func (l *fileLogger) openLocked(day string, seq int) error {
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return err
		}
		l.file = nil
	}

	activePath := l.datedSeqPath(day, seq)
	file, err := os.OpenFile(activePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}

	var size int64
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}

	l.file = file
	l.activePath = activePath
	l.activeDay = day
	l.activeSeq = seq
	l.activeSize = size
	return nil
}

// highestSeqForDay returns the largest sequence already on disk for a day whose file is at
// or over the cap, so a restart continues where the previous process stopped instead of
// re-filling a full file.
func (l *fileLogger) highestSeqForDay(day string) int {
	seq := 0
	for {
		info, err := os.Stat(l.datedSeqPath(day, seq))
		if err != nil {
			// This sequence does not exist yet: it is the one to write to.
			return seq
		}
		if info.Size() < l.maxFileBytes {
			return seq
		}
		seq++
	}
}

func (l *fileLogger) datedPath(now time.Time) string {
	return l.datedSeqPath(now.Format("2006-01-02"), 0)
}

// datedSeqPath builds `<stem>-<day>.log` for seq 0 and `<stem>-<day>.<seq>.log` above it.
//
// Sequence 0 keeps the historic name, so an install that never enables the size cap sees
// byte-identical filenames to before and nothing about its log directory changes.
//
// CRITICAL: this shape must stay in lockstep with logFiles() and dateValueFromPath().
// Retention finds files through those two, so a name this function can produce but they
// cannot recognise is a file NOTHING will ever delete — which is precisely the disk
// exhaustion the size cap exists to prevent. Change all three together, and see
// TestSizeRotatedFilesAreStillPruned.
func (l *fileLogger) datedSeqPath(day string, seq int) string {
	dir := filepath.Dir(l.path)
	base := filepath.Base(l.path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if seq <= 0 {
		return filepath.Join(dir, fmt.Sprintf("%s-%s%s", stem, day, ext))
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.%d%s", stem, day, seq, ext))
}

func (l *fileLogger) logFiles() ([]string, error) {
	dir := filepath.Dir(l.path)
	base := filepath.Base(l.path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	matches, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%s-????-??-??%s", stem, ext)))
	if err != nil {
		return nil, err
	}

	// Size-rotated siblings: `<stem>-<date>.<seq>.log`. Without this second glob the
	// sequenced files are invisible to retention and would never be deleted — the exact
	// disk exhaustion the size cap exists to prevent. The pattern is loose (`.*`) and
	// dateValueFromPath does the real validation, so a stray `<stem>-<date>.bak.log`
	// is matched here and then correctly rejected there rather than deleted.
	seqMatches, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%s-????-??-??.*%s", stem, ext)))
	if err != nil {
		return nil, err
	}
	matches = append(matches, seqMatches...)

	if _, err := os.Stat(l.path); err == nil {
		matches = append(matches, l.path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	sort.Strings(matches)
	return matches, nil
}

func (l *fileLogger) dateFromPath(path string) (int, int, bool) {
	parsed, ok := l.dateValueFromPath(path)
	if !ok {
		return 0, 0, false
	}
	return parsed.Year(), int(parsed.Month()), true
}

func (l *fileLogger) dateValueFromPath(path string) (time.Time, bool) {
	base := filepath.Base(l.path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	fileBase := filepath.Base(path)
	if !strings.HasPrefix(fileBase, stem+"-") || !strings.HasSuffix(fileBase, ext) {
		return time.Time{}, false
	}

	datePart := strings.TrimSuffix(strings.TrimPrefix(fileBase, stem+"-"), ext)
	// Strip a size-rotation sequence suffix (`2026-07-29.3` -> `2026-07-29`) before
	// parsing. Only an all-digit suffix is accepted, so `2026-07-29.bak` is rejected
	// rather than silently treated as a log file and deleted by retention.
	if dot := strings.IndexByte(datePart, '.'); dot >= 0 {
		seq := datePart[dot+1:]
		if seq == "" || strings.IndexFunc(seq, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return time.Time{}, false
		}
		datePart = datePart[:dot]
	}
	parsed, err := time.Parse("2006-01-02", datePart)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (l *fileLogger) removeLogFile(path string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil && filepath.Clean(path) == l.activePath {
		if err := l.file.Close(); err != nil {
			return false, err
		}
		l.file = nil
		l.activePath = ""
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
