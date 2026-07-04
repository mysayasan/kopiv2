# Module: infra/apphost/run.go

## Purpose

Implements the reusable runtime host for all app modules.

## Platform dispatch (`Run` / `runApp`)

`Run(app)` is now a thin platform dispatcher (`runWithPlatform`, in `service_windows.go`/`service_other.go`); the former body of `Run` is renamed `runApp` and holds all the shared runtime wiring described below.

- On Windows, when the process was launched by the Service Control Manager (`svc.IsWindowsService()` true), `runWithPlatform` runs `runApp` under `svc.Run` via a `windowsService` adapter (`service_windows.go`) instead of calling it directly, so `sc start`/`services.msc`/a Windows installer service registration can control it.
- On every other case (interactive Windows run, or any non-Windows OS via `service_other.go`), `runWithPlatform` calls `runApp` directly — identical to the previous behavior.
- `runApp`'s shutdown `select` gained a `case <-svcStop` (via `platformShutdownChan()`) alongside the existing OS-signal and restart-request cases: an SCM Stop/Shutdown request closes `windowsServiceStop`, which triggers the same graceful shutdown path as Ctrl+C/SIGTERM. `platformShutdownChan()` returns a nil channel on non-Windows and on an interactive Windows run, so that case is inert there.

## Responsibilities

- Resolve a home directory (`resolveHomeDir`: read-only app root — static assets, bundled scripts, default config) and a data directory (`resolveDataDir`: writable state root — mutable config, database, recordings, logs, keys), independently overridable via `<APP>_HOME`/`<APP>_DATA` or generic `KOPIV2_HOME`/`KOPIV2_DATA` env vars; a dev checkout has both equal to the app's `BaseDir()`, unchanged from before the split.
- Load selected app config from the data directory (`loadConfig`), seeding it from the home directory's shipped default `config.json` on first run when the data dir has none yet (a packaged install's writable copy).
- Apply secret and DB environment overrides.
- Apply cache environment overrides.
- Apply SSO environment overrides.
- Apply transaction lock environment overrides.
- Apply logging and API log cleanup environment overrides.
- Apply telemetry environment overrides.
- Apply server environment overrides for hostnames and explicit TLS/non-TLS ports.
- Normalize data-directory-relative paths (TLS, SSO CA bundle, file storage, logging, SQLite database files, the vision snapshot/recordings root, and the AI training data dir) against the data directory via `ResolveWritablePath`, which falls back to the pre-packaging legacy (CWD-relative) location when the data-dir target doesn't exist yet but the legacy path does — an upgrade-safety no-op for a dev checkout or an installed service (`WorkingDirectory=dataDir`).
- Generate a self-signed TLS keypair (`ensureSelfSignedCert`, `selfcert.go`) before starting listeners when any listener is TLS and the configured cert/key files don't already exist, so a fresh packaged install serves HTTPS immediately.
- Initialize the runtime logger before bootstrap and shared service wiring.
- Initialize the shared scheduler and expose it through app dependencies.
- Initialize the shared operation-job repository for durable file-storage uploads.
- Start the file-storage upload job worker when transaction job worker config is enabled.
- Start expired file cleanup when `fileStorage.cleanup.enabled` is true.
- Start scheduled runtime log cleanup when configured.
- Start scheduled API log cleanup when configured.
- Run shared bootstrap engine using app-provided entities and seeders.
- Wire global middleware and shared API modules.
- Honor app-provided shared API module selection when an app implements `SharedAPIConfigurator`.
- Create shared DTO service adapters from core shared services before mounting shared API modules.
- Wire API activity logging middleware on the `/api` router.
- Wire sliding-window rate-limit middleware on the `/api` router after API activity logging.
- Register shared cache-service admin API routes under `/api/cache-service`.
- Register shared app-registry admin API routes under `/api/app-registry`.
- Register shared API log API routes under `/api/log`.
- Register shared runtime log API routes under `/api/log-service`.
- Seed built-in accessrbac roles (`superadmin`, `viewer`) and enforce viewer least-privilege when `SharedAPIConfig.AccessRbac` is true, then mount the shared `/api/access-rbac` management surface protected by the accessrbac middleware. `EnsureViewerDefaults` is called **to strip** the legacy read-everything `GET /api` wildcard from existing deployments (viewer now starts with no permissions; an admin grants specific read paths via the matrix).
- Load the embedded version manifest and register the shared public version endpoint under `/api/version`.
- Initialize Prometheus telemetry when configured and mount the metrics endpoint.
- Mount shared operational route groups; identity apps such as `myidsan` register login/user routes from their own app package.
- Build and validate cache provider (`default`, `redis`, `inmemory`, or `memory`) from runtime config.
- Build and validate transaction lock provider (`redis`, `memory`, or `inmemory`) from runtime config.
- Register shared Swagger/OpenAPI routes for runtime API documentation.
- Invoke app-specific route registration.
- Invoke optional app-specific non-API web route registration before static asset fallback.
- Serve static SPA files from selected app directory.
- Build listener matrix from server hostnames and TLS/non-TLS port lists.
- Start one or more HTTP servers for the configured listener ports.
- Manage multi-listener lifecycle and graceful shutdown.
- Provide a `Restarter` (via `Dependencies.Restarter`) that app modules call to request a graceful restart; the run loop selects on its cancel, runs the normal shutdown, then relaunches a fresh process from the on-disk executable (`relaunchSelf`).
- Select DB adapter from `db.engine` with environment override support.

