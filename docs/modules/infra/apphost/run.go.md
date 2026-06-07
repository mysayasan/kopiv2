# Module: infra/apphost/run.go

## Purpose

Implements the reusable runtime host for all app modules.

## Responsibilities

- Load selected app config from app base directory.
- Apply secret and DB environment overrides.
- Apply cache environment overrides.
- Apply transaction lock environment overrides.
- Apply logging and API log cleanup environment overrides.
- Apply telemetry environment overrides.
- Apply server environment overrides for hostnames and explicit TLS/non-TLS ports.
- Normalize app-relative paths (TLS, file storage, and logging).
- Initialize the runtime logger before bootstrap and shared service wiring.
- Initialize the shared scheduler and expose it through app dependencies.
- Initialize the shared operation-job repository for durable file-storage uploads.
- Start the file-storage upload job worker when transaction job worker config is enabled.
- Start expired file cleanup when `fileStorage.cleanup.enabled` is true.
- Start scheduled runtime log cleanup when configured.
- Start scheduled API log cleanup when configured.
- Run shared bootstrap engine using app-provided entities and seeders.
- Wire global middleware and shared API modules.
- Wire API activity logging middleware on the `/api` router.
- Register shared cache-service admin API routes under `/api/cache-service`.
- Register shared API log API routes under `/api/log`.
- Register shared runtime log API routes under `/api/log-service`.
- Load the embedded version manifest and register the shared public version endpoint under `/api/version`.
- Initialize Prometheus telemetry when configured and mount the metrics endpoint.
- Register local credential auth routes (`/api/login/default`, `/api/login/default/register`) regardless of OAuth provider configuration.
- Build and validate cache provider (`default`, `redis`, `inmemory`, or `memory`) from runtime config.
- Build and validate transaction lock provider (`redis`, `memory`, or `inmemory`) from runtime config.
- Register shared Swagger/OpenAPI routes for runtime API documentation.
- Invoke app-specific route registration.
- Serve static SPA files from selected app directory.
- Build listener matrix from server hostnames and TLS/non-TLS port lists.
- Start one or more HTTP servers for the configured listener ports.
- Manage multi-listener lifecycle and graceful shutdown.
- Select DB adapter from `db.engine` with environment override support.

## Notes

- Shared modules are mounted once in the host; app modules only provide app-specific routes/workers.
- App modules can register app-specific periodic jobs through `deps.Scheduler`.
- OAuth providers remain optional; disabling Google/GitHub does not disable local credential auth routes.
- Google and GitHub client secrets can be supplied from environment variables when their providers are configured.
- Swagger/OpenAPI docs are served from `/swagger` and `/swagger/openapi.json`.
- Readiness checks include DB and cache dependency checks.
- App worker shutdown is invoked before HTTP server shutdown when provided.
- Hostname wildcard (`*` or empty hostname) maps to bind-all interfaces.
- `server.tlsPorts` starts HTTPS listeners and `server.nonTlsPorts` starts HTTP listeners.
- Empty `tlsPorts` or `nonTlsPorts` means that protocol mode is not started.
- A port cannot appear in both `server.tlsPorts` and `server.nonTlsPorts`.
- Legacy env compatibility is preserved for `SERVER_ADDR`, `SERVER_PORTS`, `SERVER_USE_TLS`, `SERVER_ENABLE_TLS`, and `SERVER_ENABLE_NON_TLS`.
- `DB_ENGINE` overrides `db.engine`; runtime DB adapters are available for both `postgres` and `mariadb`.
- `LOG_ENABLED`, `LOG_PATH`, and `LOG_MAX_LINE_BYTES` override runtime logging config.
- `LOG_CLEANUP_ENABLED`, `LOG_MAX_RETENTION_DAYS`, and `LOG_CLEANUP_FREQUENCY_MINUTES` override runtime log cleanup config.
- `API_LOG_CLEANUP_ENABLED`, `API_LOG_MAX_RETENTION_DAYS`, and `API_LOG_CLEANUP_FREQUENCY_MINUTES` override database-backed API log cleanup config.
- `TELEMETRY_ENABLED`, `PROMETHEUS_ENABLED`, `PROMETHEUS_METRICS_PATH`, and `PROMETHEUS_API_DURATION_THRESHOLD_MS` override telemetry config.
- The runtime logger writes JSON lines to stdout and the configured log file so OS-level collectors and the API listing endpoint can use the same log stream.
- Empty cache provider defaults to `inmemory`; `default` and `memory` are accepted aliases.
- Empty transaction lock provider inherits `cache.provider`; Redis is recommended for production multi-instance deployments.
- Transaction lock wait timeout, lease, operation timeout, and stuck timeout can be overridden by `TRANSACTION_LOCK_WAIT_TIMEOUT_MS`, `TRANSACTION_LOCK_LEASE_MS`, `TRANSACTION_OPERATION_TIMEOUT_MS`, and `TRANSACTION_STUCK_TIMEOUT_MS`.
- File-storage upload worker config can be overridden by `TRANSACTION_JOB_WORKER_ENABLED`, `TRANSACTION_JOB_WORKER_FREQUENCY_SECONDS`, and `TRANSACTION_MAX_ATTEMPTS`.
- The upload worker recovers stale running jobs before processing queued/retrying jobs and logs recovered/processed counts.
- The file-storage expiry cleanup scheduler uses `fileStorage.cleanup.frequencySeconds` and `fileStorage.cleanup.batchSize`, and logs only when files are deleted.
- `GET /api/version` is mounted without auth/RBAC so clients can read app/core versions before login.
- `GET /metrics` is mounted when telemetry and Prometheus are enabled.
