# Technical Specification

## Scope

`kopiv2` is a Linux-first, lightweight Go backend that provides:

- HTTP API with cookie-backed JWT auth and RBAC enforcement.
- Camera stream orchestration for MJPEG sources.
- SQL persistence using a generic repository layer.
- Static SPA serving from app assets.
- Public runtime version reporting with separate core and app SemVer values.
- A dedicated identity app (`myidsan`) for user management, RBAC administration, and planned SSO authority.

The runtime now uses a reusable multi-app launcher pattern:

- root launcher: `main.go` with `-app <name>`
- shared startup/runtime host: `infra/apphost`
- per-app compile target: `cmd/<app>/main.go`

## Runtime Targets

- Primary: Linux hosts (including low-resource deployments).
- Container runtime: Docker and Docker Compose.
- Service runtime: systemd unit supported in `deploy/linux`.

## Runtime Characteristics

- Go version: `1.26.4`.
- App selection:
  - runtime selection via `go run . -app <name>`
  - compile selection via `go build ./cmd/<name>`
  - currently registered apps: `mymatasan`, `myidsan`, `myseliasan`
- HTTP server defaults:
  - Read header timeout: `5s`
  - Read timeout: `15s`
  - Write timeout: `30s`
  - Idle timeout: `60s`
- Graceful shutdown on `SIGINT` and `SIGTERM`.
- Version manifest: embedded from `infra/versioning/version.json` at build time.

## Security Model

- Authentication: JWT session stored in an HttpOnly cookie.
- CSRF protection: unsafe authenticated methods (`POST`, `PUT`, `PATCH`, `DELETE`) require `X-CSRF-Token` matching the readable CSRF cookie.
- Local credential auth endpoints are available under `/api/login/default` (login) and `/api/login/default/register` (register) in `myidsan`; relying/resource apps do not mount login APIs.
- OAuth login providers (Google/GitHub) are optional and do not disable local credential auth when not configured.
- Local credential passwords are stored as bcrypt hashes, with lazy migration from legacy plain-text values on successful login.
- Authorization: endpoint-level RBAC based on user role mappings.
- `myidsan` is the central policy authority for cross-app SSO. Tokens carry issuer (`iss`), audience (`aud`), expiry, session id (`sid`), resource app code, and policy version. Redis should be used for shared session/RBAC cache in multi-process deployments; in-memory cache requires relying apps to call `myidsan` for introspection and authorization decisions when local cache misses.
- `mymatasan` is a relying/resource app and does not mount login, user/group/role, app-registry, endpoint, or endpoint-RBAC management APIs. Those management surfaces live in `myidsan`.
- `myseliasan` is a relying control-plane app and has no public landing page. It redirects unauthenticated users to MyIDSan and creates its own local session only after MyIDSan returns a valid authorization code.
- Registered apps are stored in `app_registry`; endpoint policies are scoped by `api_endpoint.appCode`.
- Per-client SSO policy is stored in `app_auth_config`, and exact callback allow-list entries are stored in `app_redirect_uri`.
- Browser cross-app login uses MyIDSan `GET /api/auth/authorize`, `GET|POST /api/auth/login`, and `POST /api/auth/token`.
- `POST /api/sso/introspect` validates token/session state for service-to-service fallback.
- `POST /api/sso/authorize` validates token/session state and returns an app-scoped RBAC decision for service-to-service fallback.
- API endpoint metadata includes `accessTier` (`0=DevOnly`, `1=AuthOnly`, `2=Public`) for route classification. The tier does not replace auth/RBAC; `DevOnly` endpoints still require authorization when registered behind protected handlers.
- Browser-readable MyIDSan UI cookies are limited to presentation state such as the active page and table filters, sorters, and page position. They are not authentication or authorization material; identity remains in the HttpOnly JWT cookie and server-side RBAC checks.
- API rate limiting uses a sliding-window counter per endpoint access tier. Redis-backed cache shares counters across instances; in-memory cache is process-local.
- Secrets:
  - `JWT_SECRET` required.
  - `GOOGLE_CLIENT_SECRET` required when Google login is enabled in config.
  - `GITHUB_CLIENT_SECRET` required when GitHub login is enabled in config.
