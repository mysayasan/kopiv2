# kopiv2

Lightweight Go backend for Linux-first deployments, including small devices, with JWT auth, RBAC, camera streaming, and selectable SQL persistence.

## Table of Contents

- [Quickstart](#quickstart)
- [Overview](#overview)
- [Architecture](#architecture)
- [Documentation](#documentation)
- [Project Structure](#project-structure)
- [Requirements](#requirements)
- [Configuration](#configuration)
- [Makefile Commands](#makefile-commands)
- [Run Locally](#run-locally)
- [Run with Docker](#run-with-docker)
- [Run with Docker Compose](#run-with-docker-compose)
- [Linux systemd Deployment](#linux-systemd-deployment)
- [API Health Endpoints](#api-health-endpoints)
- [Versioning](#versioning)
- [Telemetry](#telemetry)
- [API Documentation (Swagger)](#api-documentation-swagger)
- [Testing](#testing)
- [Production Checklist](#production-checklist)
- [Operational Hardening Included](#operational-hardening-included)
- [Troubleshooting](#troubleshooting)

## Quickstart

Fastest way to run app + PostgreSQL locally:

```bash
make up
curl http://localhost:3000/health
curl http://localhost:3000/ready
```

To stop:

```bash
make down
```

## Overview

This repository contains modular backend applications:

- `apps/mymatasan`: standalone camera, ONVIF discovery, WebRTC live stream, and video intelligence app for small devices.
- `apps/myidsan`: identity, user management, and RBAC administration app.
- `apps/myseliasan`: relying control-plane app for `mymatasan`, authenticated through `myidsan`.

Primary goals:

- Linux compatibility.
- Lightweight runtime footprint.
- Stable service behavior (timeouts, graceful shutdown, readiness checks).
- Secure API path with authentication and authorization controls.

## Architecture

High-level flow:

1. Incoming HTTP requests are handled by Gorilla Mux.
2. Global middleware applies CORS, request logging, and request ID tracing.
3. API routes under `/api` persist activity and elapsed duration into `api_log`, apply access-tier rate limiting, then protected routes pass through JWT authentication and RBAC authorization.
4. A shared bootstrap engine checks/creates the database and syncs schema from registered entity types before the server starts.
5. Business services orchestrate repository access, ONVIF devices, live stream sessions, and app workers such as the MyMataSan vision monitor.
6. Transaction coordination uses Redis (or in-memory fallback for single-process/dev apps) to serialize critical file-storage work with FIFO lock acquisition and stuck-lock telemetry.
7. Shared cache layer uses Redis or the in-process default cache depending on the selected app profile.
8. The configured SQL engine stores persistent entities; `mymatasan` defaults to SQLite for small-device deployment.
9. Static SPA assets are served from the selected app directory, for example `apps/mymatasan/static` or `apps/myidsan/static`.

Main components:

- Root app launcher: `main.go` (`-app <name>`)
- Shared runtime host: `infra/apphost/`
- Per-app compile target: `cmd/<app>/main.go`
- Domain contracts/entities: `domain/`
- Shared APIs/services/repos: `domain/shared/`
- Infrastructure adapters (DB/config/login/ONVIF/RTSP/stream/vision): `infra/`
- Embedded version manifest and bump tooling: `infra/versioning/`

## Documentation

Technical documentation is located in `docs/`.

- `docs/README.md`
- `docs/TECHNICAL_SPEC.md`
- `docs/REQUEST_FLOW.md`
- `docs/HOWTO.md`
- `docs/DB_BOOTSTRAP_SPEC.md`
- `docs/modules/` (module docs separated by source filename)

Documentation maintenance policy:

1. Any behavioral code change must update matching module docs in `docs/modules/...`.
2. Any architecture/runtime/ops change must update relevant docs in `docs/`.
3. Any user-facing change to setup/run/architecture must update root `README.md` in the same change.
4. Any bootstrap/database provisioning change must update `docs/DB_BOOTSTRAP_SPEC.md`.

## Project Structure

Relevant top-level layout:

```text
.
|- apps/mymatasan/         # Camera, WebRTC stream, and video intelligence app
|- apps/myidsan/           # Identity, user management, and RBAC administration app
|- domain/                 # Domain entities, enums, shared APIs/services
|- infra/                  # Config, DB, auth login providers, camera adapters
|- deploy/linux/           # systemd templates
|- Dockerfile              # Production image build
|- docker-compose.yml      # App + PostgreSQL compose stack
|- go.mod
```

## Requirements

- Go 1.26.4
- PostgreSQL, MariaDB, or SQLite (for readiness and persistent data)
- Linux runtime target (primary)
- Optional local tools:
  - Docker and Docker Compose
  - FFmpeg (if using camera stream features outside container)
  - Docker daemon (required for MariaDB bootstrap integration test)

## Configuration

Base config files:

- `apps/mymatasan/config.json`
- `apps/mymatasan/config.dev.json`
- `apps/myidsan/config.json`
- `apps/myidsan/config.dev.json`

Environment selection:

- `ENVIRONMENT=dev` uses `config.dev.json`
- otherwise uses `config.json`

### Required Secrets

- `JWT_SECRET` when the selected app config does not already provide `jwt.secret`
- `GOOGLE_CLIENT_SECRET` (required when Google login config is enabled)
- `GITHUB_CLIENT_SECRET` (required when GitHub login config is enabled)

The app fails fast if required secrets are missing.

### Runtime Environment Variables

| Variable | Purpose | Default |
|---|---|---|
| `ENVIRONMENT` | Select config file (`dev` or non-dev) | none |
| `SERVER_HOSTNAMES` | Optional comma-separated hostname/IP list override (use `*` for all NICs) | config value |
| `SERVER_TLS_PORTS` | Optional comma-separated HTTPS listener port override | config value |
| `SERVER_NON_TLS_PORTS` | Optional comma-separated HTTP listener port override | config value |
| `SERVER_PORTS` | Legacy comma-separated shared port list override | config value |
| `SERVER_ENABLE_TLS` | Legacy override for TLS listener mode (`true/false`) | config value |
| `SERVER_ENABLE_NON_TLS` | Legacy override for non-TLS listener mode (`true/false`) | config value |
| `SERVER_ADDR` | Legacy single bind address override (`host:port` or `:port`) | none |
| `SERVER_USE_TLS` | Legacy single mode toggle (`true/false`) | none |
| `JWT_SECRET` | JWT signing/verification secret | none (required) |
| `SSO_ISSUER` | Token issuer claim, usually `myidsan` for relying apps | config value |
| `SSO_AUDIENCE` | Comma-separated accepted token audiences | config value |
| `SSO_SESSION_TTL_SECONDS` | Session cookie and cache TTL in seconds | config value |
| `SSO_POLICY_CACHE_TTL_SECONDS` | RBAC policy cache TTL in seconds | config/cache TTL |
| `SSO_INTERNAL_TOKEN` | Service-to-service token for myidsan SSO fallback APIs | config value |
| `SSO_PROVIDER_BASE_URL` | MyIDSan base URL used by relying apps for authorization-code login | config value |
| `SSO_CA_CERT_PATH` | Optional PEM CA/certificate bundle trusted by relying-app backend calls to MyIDSan | config value |
| `SSO_CLIENT_ID` | Relying-app SSO client ID | config value |
| `SSO_CLIENT_SECRET` | Relying-app SSO client secret | config value |
| `SSO_REDIRECT_PATH` | Relying-app callback path | config value |
| `SSO_AUTH_CODE_TTL_SECONDS` | MyIDSan authorization-code TTL | config value |
| `SSO_ACCESS_TOKEN_TTL_SECONDS` | MyIDSan issued token TTL | config value |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret | none when enabled |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth client secret | none when enabled |
| `DB_HOST` | Override DB host from config file | config value |
| `DB_PORT` | Override DB port from config file | config value |
| `DB_USER` | Override DB user from config file | config value |
| `DB_PASSWORD` | Override DB password from config file | config value |
| `DB_NAME` | Override DB name from config file | config value |
| `DB_SSL_MODE` | Override DB SSL mode from config file | config value |
| `DB_ENGINE` | Override DB engine from config file (`postgres`/`mariadb`/`sqlite`) | `postgres` |
| `CACHE_PROVIDER` | Cache backend provider (`default`, `redis`, `inmemory`, or `memory`) | config value |
| `CACHE_TTL_SECONDS` | Default cache TTL in seconds | config value |
| `CACHE_KEY_PREFIX` | Global cache key prefix | config value |
| `RATE_LIMIT_ENABLED` | Enable API sliding-window rate limiting (`true/false`) | config value |
| `LOG_ENABLED` | Enable runtime file logging (`true/false`) | config value |
| `LOG_PATH` | Runtime log base path for dated daily files; relative paths resolve from app directory | config value |
| `LOG_MAX_LINE_BYTES` | Maximum bytes retained per runtime log line | config value |
| `LOG_CLEANUP_ENABLED` | Enable scheduled runtime log cleanup (`true/false`) | config value |
| `LOG_MAX_RETENTION_DAYS` | Delete dated runtime log files older than this many days | config value |
| `LOG_CLEANUP_FREQUENCY_MINUTES` | Scheduled runtime log cleanup interval in minutes | `60` |
| `API_LOG_CLEANUP_ENABLED` | Enable scheduled database API log cleanup (`true/false`) | config value |
| `API_LOG_MAX_RETENTION_DAYS` | Delete API log rows older than this many days | config value |
| `API_LOG_CLEANUP_FREQUENCY_MINUTES` | Scheduled API log cleanup interval in minutes | `60` |
| `TELEMETRY_ENABLED` | Enable telemetry module (`true/false`) | config value |
| `PROMETHEUS_ENABLED` | Enable Prometheus metrics exporter (`true/false`) | config value |
| `PROMETHEUS_METRICS_PATH` | HTTP path for Prometheus scrape endpoint | config value |
| `PROMETHEUS_API_DURATION_THRESHOLD_MS` | API duration threshold for slow-request metrics | config value |
| `REDIS_ADDR` | Redis endpoint (`host:port`) | config value |
| `REDIS_PASSWORD` | Redis password | config value |
| `REDIS_DB` | Redis DB index | config value |
| `REDIS_USE_TLS` | Redis TLS mode (`true/false`) | config value |
| `REDIS_CONNECT_TIMEOUT_MS` | Redis connect timeout in ms | config value |
| `REDIS_OPERATION_TIMEOUT_MS` | Redis operation timeout in ms | config value |
| `TRANSACTION_LOCK_PROVIDER` | Transaction lock provider (`redis`, `memory`, or `inmemory`; empty inherits cache provider) | config value |
| `TRANSACTION_LOCK_WAIT_TIMEOUT_MS` | Maximum time a request waits for a transaction lock | config value |
| `TRANSACTION_LOCK_LEASE_MS` | Redis lock lease duration, renewed while the owner is active | config value |
| `TRANSACTION_OPERATION_TIMEOUT_MS` | Maximum runtime for coordinated file-storage transaction work | config value |
| `TRANSACTION_STUCK_TIMEOUT_MS` | Duration after which a held transaction lock emits stuck telemetry | config value |
| `TRANSACTION_JOB_WORKER_ENABLED` | Enable durable file-storage upload job worker (`true/false`) | config value |
| `TRANSACTION_JOB_WORKER_FREQUENCY_SECONDS` | Upload job worker polling interval in seconds | config value |
| `TRANSACTION_MAX_ATTEMPTS` | Maximum attempts before a durable upload job is failed and cleaned up | config value |

Bootstrap behavior is controlled by `bootstrap` in config:

- `enabled`: turn startup provisioning on or off
- `autoCreateDatabase`: create the database if missing
- `autoCreateSchema`: create missing tables and columns
- `autoMigrate`: apply safe additive migrations
- `autoSeed`: run seed providers if registered
- `allowReset`: reserved for dev-only destructive operations
- `setupPath`: reserved for bootstrap/setup UI routing
- `seedStatements`: optional SQL statements executed as initial data when `autoSeed` is enabled

The bootstrap seeder also creates a minimal built-in core dataset on first run:

- `user_group`: `system`
- `user_role`: `superadmin` linked to the `system` group
- `user_login`: default superadmin account (`email/username=superadmin`, `password=superadmin123`, stored as bcrypt) linked to the `superadmin` role
- `app_registry` rows for registered SSO apps.
- `user_session` schema for future audit/revocation storage while the current hot path uses cache-backed sessions.
- `api_endpoint` and `api_endpoint_rbac` rows for protected API modules, using `*` as the host so the defaults work on any deployment address. Endpoint rows include `appCode` and `accessTier` (`0=DevOnly`, `1=AuthOnly`, `2=Public`); protected shared management APIs seed as `DevOnly`.

`myidsan` seeds the same default identity dataset plus the identity-management endpoint catalog for login, federated auth, user/group/role, app registry, app auth config, app redirect URI, endpoint, RBAC, cache, log, file-storage administration, and core relying-app policies for `mymatasan` and `myseliasan`.

Credential policy for user creation:

- Username and password cannot be identical.
- The only allowed identical credential pair is the one-time bootstrap default superadmin account (`superadmin` / `superadmin123`).

Local auth endpoints:

- `POST /api/login/default` for username/password login.
- `POST /api/login/default/register` for local account registration.
- Local auth is always available and independent from Google/GitHub OAuth setup.

Standalone MyMataSan auth:

- `mymatasan` app-specific ONVIF, Settings, and Vision APIs use standalone DB-backed HTTP Basic Auth.
- On first startup, `mymatasan` seeds `admin` / `Admin123` when no local users exist.
- User management lives under the `Settings` page and `/api/settings/users`.
- Change the seeded admin password before deploying outside a trusted local network.

Local password storage:

- New local passwords are stored using bcrypt hash.
- Existing legacy plain-text passwords are migrated to bcrypt automatically after a successful local login.

The bootstrap status page and JSON status endpoint are exposed at `setupPath` and `setupPath/status`.

Server listener behavior is controlled by `server` in config:

- `hostnames`: bind targets. Empty or `*` means wildcard bind across available NICs.
- `tlsPorts`: HTTPS listener ports. Empty means no HTTPS listener.
- `nonTlsPorts`: HTTP listener ports. Empty means no HTTP listener.
- `ports`, `enableTls`, `enableNonTls`: legacy shared-port mode used only when `tlsPorts` and `nonTlsPorts` are both empty.

At least one of `tlsPorts` or `nonTlsPorts` must contain a port in the explicit-port model. The same port cannot appear in both lists because HTTP and HTTPS cannot bind the same address at the same time.
When `tlsPorts` is non-empty, `tls.certPath` and `tls.keyPath` must be configured and point to readable files. Relative TLS paths resolve from the selected app directory, for example `apps/myidsan/certs/cert.pem`.

Database behavior is controlled by `db` in config:

- `engine`: selects DB adapter (`postgres`, `mariadb`, or `sqlite`).
- Runtime DB adapter and bootstrap are implemented for `postgres`, `mariadb`, and `sqlite`.
- For SQLite, `db_name` is the database file path; relative paths resolve from the selected app directory. `:memory:` is supported for tests/dev experiments only.
- SQLite runs through the same CRUD/bootstrap contracts, but it is best for single-process or small-device deployments. Prefer PostgreSQL or MariaDB for multi-instance production workloads.
- Core bootstrap seed SQL is dialect-portable for supported engines.
- `apps/mymatasan/config.dev.json` defaults to SQLite at `./data/mymatasan.db` with `CACHE_PROVIDER=default` for standalone small-device operation.

MySeliaSan local development:

1. Start MyIDSan at `https://localhost:3001`.
2. Start MySeliaSan at `https://localhost:3002`.
3. Open `https://localhost:3002`; the root page redirects to MyIDSan when no MySeliaSan session exists.

For local HTTPS, replace the app cert/key files under `apps/myidsan/certs` and `apps/myseliasan/certs` with certificates signed by a CA trusted by the machine running MySeliaSan. The callback flow includes a backend HTTPS token exchange from MySeliaSan to MyIDSan. If the CA is not in the OS trust store, set `sso.caCertPath` or `SSO_CA_CERT_PATH` in MySeliaSan to a PEM CA bundle; relative paths resolve from `apps/myseliasan`.
`sso.caCertPath` adds trust roots only for that backend token exchange. It does not skip TLS verification, so expired certificates, wrong hostnames, and invalid chains still fail. Outside dev mode those failures can appear to the browser as `403 limited access`; check MySeliaSan logs for the exact token-exchange error.

```bash
export ENVIRONMENT=dev
export JWT_SECRET=replace-with-strong-secret
go run . -app myseliasan
```

Telemetry behavior is controlled by `telemetry` in config:

- `enabled`: enables shared telemetry wiring.
- `prometheus.enabled`: exposes Prometheus-format metrics.
- `prometheus.metricsPath`: scrape endpoint path, default `/metrics`.
- `prometheus.apiDurationThresholdMs`: threshold used for slow API request metrics.

Rate limiting is controlled by `rateLimit` in config:

- `enabled`: enables sliding-window limits for `/api` routes.
- `endpointCacheTtlSeconds`: caches endpoint access-tier metadata.
- `defaultWindowSeconds`: fallback window for tier configs.
- `devOnly`, `authOnly`, `public`: per-tier `enabled`, `requests`, and `windowSeconds`.
- Redis cache is recommended for production multi-instance deployments so counters are shared.

Transaction coordination behavior is controlled by `transaction` in config:

- `lockProvider`: selects lock backend. Empty inherits `cache.provider`; Redis is recommended for multi-instance production.
- `lockWaitTimeoutMs`: cancels lock wait when FIFO queue acquisition takes too long.
- `lockLeaseMs`: Redis owner lease duration; owners renew while active.
- `operationTimeoutMs`: bounds coordinated file-storage transaction work.
- `stuckTimeoutMs`: emits telemetry when a lock is held longer than expected.
- `jobWorkerEnabled`: starts the durable file-storage upload worker.
- `jobWorkerFrequencySeconds`: controls how often the worker recovers stale jobs and processes queued work.
- `maxAttempts`: caps upload job retries before final failure cleanup.

File storage supports both synchronous and durable async upload boundaries:

- `POST /api/file-storage/upload`: stages files, then runs the DB metadata insert plus final file write in one coordinated request.
- `POST /api/file-storage/upload-async`: stages files and creates an `operation_job` row for the backend worker.
- Both upload endpoints accept `securityLvl` (`0=SystemOnly`, `1=Group`, `2=Role`, `3=Public`) and optional expiry via either absolute Unix-second `expiredAt` or countdown `expiresIn` plus `expiresInUnit`; omitted values default to `SystemOnly` and no expiry.
- `GET /api/file-storage/download?id=<id>` and `GET /api/file-storage/download?ids=<ids>` download by metadata ID only. GUIDs remain internal storage identifiers. Add `view=true` on a single `id` download to render inline in the browser.
- File-storage cleanup is controlled by `fileStorage.cleanup.enabled`, `fileStorage.cleanup.frequencySeconds`, and `fileStorage.cleanup.batchSize`; expired rows are swept by the runtime scheduler.
- `GET /api/file-storage/job?id=<id>`: reads upload job status, attempt count, deadlines, result, and last error.

For production multi-instance deployments, use Redis transaction locking and keep the async worker enabled. That makes upload requests short-lived while preserving FIFO execution, retry, stale-job recovery, and cleanup behavior in the backend.

## Makefile Commands

The repository includes a `Makefile` for standard development and operations commands.

If `make` is not installed in your shell, use the direct commands shown in the Run and Testing sections.

```bash
make help          # list available commands
make run APP=...   # run selected app locally (requires DB + env)
make build APP=... # build selected app binary only
make test          # run all tests
make test-app APP=... # run selected app tests only
make test-mid      # run middleware tests only
make test-bootstrap-mariadb # run Docker-backed MariaDB bootstrap integration test
make docker-build APP=... # build docker image for selected app
make up            # start docker compose stack
make down          # stop docker compose stack
make logs          # tail compose logs
```

## Run Locally

From repository root:

```bash
go run . -app mymatasan
```

Example local environment:

```bash
export ENVIRONMENT=dev
export SERVER_HOSTNAMES=*
export SERVER_NON_TLS_PORTS=3000
export SERVER_TLS_PORTS=
export CACHE_PROVIDER=default
export DB_ENGINE=sqlite
export DB_NAME=./data/mymatasan.db
go run . -app mymatasan
```

Run the identity app:

```bash
go run . -app myidsan
```

The dev config file defaults `db.port` to `5433`. The example above overrides it to `5432`; keep whichever port matches your local PostgreSQL.
`apps/myidsan/config.dev.json` also defaults PostgreSQL to port `5433`, database `myidsandb`, and HTTPS port `3001`. Both MyIDSan configs expect `apps/myidsan/certs/cert.pem` and `apps/myidsan/certs/key.pem`.

For a small single-process local run, switch any app to SQLite with:

```bash
export DB_ENGINE=sqlite
export DB_NAME=./data/kopiv2.db
go run . -app mymatasan
```

When SQLite is selected, `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, and `DB_SSL_MODE` are ignored by the adapter.

## Run with Docker

Build image:

```bash
docker build --build-arg APP=mymatasan -t kopiv2:mymatasan .
```

Build the identity app image:

```bash
docker build --build-arg APP=myidsan -t kopiv2:myidsan .
```

Run standalone MyMataSan with its local SQLite/default-cache profile:

```bash
docker run --rm -p 3000:3000 \
  -e ENVIRONMENT=dev \
  -e SERVER_HOSTNAMES=* \
  -e SERVER_NON_TLS_PORTS=3000 \
  -e SERVER_TLS_PORTS= \
  -e DB_ENGINE=sqlite \
  -e DB_NAME=./data/mymatasan.db \
  -e CACHE_PROVIDER=default \
  kopiv2:latest
```

## Run with Docker Compose

Use bundled app + PostgreSQL + Redis stack:

```bash
docker compose up --build
```

Run detached:

```bash
docker compose up -d --build
```

Stop stack:

```bash
docker compose down
```

## Linux systemd Deployment

Templates included:

- `deploy/linux/kopiv2.service`
- `deploy/linux/kopiv2.env.example`

Suggested flow:

1. Copy binary and app files under `/opt/kopiv2`.
2. Copy environment file to `/etc/kopiv2/kopiv2.env`.
3. Install service unit to `/etc/systemd/system/kopiv2.service`.
4. Enable and start service.

Commands:

```bash
sudo systemctl daemon-reload
sudo systemctl enable kopiv2
sudo systemctl start kopiv2
sudo systemctl status kopiv2
```

## API Health Endpoints

- `GET /health` -> liveness probe
- `GET /ready` -> readiness probe (includes DB and cache ping)
- `GET /api/health` -> API namespace health
- `GET /api/version` -> public runtime version for the selected app and shared core
- `GET /metrics` -> Prometheus metrics endpoint when telemetry is enabled

## Versioning

Runtime versioning uses standard SemVer with separate core and app versions:

- `core.version` tracks shared framework/runtime code (`infra`, `domain`, shared APIs/services).
- `apps.<name>.version` tracks one app module, such as `mymatasan`.
- `myidsan` is included as its own app version entry.

The public endpoint only returns the running app version and the shared core version:

```bash
curl http://localhost:3000/api/version
```

Response shape:

```json
{
  "message": "succeed",
  "result": {
    "app": "mymatasan",
    "appVersion": "1.0.0",
    "coreVersion": "1.0.0",
    "commit": "abc123",
    "updatedAt": "2026-06-07T12:00:00Z"
  }
}
```

Version bumps are driven by pending changelog entries under `changes/pending/YYYYMMDD-HHMMSS-short-title/change.json`.
On push to `main`, `.github/workflows/main.yml` runs `go run ./infra/versioning/cmd/versionbump`, updates `infra/versioning/version.json`, and moves processed entries to `changes/applied/`.
Pending entries support the legacy `level/scope/app` shape and the newer multi-target shape, for example `{"type":"minor","scope":"core,myidsan,mymatasan","summary":"..."}`. `type` is resolved to `major`, `minor`, or `patch`; comma-separated scopes can target core aliases and one or more app names from the manifest.

## Telemetry

Prometheus telemetry is enabled by default in app config and exposed at `/metrics`.

Initial metrics:

- `kopiv2_api_requests_total`
- `kopiv2_api_request_duration_ms`
- `kopiv2_api_slow_requests_total`
- `kopiv2_api_slow_request_duration_ms`
- `kopiv2_tx_lock_events_total`
- `kopiv2_tx_lock_wait_ms`
- `kopiv2_tx_lock_stuck_total`

API metrics use restrained labels: `app`, `method`, route-template `path`, and `status`.
Slow-request metrics increment only when API duration is greater than or equal to `telemetry.prometheus.apiDurationThresholdMs`.
Transaction lock metrics use restrained labels: `app`, `provider`, low-cardinality `resource`, and `outcome`.
Stuck transaction locks and stale async upload jobs are visible through telemetry/logging: lock-level stuck observations increment Prometheus metrics, and recovered/processed upload jobs are logged by the scheduler.

## Cache Admin Endpoints

Protected endpoints (admin via RBAC):

- `GET /api/cache-service` -> list cache keys (supports `prefix`, `limit`, `offset` query)
- `GET /api/cache-service/health` -> cache provider health check
- `DELETE /api/cache-service` -> wipe cache by `key`, by `prefix`, or all entries with `wipeAll=true`
- `POST /api/cache-service/wipe` -> wipe cache using JSON body (`key`, `prefix`, `wipeAll`)
- `GET /api/log` -> list database-backed API activity logs
- `DELETE /api/log?year=YYYY&month=MM` -> delete API activity logs for one month
- `GET /api/log-service` -> list runtime service logs from the configured log file
- `DELETE /api/log-service?year=YYYY&month=MM` -> delete dated runtime log files for one month

Safety rule:

- `DELETE /api/cache-service` requires one of `key`, `prefix`, or `wipeAll=true`.
- `POST /api/cache-service/wipe` requires one of `key`, `prefix`, or `wipeAll=true` in payload.
- `DELETE /api/log` cannot delete the current month. This rule is enforced in the API log service, not only the HTTP API.
- `DELETE /api/log-service` cannot delete the current month. This rule is enforced in the runtime log service, not only the HTTP API.

Audit behavior:

- Successful wipe actions are recorded in API logs with actor, request URL, source IP, and wipe mode.
- API activity rows include `durationMs`, the elapsed request handling time in milliseconds.

## API Documentation (Swagger)

Runtime serves OpenAPI and Swagger UI for FE integration:

- `GET /swagger` -> Swagger UI
- `GET /swagger/openapi.json` -> OpenAPI 3.0 JSON

Notes:

- Endpoint list is auto-discovered from registered Gorilla Mux routes, including shared APIs and app-specific APIs.
- Standard JSON API responses include top-level `durationMs`, the elapsed request handling time in milliseconds.
- Auth-protected `/api/*` routes are marked with cookie session auth in docs, except login/callback and `/api/health`; unsafe methods also require `X-CSRF-Token`.
- Key endpoints include reusable request/response schema components under `components.schemas`.
- Key list/create/update endpoints now use endpoint-specific wrapper schemas (for example: `PagingUserGroupResponse` and `DefaultUserGroupResponse`) so FE clients can generate stricter typed models.
- Shared DB-backed list endpoints document `limit`, `offset`, `filters`, and `sorters` query parameters using the shared SQL enum contract (`compare`: 1 eq, 2 neq, 3 gt, 4 lt, 5 gte, 6 lte; `sort`: 1 asc, 2 desc). `filters` and `sorters` accept a JSON object or array; repeated `filter` and `sorter` query parameters are also supported. Multiple filters are combined by `AND`, and multiple sorters are applied in request order. Paging responses return offset-window metadata: `limit`, `offset`, `resCnt`, `totalCnt`, `hasNext`, and `nextOffset`.
- Non-JSON routes are documented with explicit response contracts as well (for example: OAuth login redirects with `302` and binary file download with `application/octet-stream`).
- Cache admin endpoints are included in the generated spec (`cache-service` tag) for list, health, and wipe operations.
- Runtime log listing and monthly deletion are included in the generated spec (`log-service` tag).
- Runtime version is included in the generated spec (`system` tag).
- File-storage sync upload, async upload, ID download, inline view, expiry, security level, and job status endpoints are included in the generated spec (`file-storage` tag).
- Cache wipe is available in both query-based (`DELETE`) and payload-based (`POST /wipe`) contracts.
- Each app can enrich summaries/descriptions by implementing the shared API docs provider in its app module.

## Identity and SSO Direction

`myidsan` is the identity-provider foundation for single sign-on across `kopiv2` apps. It owns the identity/RBAC management surface, app registry, app auth config, redirect URI allow-list, token issuer settings, cache-backed sessions, browser authorization-code flow, and internal introspection/authorization APIs for relying apps.

`mymatasan` is now treated as a standalone device app: it does not mount MyIDSan login, SSO browser callback, user/group/role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, or cache-service management APIs. Its app-specific ONVIF, Settings, and Vision APIs are protected by standalone DB-backed Basic Auth until the stricter MySeliaSan-to-MyMataSan control protocol is defined. Saved-camera live view uses configurable RTSP-to-WebRTC H264 forwarding first, with MJPEG retained as a fallback or primary mode when WebRTC is disabled.

MyMataSan ONVIF endpoints:

- `POST /api/onvif/discover` -> local WS-Discovery scan.
- `POST /api/onvif/probe` -> manual IP/host/device-service URL probe.
- `GET /api/onvif/devices` -> list saved ONVIF devices.
- `GET /api/onvif/stream-config` -> read WebRTC, ICE server, and MJPEG fallback live-view settings.
- `POST /api/onvif/devices` and `POST /api/onvif/devices/discovered` -> save or update a device by XAddr.
- `POST /api/onvif/devices/{id}/stream-uri` -> resolve a saved ONVIF device to an RTSP URI.
- `POST /api/onvif/devices/{id}/camera-password` -> change a camera-local ONVIF user password with Device Management `SetUser`.
- `POST /api/onvif/devices/{id}/rtsp-test` -> probe the saved RTSP URI and store transport/track metadata.
- `POST /api/onvif/devices/{id}/live-view` -> resolve saved ONVIF stream and snapshot URIs for browser live view.
- `POST /api/onvif/devices/{id}/webrtc/offer` -> answer a browser WebRTC offer and forward H264 RTSP RTP packets.
- `POST /api/onvif/devices/{id}/ptz/move` -> move a saved PTZ-capable camera with ONVIF `ContinuousMove`.
- `POST /api/onvif/devices/{id}/ptz/stop` -> stop ONVIF PTZ movement.
- `GET /api/onvif/devices/{id}/live.mjpeg` -> stream RTSP or snapshot frames as browser-friendly MJPEG fallback.
- `DELETE /api/onvif/devices/{id}` -> remove a saved device.
- `GET /api/settings/runtime` -> read runtime Decoder and Live Stream settings.
- `PUT /api/settings/runtime` -> update runtime settings without restarting `mymatasan`.
- `POST /api/settings/runtime/reset` -> reset runtime settings to startup config defaults.
- `GET /api/settings/users` -> list local login users.
- `POST /api/settings/users` -> create a local login user.
- `PUT /api/settings/users/{id}` -> update username, display name, admin flag, and active flag.
- `POST /api/settings/users/{id}/password` -> reset a local user's password.
- `DELETE /api/settings/users/{id}` -> delete a local user.
- `GET /api/vision/rules` -> list camera detection rules.
- `POST /api/vision/rules` -> create or update a camera detection rule with detection type, polygon, threshold, cooldown, sound setting, and optional rule-level schedule policy.
- `DELETE /api/vision/rules/{id}` -> delete a detection rule.
- `GET /api/vision/alerts` -> list AI alert events.
- `POST /api/vision/alerts` -> create an alert event for integration checks or detector output.
- `POST /api/vision/alerts/{id}/ack` -> acknowledge an alert event.

MyMataSan vision rules are camera-first in the frontend. The reusable `infra/vision` package owns the app-neutral rule, alert, schedule, frame, and detector contracts. The current MVP detector compares consecutive JPEG frames inside the configured polygon, applies threshold/min-frame/cooldown settings, and writes alert events that the AI page and live-view tiles can surface.

MyIDSan's admin SPA derives navigation from `/api/endpoint-rbac/ep/me` plus endpoint metadata. Page visibility and create, edit, and delete buttons follow the same RBAC method grants; browser-readable cookies remember only presentation state such as active page, filters, sorting, and table page.

SSO flow:

1. Users authenticate at `myidsan`.
2. `myidsan` issues an HMAC JWT with standard `iss`, multi-value `aud`, `exp`, and an SSO session id (`sid`).
3. The session is cached under `sso:session:<sid>` using the configured cache provider. Redis shares that session across apps; memory cache is process-local.
4. Resource apps validate issuer/audience locally and cache RBAC policies by resource app, role, and policy version.
5. With in-memory cache only, resource apps can call `POST /api/sso/introspect` and `POST /api/sso/authorize` on `myidsan` using `X-Myidsan-Internal-Token` or `Authorization: Bearer <SSO_INTERNAL_TOKEN>`.

Browser relying-app flow:

1. A relying app such as `myseliasan` redirects unauthenticated users to `myidsan /api/auth/authorize`.
2. MyIDSan validates `client_id`, `audience`, and exact `redirect_uri` from `app_auth_config` and `app_redirect_uri`.
3. If the user is not signed in to MyIDSan, MyIDSan serves `/api/auth/login` and resumes authorization after login.
4. MyIDSan returns a short-lived one-time code to the relying app callback.
5. The relying app exchanges the code at `/api/auth/token` using its client secret, then creates its own HttpOnly session cookie.

`api_endpoint.appCode` scopes RBAC endpoint rows per app. Fresh bootstrap uses `appCode + host + path` as the intended uniqueness shape; older databases that already created the previous host/path unique index may need an operator migration before storing duplicate paths for multiple apps.

## Testing

Run focused middleware tests:

```bash
make test-mid
```

Run app integration tests:

```bash
make test-app
```

Run all tests:

```bash
make test
```

Run real MariaDB bootstrap integration test:

```bash
make test-bootstrap-mariadb
```

Equivalent direct command:

```bash
RUN_MARIADB_IT=1 go test ./infra/db/bootstrap -run TestBootstrapEnsureMariaDBIntegration -v
```

## Production Checklist

Before shipping to production, verify:

1. Secrets and auth:
  - `JWT_SECRET` is strong and rotated via secure secret management.
  - `GOOGLE_CLIENT_SECRET` is set when Google login is enabled.

2. Database and readiness:
  - production DB credentials are not default values.
  - `GET /ready` returns 200 in deployment environment.

3. Network and transport:
  - service is behind reverse proxy or load balancer with TLS termination, or TLS is enabled in app.
  - do not assign the same port to both `tlsPorts` and `nonTlsPorts`.
  - only required ports are exposed.

4. Logging and observability:
  - request logs are collected centrally.
  - health and readiness endpoints are wired to your monitor/probe system.

5. Runtime hardening:
  - run as non-root user where possible.
  - restart policy is enabled (`systemd` or orchestrator equivalent).
  - resource limits are set (CPU/memory/file descriptors).

6. Backup and recovery:
  - backup schedule is configured and tested for the selected DB engine.
  - restore drill is validated on staging.

## Operational Hardening Included

Implemented baseline hardening:

- JWT auth + RBAC route protection.
- Sliding-window API rate limits by endpoint access tier.
- Request ID propagation via `X-Request-ID`.
- Structured request and service logging with timing, status code, file persistence, and stdout teeing.
- HTTP server timeout guards.
- DB and cache-backed readiness checks.
- Graceful shutdown on `SIGINT` and `SIGTERM`.
- Camera stream worker lifecycle management and coordinated drain.
- Shared scheduler available to app modules for reminders and other periodic jobs.
- Error wrapping in data layer for better diagnostics.

## Troubleshooting

Common issues:

1. Container exits on startup with DB error:
	- ensure `DB_HOST` points to reachable PostgreSQL from container.
	- for local Docker on Windows/macOS, use `host.docker.internal`.
	- or use `docker compose up --build` to run app and DB together.

2. `GET /ready` returns non-200:
  - verify DB credentials and network reachability.
  - verify Redis endpoint/password and connectivity when `CACHE_PROVIDER=redis`.

3. TLS startup error:
  - if `server.tlsPorts` or `SERVER_TLS_PORTS` contains ports, ensure `tls.certPath` and `tls.keyPath` are configured and the files exist.
  - relative TLS paths resolve from the selected app folder, such as `apps/myidsan/certs`.
  - for plain HTTP local runs, leave `tlsPorts` empty and set `nonTlsPorts` to the desired ports.

4. MySeliaSan callback returns `403 limited access`:
  - this is usually a hidden callback/token-exchange error, not MyIDSan endpoint RBAC.
  - verify `sso.providerBaseUrl`, `sso.redirectBaseUrl`, client id/secret, and exact registered callback URI.
  - for local HTTPS, verify the MyIDSan cert is not expired, is valid for `localhost`, and is trusted by the MySeliaSan backend through the OS trust store or `sso.caCertPath`/`SSO_CA_CERT_PATH`.
  - restart both apps after replacing certificate files, because listeners load TLS material at startup.

5. Startup fails with server port error:
  - ensure at least one of `server.tlsPorts` or `server.nonTlsPorts` contains a valid port.
  - ensure the same port is not listed in both `tlsPorts` and `nonTlsPorts`.
  - if using env overrides, set `SERVER_TLS_PORTS` and/or `SERVER_NON_TLS_PORTS` with valid comma-separated integers.

6. Live preview or vision sampling cannot capture frames:
  - verify the saved device has an RTSP URI or snapshot URI by resolving live view from the camera settings page.
  - verify `decoder.mjpeg.ffmpegPath` points to a working ffmpeg executable when MJPEG fallback or vision RTSP snapshots are used.
  - verify the camera credentials are saved when the camera requires authenticated ONVIF, snapshot, or RTSP access.
