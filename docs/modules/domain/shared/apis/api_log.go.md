# Module: domain/shared/apis/api_log.go

## Purpose

Exposes database-backed API activity logs for authenticated administrators.

## Responsibilities

- Mount `GET /api/log`.
- Mount `DELETE /api/log`.
- Require auth middleware and RBAC validation.
- Read `limit` and `offset` query parameters for listing.
- Read `year` and `month` query parameters for monthly deletion.
- Return forbidden when callers try to delete current-month API logs.

## Notes

- This endpoint lists request activity stored in the `api_log` table, not runtime service log files from `/api/log-service`.
- Listed rows include `durationMs`, the elapsed request handling time in milliseconds.
- Delete requests are unsafe cookie-authenticated calls and therefore require `X-CSRF-Token`.
