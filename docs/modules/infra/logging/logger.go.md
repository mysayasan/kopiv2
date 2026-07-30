# Module: infra/logging/logger.go

## Purpose

Provides cross-platform runtime logging for apphost and app services.

## Responsibilities

- Write structured JSON-lines logs to stdout and the configured file path.
- Derive dated daily log files from the configured base path (for example `mymatasan-2026-06-07.log` from `mymatasan.log`).
- Cap the active file's size (`Config.MaxFileSizeMb`, 0/negative = uncapped, the historic behaviour) and roll to a sequenced sibling — `mymatasan-2026-06-07.1.log`, `.2.log`, ... — when the cap is hit, without changing the sequence-0 filename.
- Use `filepath` and `os` APIs so paths work on Linux, Windows, and macOS.
- Provide `Debugf`, `Infof`, `Warnf`, and `Errorf` helpers for service-level logging.
- Implement `io.Writer` so the standard library `log` package can be routed through the same logger.
- List persisted log entries with limit/offset pagination support.
- Delete batches of dated log files by year and month.
- Delete dated log files older than a retention cutoff for scheduled cleanup, including size-rotated siblings.

## Notes

- Log entries include timestamp, RFC3339 time, level, source, message, and OS.
- Disabled logging still allows stdout writes but returns an empty list from the API-facing reader.
- Oversized lines are truncated by `maxLineBytes`.
- The configured path is treated as a base name; deletion only removes files with the derived dated filename pattern (both the plain `<stem>-<date>.log` and size-rotated `<stem>-<date>.<seq>.log` siblings).
- `List` orders newest-first by reversing append (read) order and then stable-sorting on the integer `Timestamp` alone — never by comparing the RFC3339 `Time` string, which is unreliable on a timestamp tie (Go trims trailing fraction zeros, so `.1Z` can string-compare greater than `.1000001Z`; and on Windows the clock is coarse enough that adjacent writes get a byte-identical `Time`). Read order needs no clock and cannot tie.
- **Size cap (`MaxFileSizeMb`).** `ensureActiveFileLocked` starts a fresh sequence-0 file on every calendar-day change regardless of the cap; within a day, once `activeSize` (tracked from each `Fprintln`'s byte count, not stat'd per line) reaches the cap it opens the next sequence via `openLocked`. On first open of a day with a cap configured, `highestSeqForDay` resumes at the highest *unfilled* sequence on disk (rather than reopening `<stem>-<date>.log`), so a restart never blows past the cap by continuing to append to an already-full file. A single line is never split, so a file may exceed the cap by at most one line.
- **Retention must recognise every rotated shape, or a file becomes undeletable.** `logFiles()` globs both the plain dated shape and the sequenced `<stem>-????-??-??.*<ext>` shape; `dateValueFromPath()` is the strict gate — it strips a trailing suffix from the date component only when that suffix is entirely digits (so `<stem>-<date>.bak.log` still fails to parse and is left alone), then parses the remaining `YYYY-MM-DD`. Both must change together — see `logger_size_test.go`'s `TestSizeRotatedFilesAreStillPruned`, which is mutation-tested: reverting the parser change alone makes it delete 1 of 3 seeded files instead of 3.
