# Module: infra/config/config_models.go

## Purpose

Defines the top-level app configuration model loaded from app config JSON.

## Responsibilities

- Model optional OAuth provider configuration for Google and GitHub.
- Model server listener hostnames and explicit TLS/non-TLS ports.
- Model bootstrap, JWT, SSO, file storage, cache, rate limiting, transaction coordination, logging, API log cleanup, telemetry, TLS, and DB settings.

## Notes

- `login.google` and `login.github` are independently optional.
- Apphost requires each configured OAuth provider to have its matching client secret before startup continues.
- `server.tlsPorts` and `server.nonTlsPorts` are the preferred listener config fields.
- `tls.certPath` and `tls.keyPath` are required when HTTPS listeners are enabled; relative paths are app-relative.
- Legacy `server.ports`, `server.enableTls`, and `server.enableNonTls` remain available only as a fallback when explicit port lists are empty.
- `logging.path` is app-relative unless absolute, and is resolved with Go `filepath` for Windows, Linux, and macOS.
- `fileStorage.path` is app-relative unless absolute.
- `fileStorage.cleanup.enabled` starts the expired file cleanup scheduler.
- `fileStorage.cleanup.frequencySeconds` controls scheduler check frequency and defaults to 60 seconds in apphost.
- `fileStorage.cleanup.batchSize` controls the maximum expired rows removed per scheduler run and defaults to 100 in apphost.
- `logging.path` is used as the base filename for dated daily log files.
- `logging.maxLineBytes` bounds each listed log line to avoid oversized API responses.
- `logging.cleanup.enabled` starts the runtime log cleanup scheduler.
- `logging.cleanup.maxRetentionDays` controls the retention cutoff.
- `logging.cleanup.frequencyMinutes` controls scheduler check frequency and defaults to 60 minutes in apphost.
- `apiLog.cleanup.enabled` starts database-backed API log retention cleanup.
- `apiLog.cleanup.maxRetentionDays` controls the API log row retention cutoff.
- `apiLog.cleanup.frequencyMinutes` controls API log cleanup frequency and defaults to 60 minutes in apphost.
- `telemetry.enabled` enables shared telemetry wiring.
- `telemetry.prometheus.enabled` exposes Prometheus-format metrics.
- `telemetry.prometheus.metricsPath` controls the metrics scrape route.
- `telemetry.prometheus.apiDurationThresholdMs` controls slow API request metrics.
- `rateLimit.enabled` enables API sliding-window rate limiting.
- `rateLimit.devOnly`, `rateLimit.authOnly`, and `rateLimit.public` configure per-tier request counts and windows.
- `sso.issuer` configures the expected/issued JWT issuer.
- `sso.audience` configures comma-separated accepted JWT audiences.
- `sso.sessionTtlSeconds` controls cookie/session-cache lifetime.
- `sso.policyCacheTtlSeconds` controls RBAC policy cache lifetime.
- `sso.internalToken` protects myidsan service-to-service introspection and authorization APIs.
- `sso.providerBaseUrl` points relying apps to MyIDSan for authorization-code login.
- `sso.caCertPath` optionally points to a PEM CA/certificate bundle used by relying-app backend HTTPS calls to MyIDSan.
- `sso.clientId` and `sso.clientSecret` configure relying-app token exchange credentials.
- `sso.redirectBaseUrl` configures the relying-app public callback origin used in authorization requests.
- `sso.redirectPath` configures the relying-app callback path.
- `sso.authCodeTtlSeconds` and `sso.accessTokenTtlSeconds` provide MyIDSan defaults when per-client DB config does not override them.
- `transaction.lockProvider` selects Redis or in-memory FIFO transaction locking; empty inherits `cache.provider`.
- `transaction.lockWaitTimeoutMs` bounds queue wait time.
- `transaction.lockLeaseMs` controls Redis owner lease duration.
- `transaction.operationTimeoutMs` bounds coordinated file-storage transaction work.
- `transaction.stuckTimeoutMs` emits telemetry when a lock is held too long.
- `transaction.jobWorkerEnabled` starts the durable file-storage upload worker.
- `transaction.jobWorkerFrequencySeconds` controls worker polling frequency.
- `transaction.maxAttempts` caps retry attempts before a durable upload job fails and cleans up.
