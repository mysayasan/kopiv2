# Module: domain/shared/services/runtime_log.go

## Purpose

Wraps the infrastructure logger behind a shared domain service.

## Responsibilities

- Provide `IRuntimeLogService`.
- Return paginated runtime log entries from the configured logger.
- Delete dated log files by year and month.
- Reject deletion of current-month logs before calling the logger.
- Delete logs older than the configured retention window for scheduled cleanup.
- Return an empty list when no logger is configured.

## Notes

- Keeping the API behind a service preserves the shared API/service pattern used by cache and audit-log modules.
