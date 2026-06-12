# Module: infra/logging/logger.go

## Purpose

Provides cross-platform runtime logging for apphost and app services.

## Responsibilities

- Write structured JSON-lines logs to stdout and the configured file path.
- Derive dated daily log files from the configured base path (for example `mymatasan-2026-06-07.log` from `mymatasan.log`).
- Use `filepath` and `os` APIs so paths work on Linux, Windows, and macOS.
- Provide `Debugf`, `Infof`, `Warnf`, and `Errorf` helpers for service-level logging.
- Implement `io.Writer` so the standard library `log` package can be routed through the same logger.
- List persisted log entries with limit/offset pagination support.
- Delete batches of dated log files by year and month.
- Delete dated log files older than a retention cutoff for scheduled cleanup.

## Notes

- Log entries include timestamp, RFC3339 time, level, source, message, and OS.
- Disabled logging still allows stdout writes but returns an empty list from the API-facing reader.
- Oversized lines are truncated by `maxLineBytes`.
- The configured path is treated as a base name; deletion only removes files with the derived dated filename pattern.