- OAuth redirect state is generated per login request and validated against an HTTP-only state cookie on callback.

## Cache Model

- Cache abstraction is runtime-selected via configuration (`default`, `redis`, `inmemory`, or `memory`).
- Primary shared cache backend for multi-instance deployments: Redis.
- `default`, `inmemory`, and `memory` all select the local in-process memory cache.
- SSO sessions are cached as `sso:session:<sid>`.
- RBAC role access lists are cached by resource app, role key, and policy version, then invalidated on endpoint or endpoint-RBAC create/update/delete.
- Readiness includes cache ping to ensure runtime dependencies are available.
- Shared admin cache API is exposed under `/api/cache-service` for key listing and controlled wipe operations.
- API activity is persisted into `api_log` for both authenticated and non-authenticated `/api` requests, including elapsed `durationMs`.
- Successful cache wipe operations are persisted into API logs for operational audit trail.
- Shared API log listing and monthly database row deletion are exposed under `/api/log` for authenticated/RBAC-protected operators.
- Runtime service logs are written as JSON lines to stdout and dated cross-platform log files derived from the configured base path.
- Shared runtime log listing and monthly log-file deletion are exposed under `/api/log-service` for authenticated/RBAC-protected operators.
- Shared telemetry can expose Prometheus-format metrics at the configured metrics path.
- API telemetry records request counts, duration histograms, and slow request counts using a configurable duration threshold.

## Transaction Coordination

- Critical multi-step operations use an application-level FIFO lock before executing the DB/filesystem unit of work.
- Production multi-instance deployments should use the Redis lock provider.
- In-memory locking is available only for single-process development or tests.
- Redis locks use owner tokens and renewable leases so stale owners cannot release another request's lock.
- Wait timeout removes an abandoned waiter from the FIFO queue.
- Stuck timeout emits telemetry when a lock is held longer than expected.
- DB consistency still uses request-scoped `database/sql` transactions; the coordinator serializes access and prevents request races.
- File-storage uploads are staged first, then metadata insert and final file copy run under the same coordinated transaction workflow with compensation cleanup on failure.
- Synchronous upload keeps the existing request/response contract for development and simple callers.
- Async upload creates an `operation_job` row with idempotency key, payload, retry counters, status, deadline, result, and error state.
- The backend worker recovers stale `running` upload jobs, requeues retryable work, and fails/cleans up exhausted jobs.
- The async worker still uses the same FIFO coordinator and request-scoped DB transaction when executing each upload job.
- File metadata carries `securityLvl` and absolute `expiredAt`; upload endpoints can convert countdown expiry fields into `expiredAt` before entering the service.
- Download authorization is enforced in the file-storage service before reading the physical GUID path.
- File expiry is enforced immediately on download and by a scheduler that sweeps expired physical files plus metadata in bounded batches.

## Data and Persistence

- Databases: PostgreSQL, MariaDB, and SQLite.
- Readiness check performs DB ping through `IDbCrud.Ping(ctx)`.
- Repository layer wraps DB errors with `%w` context for diagnostics.
- Startup bootstrap uses entity reflection to create missing database objects and store a schema manifest hash.
- Safe schema updates are additive only by default.
- Optional initial data can be supplied through config-driven SQL seed statements when bootstrap seeding is enabled.
- The app also seeds a minimal core identity dataset (`system` group, `superadmin` role, and first-run `superadmin` login account with bcrypt password storage) during bootstrap.
- The app seeds wildcard-host endpoint rows with access tiers and RBAC rows for protected API modules so first-run permissions work without binding to a specific host name. Protected shared management APIs seed as `DevOnly`.

