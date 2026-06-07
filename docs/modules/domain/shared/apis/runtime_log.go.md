# Module: domain/shared/apis/runtime_log.go

## Purpose

Exposes runtime log listing for authenticated administrators.

## Responsibilities

- Mount `GET /api/log-service`.
- Mount `DELETE /api/log-service`.
- Require auth middleware and RBAC validation.
- Read `limit` and `offset` query parameters.
- Return paginated runtime log entries from the configured logging module.
- Delete dated runtime log files by `year` and `month` query parameters.
- Return forbidden when callers try to delete the current month.

## Notes

- This endpoint lists operational/service logs, not database-backed audit logs from `/api/log`.
- Delete requests are unsafe cookie-authenticated calls and therefore require `X-CSRF-Token`.
