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

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Timestamp == entries[j].Timestamp {
			return entries[i].Time > entries[j].Time
		}
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
		fmt.Fprintln(l.file, string(line))
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
	activePath := l.datedPath(now)
	if l.file != nil && l.activePath == activePath {
		return nil
	}

	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return err
		}
		l.file = nil
	}

	file, err := os.OpenFile(activePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}

	l.file = file
	l.activePath = activePath
	return nil
}

func (l *fileLogger) datedPath(now time.Time) string {
	dir := filepath.Dir(l.path)
	base := filepath.Base(l.path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", stem, now.Format("2006-01-02"), ext))
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