## Configuration Contract

Config source:

- `ENVIRONMENT=dev` -> `apps/<selected-app>/config.dev.json`
- otherwise -> `apps/<selected-app>/config.json`

Environment overrides (runtime):

- server: `SERVER_HOSTNAMES`, `SERVER_TLS_PORTS`, `SERVER_NON_TLS_PORTS`
- legacy server compatibility: `SERVER_ADDR`, `SERVER_PORTS`, `SERVER_USE_TLS`, `SERVER_ENABLE_TLS`, `SERVER_ENABLE_NON_TLS`
- db: `DB_ENGINE`, `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSL_MODE`
- cache: `CACHE_PROVIDER`, `CACHE_TTL_SECONDS`, `CACHE_KEY_PREFIX`, `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB`, `REDIS_USE_TLS`, `REDIS_CONNECT_TIMEOUT_MS`, `REDIS_OPERATION_TIMEOUT_MS`
- sso: `SSO_ISSUER`, `SSO_AUDIENCE`, `SSO_SESSION_TTL_SECONDS`, `SSO_POLICY_CACHE_TTL_SECONDS`, `SSO_INTERNAL_TOKEN`, `SSO_PROVIDER_BASE_URL`, `SSO_CA_CERT_PATH`, `SSO_CLIENT_ID`, `SSO_CLIENT_SECRET`, `SSO_REDIRECT_BASE_URL`, `SSO_REDIRECT_PATH`, `SSO_AUTH_CODE_TTL_SECONDS`, `SSO_ACCESS_TOKEN_TTL_SECONDS`
- rate limit: `RATE_LIMIT_ENABLED`
- transaction: `TRANSACTION_LOCK_PROVIDER`, `TRANSACTION_LOCK_WAIT_TIMEOUT_MS`, `TRANSACTION_LOCK_LEASE_MS`, `TRANSACTION_OPERATION_TIMEOUT_MS`, `TRANSACTION_STUCK_TIMEOUT_MS`, `TRANSACTION_JOB_WORKER_ENABLED`, `TRANSACTION_JOB_WORKER_FREQUENCY_SECONDS`, `TRANSACTION_MAX_ATTEMPTS`
- secrets: `JWT_SECRET`, `GOOGLE_CLIENT_SECRET`, `GITHUB_CLIENT_SECRET`
- logging: `LOG_ENABLED`, `LOG_PATH`, `LOG_MAX_LINE_BYTES`, `LOG_CLEANUP_ENABLED`, `LOG_MAX_RETENTION_DAYS`, `LOG_CLEANUP_FREQUENCY_MINUTES`
- api log cleanup: `API_LOG_CLEANUP_ENABLED`, `API_LOG_MAX_RETENTION_DAYS`, `API_LOG_CLEANUP_FREQUENCY_MINUTES`
- telemetry: `TELEMETRY_ENABLED`, `PROMETHEUS_ENABLED`, `PROMETHEUS_METRICS_PATH`, `PROMETHEUS_API_DURATION_THRESHOLD_MS`

Server config contract (`server` in app config):

- `hostnames`: host or IP list. Empty or `*` means wildcard bind across NICs.
- `tlsPorts`: HTTPS listener ports. Empty means no HTTPS listener.
- `nonTlsPorts`: HTTP listener ports. Empty means no HTTP listener.
- `ports`, `enableTls`, `enableNonTls`: legacy shared-port mode used only when explicit TLS/non-TLS port lists are empty.

TLS config contract (`tls` in app config):

- `certPath`: certificate path used when any HTTPS listener is enabled.
- `keyPath`: private-key path used when any HTTPS listener is enabled.
- Relative TLS paths resolve from the selected app directory.

Database config contract (`db` in app config):