## Notes

- Shared modules are mounted once in the host; app modules only provide app-specific routes/workers.
- Apps can disable selected shared modules to keep resource-app API surfaces small; `mymatasan` disables all shared APIs except the version endpoint. Identity routes live only in `myidsan`. The shared accessrbac management surface (`/api/access-rbac`) is mounted by apphost for any app with `SharedAPIConfig.AccessRbac = true` (the default); the app must include `AccessRole`/`AccessRolePermission` entities and bind a user resolver.
- App modules can register app-specific periodic jobs through `deps.Scheduler`.
- App modules can register protected non-API routes by implementing `WebRouteRegistrar`; `myseliasan` uses this to guard `/` before serving the dashboard shell.
- OAuth providers remain optional; disabling Google/GitHub does not disable local credential auth routes.
- A social-login provider block whose `client_id` or `client_secret` (config file or `GOOGLE_CLIENT_SECRET` / `GITHUB_CLIENT_SECRET` env) is blank is **disabled with a warning** and set to `nil` instead of blocking startup. The identity service continues serving local login and any other configured providers. Previously, a missing client secret was a fatal boot error.
- Google and GitHub client secrets can be supplied from environment variables (`GOOGLE_CLIENT_SECRET`, `GITHUB_CLIENT_SECRET`) when their providers are configured.
- Swagger/OpenAPI docs are served from `/swagger` and `/swagger/openapi.json`.
- Readiness checks include DB and cache dependency checks.
- App worker shutdown is invoked before HTTP server shutdown when provided.
- `Restarter.Restart(reason)` cancels the run loop, runs the same graceful shutdown, then either exits for a process supervisor or self-relaunches. `supervisedRestart()` is true when `KOPIV2_SUPERVISED` is set OR the process runs as a Windows service (`platformSupervised()`); in that case it exits with `restartExitCode` (70) so the supervisor relaunches it (and a self-relaunch can't race/double-start a fresh service instance for the listen port). Otherwise `relaunchSelf()` restarts in place: Unix `execve`s the same image (same pid, no port hand-off race); Windows spawns a detached child with **`CREATE_NO_WINDOW`** — not `DETACHED_PROCESS`, which would make Windows allocate a *new console window* for this console-subsystem binary on every relaunch (a stray "DOS window" per restart). It is also the intended primitive for self-update.
- **Self-relaunch storm guard** (`allowSelfRelaunch`, all OS): the self-relaunch path is bounded to `selfRelaunchMax` (5) relaunches within `selfRelaunchWindow` (60s), counted across the relaunch chain via `KOPIV2_RESTART_GEN`/`KOPIV2_RESTART_T0` env (carried to the child, which inherits the environment). Once exceeded it logs and stays **down** instead of relaunching, so a crash-on-boot — or any condition that immediately re-requests a restart — can never become a runaway loop that spawns endless processes/windows. A window that elapses (the app stayed up a while) resets the count, so ordinary deliberate restarts are never throttled.
- Hostname wildcard (`*` or empty hostname) maps to bind-all interfaces.
- `server.tlsPorts` starts HTTPS listeners and `server.nonTlsPorts` starts HTTP listeners.
- Empty `tlsPorts` or `nonTlsPorts` means that protocol mode is not started.
- A port cannot appear in both `server.tlsPorts` and `server.nonTlsPorts`.
- HTTPS listeners require non-empty `tls.certPath` and `tls.keyPath`; normalized relative paths resolve from the data directory, and a missing cert/key pair is generated (self-signed) rather than failing startup.
- Legacy env compatibility is preserved for `SERVER_ADDR`, `SERVER_PORTS`, `SERVER_USE_TLS`, `SERVER_ENABLE_TLS`, and `SERVER_ENABLE_NON_TLS`.
- `DB_ENGINE` overrides `db.engine`; runtime DB adapters are available for `postgres`, `mariadb`, and `sqlite`.
- When `db.engine=sqlite`, `db.db_name` is treated as a file path and relative values resolve from the data directory (see `ResolveWritablePath`).
- `LOG_ENABLED`, `LOG_PATH`, and `LOG_MAX_LINE_BYTES` override runtime logging config.
- `LOG_CLEANUP_ENABLED`, `LOG_MAX_RETENTION_DAYS`, and `LOG_CLEANUP_FREQUENCY_MINUTES` override runtime log cleanup config.
- `API_LOG_CLEANUP_ENABLED`, `API_LOG_MAX_RETENTION_DAYS`, and `API_LOG_CLEANUP_FREQUENCY_MINUTES` override database-backed API log cleanup config.
- `TELEMETRY_ENABLED`, `PROMETHEUS_ENABLED`, `PROMETHEUS_METRICS_PATH`, and `PROMETHEUS_API_DURATION_THRESHOLD_MS` override telemetry config.
- `RATE_LIMIT_ENABLED` overrides API rate limiting.
- `SSO_ISSUER`, `SSO_AUDIENCE`, `SSO_SESSION_TTL_SECONDS`, `SSO_POLICY_CACHE_TTL_SECONDS`, `SSO_INTERNAL_TOKEN`, `SSO_PROVIDER_BASE_URL`, `SSO_CA_CERT_PATH`, `SSO_CLIENT_ID`, `SSO_CLIENT_SECRET`, `SSO_REDIRECT_BASE_URL`, `SSO_REDIRECT_PATH`, `SSO_AUTH_CODE_TTL_SECONDS`, and `SSO_ACCESS_TOKEN_TTL_SECONDS` override SSO config.
- The runtime logger writes JSON lines to stdout and the configured log file so OS-level collectors and the API listing endpoint can use the same log stream.
- Empty cache provider defaults to `inmemory`; `default` and `memory` are accepted aliases.
- Empty transaction lock provider inherits `cache.provider`; Redis is recommended for production multi-instance deployments.
- Transaction lock wait timeout, lease, operation timeout, and stuck timeout can be overridden by `TRANSACTION_LOCK_WAIT_TIMEOUT_MS`, `TRANSACTION_LOCK_LEASE_MS`, `TRANSACTION_OPERATION_TIMEOUT_MS`, and `TRANSACTION_STUCK_TIMEOUT_MS`.
- File-storage upload worker config can be overridden by `TRANSACTION_JOB_WORKER_ENABLED`, `TRANSACTION_JOB_WORKER_FREQUENCY_SECONDS`, and `TRANSACTION_MAX_ATTEMPTS`.
- The upload worker recovers stale running jobs before processing queued/retrying jobs and logs recovered/processed counts.
- The file-storage expiry cleanup scheduler uses `fileStorage.cleanup.frequencySeconds` and `fileStorage.cleanup.batchSize`, and logs only when files are deleted.
- `GET /api/version` is mounted without auth so clients can read app/core versions before login.
- The shared accessrbac core (`AccessSessionMidware`) is wired regardless of `SharedAPIConfig.AccessRbac`; its user resolver starts nil and the app binds it during `RegisterAppRoutes` via `deps.Access.SetResolver(...)`. If the resolver is unbound when a protected route is hit, the middleware fails closed with 403.
- `GET /metrics` is mounted when telemetry and Prometheus are enabled.
