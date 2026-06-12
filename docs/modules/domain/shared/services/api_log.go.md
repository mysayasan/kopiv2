# Module: domain/shared/services/api_log.go

## Purpose

Wraps the database-backed API log repository behind a shared domain service.

## Responsibilities

- Provide `IApiLogService`.
- Return paginated API activity log rows with caller-provided filters and sorters.
- Use newest-first ordering when callers do not provide sorters.
- Create API log rows for middleware and audit callers.
- Persist `durationMs` when callers provide request timing metadata.
- Delete API logs by calendar month.
- Reject current-month deletion before calling the repository.
- Delete API logs older than the configured retention window for scheduled cleanup.

## Notes

- Current-month protection is service-level, so HTTP endpoints and future callers cannot bypass it.
- Retention cleanup deletes rows by `CreatedAt` cutoff using filtered repository delete.