- `engine`: DB engine selector (`postgres`, `mariadb`, or `sqlite`).
- Runtime DB adapter and bootstrap implementation support all three engines.
- For SQLite, `db_name` is a database file path and relative paths resolve from the selected app directory. `:memory:` is supported for tests/dev experiments only.
- SQLite is intended for single-process small-device deployments; use PostgreSQL or MariaDB for multi-instance production deployments.
- `apps/mymatasan/config.dev.json` defaults PostgreSQL to port `5433`; runtime `DB_PORT` overrides the config value for local or deployed environments.

SSO relying-app config contract (`sso` in app config):

- `providerBaseUrl`: MyIDSan public base URL used by browser redirects and server-side token exchange.
- `caCertPath`: optional PEM CA/certificate bundle for relying-app backend HTTPS calls to MyIDSan; relative paths resolve from the selected app directory.
- `caCertPath` appends trust roots for the token-exchange HTTP client; it does not disable TLS verification. Hostname, expiry, and certificate-chain validation still apply.
- `clientId`: relying app client id registered in MyIDSan `app_auth_config`.
- `clientSecret`: relying app secret; override with `SSO_CLIENT_SECRET` outside local development.
- `redirectBaseUrl`: relying app public base URL used to build the callback URL sent to MyIDSan. It must match a registered `app_redirect_uri` origin.
- `redirectPath`: relying app callback path, default `/api/auth/callback`.
- `authCodeTtlSeconds`: default MyIDSan authorization-code lifetime when a per-client row does not override it.
- `accessTokenTtlSeconds`: default MyIDSan issued-token lifetime when a per-client row does not override it.

Logging config contract (`logging` in app config):

- `enabled`: writes runtime log entries to the configured file when true.
- `path`: log base path. Relative paths are resolved from the selected app directory and dated daily files are derived from this name.
- `maxLineBytes`: maximum size retained for one listed log message.
- `cleanup.enabled`: starts the runtime log cleanup scheduler when true.
- `cleanup.maxRetentionDays`: scheduled cleanup deletes dated files older than this many days.
- `cleanup.frequencyMinutes`: scheduler check interval. Defaults to `60` minutes when omitted or invalid.
- Manual month deletion rejects the current month at service level.

API log config contract (`apiLog` in app config):

- `cleanup.enabled`: starts database-backed API log retention cleanup when true.
- `cleanup.maxRetentionDays`: scheduled cleanup deletes `api_log` rows older than this many days.
- `cleanup.frequencyMinutes`: scheduler check interval. Defaults to `60` minutes when omitted or invalid.
- Manual month deletion rejects the current month at service level.

Telemetry config contract (`telemetry` in app config):

- `enabled`: enables shared telemetry wiring.
- `prometheus.enabled`: enables the Prometheus text exporter.
- `prometheus.metricsPath`: route mounted by apphost for metric scrapes.
- `prometheus.apiDurationThresholdMs`: request duration threshold used by slow API metrics.

Rate limit config contract (`rateLimit` in app config):

- `enabled`: enables sliding-window API rate limiting.
- `endpointCacheTtlSeconds`: caches endpoint tier metadata to avoid DB reads on every request.
- `defaultWindowSeconds`: fallback window for tiers that omit `windowSeconds`.
- `devOnly`, `authOnly`, `public`: per-tier `enabled`, `requests`, and `windowSeconds`.

Transaction config contract (`transaction` in app config):

- `lockProvider`: transaction lock backend (`redis`, `memory`, or `inmemory`); empty inherits `cache.provider`.
- `lockWaitTimeoutMs`: maximum FIFO wait before cancellation.
- `lockLeaseMs`: Redis owner lease duration; active owners renew before expiry.
- `operationTimeoutMs`: maximum coordinated operation duration.
- `stuckTimeoutMs`: lock hold duration that emits stuck telemetry.
- `jobWorkerEnabled`: enables the backend file-storage upload worker.
- `jobWorkerFrequencySeconds`: worker polling interval for stale recovery and queued/retrying jobs; defaults to 5 seconds when omitted or invalid.
- `maxAttempts`: maximum upload job attempts before terminal failure cleanup; defaults to 3 when omitted or invalid.

