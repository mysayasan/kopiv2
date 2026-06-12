# Module: domain/utils/middlewares/api_activity_log.go

## Purpose

Persists API activity into the database-backed `api_log` table.

## Behavior

- Runs on the `/api` router for auth and non-auth API requests.
- Records request start time on the wrapped response writer for shared response helpers.
- Captures response status, request URL, method, path, duration, user agent, client IP, and timestamp.
- Persists elapsed request handling time into `api_log.duration_ms`.
- Emits API telemetry observations with app, method, route-template path, status, and duration labels when a recorder is configured.
- Sets `createdBy` from a valid auth cookie when available.
- Uses `createdBy=0` for non-auth users, login requests before a session exists, or invalid/missing cookies.
- Persists best-effort after the handler completes with a short timeout.

## Notes

- This is separate from runtime JSON-lines logging.
- Protected-route auth and RBAC still run in their own middleware.
- Domain-specific audit entries can still be written by handlers when they need richer action details.
- The wrapped response writer lets controller responses include elapsed `durationMs`.