File storage config contract (`fileStorage` in app config):

- `path`: base directory for staged and committed file objects.
- `cleanup.enabled`: starts the expired file cleanup scheduler when true.
- `cleanup.frequencySeconds`: scheduler check interval; defaults to 60 seconds when omitted or invalid.
- `cleanup.batchSize`: maximum expired file rows removed per scheduler run; defaults to 100 when omitted or invalid.

At least one explicit TLS or non-TLS port must be configured. The same port cannot be assigned to both `tlsPorts` and `nonTlsPorts`. Legacy shared-port mode still rejects simultaneous TLS and non-TLS because HTTP and HTTPS cannot bind the same address simultaneously. HTTPS listeners require non-empty certificate and key paths.

## Health Contracts

- `GET /health`: liveness.
- `GET /ready`: readiness including DB and cache connectivity.
- `GET /api/health`: API namespace status.
- `GET /api/version`: public runtime version for the selected app and shared core.
- `GET /metrics`: Prometheus metrics endpoint when telemetry is enabled.

## Versioning Model

- Core version and app version are stored separately as standard `major.minor.patch` SemVer values.
- Core version covers reusable/shared code such as `infra`, `domain`, and shared API/service modules.
- App version covers the selected app module, such as `apps/mymatasan` or `apps/myidsan`.
- The server loads an embedded manifest from `infra/versioning/version.json`.
- The runtime endpoint returns only the selected app version plus the core version; it does not expose the full app version map.
- GitHub Actions consumes pending JSON changelog entries from `changes/pending/.../change.json`, bumps the manifest, and moves processed entries to `changes/applied`.
- Pending changelog entries support the legacy `level/scope/app` shape and a multi-target `type/scope` shape. Multi-target scopes are comma-separated and can include core aliases plus app names from the manifest.

## API Documentation Contract

- `GET /swagger`: Swagger UI.
- `GET /swagger/openapi.json`: OpenAPI 3.0 document.
- Endpoint list is generated from runtime route registration, so shared and app-local APIs are documented from one source.
- Key endpoints include reusable request/response schema components (`components.schemas`) for FE integration and code generation.
- Key list/create/update endpoints are mapped to endpoint-specific response wrappers (typed `result` payloads) instead of only generic default/paging contracts.
- Shared DB-backed list endpoints expose `limit`, `offset`, and optional `filters`/`sorters` query parameters so paging can be filtered and ordered in the backend before the response is returned. `filters` and `sorters` accept JSON object or array values, with repeated `filter` and `sorter` query parameters also supported. Multiple filters are combined with `AND`; multiple sorters keep the request order.
- Non-JSON endpoints are explicitly modeled with route-accurate status/content (for example OAuth redirect `302` and MJPEG stream `206 multipart/x-mixed-replace`).
- Cache admin endpoints are documented with `cache-service` tag (`GET /api/cache-service`, `GET /api/cache-service/health`, `DELETE /api/cache-service`, `POST /api/cache-service/wipe`).
- API log endpoints are documented with `log` tag (`GET /api/log`, `DELETE /api/log`).
- Runtime log endpoints are documented with `log-service` tag (`GET /api/log-service`, `DELETE /api/log-service`).
- Runtime version endpoint is documented with `system` tag (`GET /api/version`).
- File-storage sync upload, async upload, download, inline view, and job status endpoints are documented with `file-storage` tag.
- Shared JSON response wrappers include top-level `durationMs` for elapsed request handling time in milliseconds.
- Prometheus telemetry includes transaction lock event, wait-duration, and stuck-lock metrics using low-cardinality labels.
- App modules can provide richer endpoint summaries/descriptions by implementing the shared API docs provider contract.

## Non-Goals

- Not a monolithic framework generator.
- Not optimized for distributed stream processing across nodes.
- Not a schema migration framework.
