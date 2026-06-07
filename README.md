# kopiv2

Lightweight Go backend for Linux-first deployments, including small devices, with JWT auth, RBAC, camera streaming, and PostgreSQL persistence.

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

This repository contains a modular backend application located at `apps/mymatasan`.

Primary goals:

- Linux compatibility.
- Lightweight runtime footprint.
- Stable service behavior (timeouts, graceful shutdown, readiness checks).
- Secure API path with authentication and authorization controls.

## Architecture

High-level flow:

1. Incoming HTTP requests are handled by Gorilla Mux.
2. Global middleware applies CORS, request logging, and request ID tracing.
3. API routes under `/api` persist activity and elapsed duration into `api_log`, then protected routes pass through JWT authentication and RBAC authorization.
4. A shared bootstrap engine checks/creates the database and syncs schema from registered entity types before the server starts.
5. Business services orchestrate repository access and camera stream workers.
6. Shared cache layer uses Redis (or in-memory fallback) for cross-instance RBAC cache consistency.
7. PostgreSQL stores persistent entities.
8. Static SPA assets are served from `apps/mymatasan/static`.

Main components:

- Root app launcher: `main.go` (`-app <name>`)
- Shared runtime host: `infra/apphost/`
- Per-app compile target: `cmd/<app>/main.go`
- Domain contracts/entities: `domain/`
- Shared APIs/services/repos: `domain/shared/`
- Infrastructure adapters (DB/config/login/camera): `infra/`
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
|- apps/mymatasan/         # Main app module (HTTP server, APIs, services, static files)
|- domain/                 # Domain entities, enums, shared APIs/services
|- infra/                  # Config, DB, auth login providers, camera adapters
|- deploy/linux/           # systemd templates
|- Dockerfile              # Production image build
|- docker-compose.yml      # App + PostgreSQL compose stack
|- go.mod
```

## Requirements

- Go 1.26.4
- PostgreSQL (for readiness and persistent data)
- Linux runtime target (primary)
- Optional local tools:
  - Docker and Docker Compose
  - FFmpeg (if using camera stream features outside container)
  - Docker daemon (required for MariaDB bootstrap integration test)

## Configuration

Base config files:

- `apps/mymatasan/config.json`
- `apps/mymatasan/config.dev.json`

Environment selection:

- `ENVIRONMENT=dev` uses `config.dev.json`
- otherwise uses `config.json`

### Required Secrets

- `JWT_SECRET`
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
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret | none when enabled |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth client secret | none when enabled |
| `DB_HOST` | Override DB host from config file | config value |
| `DB_PORT` | Override DB port from config file | config value |
| `DB_USER` | Override DB user from config file | config value |
| `DB_PASSWORD` | Override DB password from config file | config value |
| `DB_NAME` | Override DB name from config file | config value |
| `DB_SSL_MODE` | Override DB SSL mode from config file | config value |
| `DB_ENGINE` | Override DB engine from config file (`postgres`/`mariadb`) | `postgres` |
| `CACHE_PROVIDER` | Cache backend provider (`default`, `redis`, `inmemory`, or `memory`) | config value |
| `CACHE_TTL_SECONDS` | Default cache TTL in seconds | config value |
| `CACHE_KEY_PREFIX` | Global cache key prefix | config value |
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
- `api_endpoint` and `api_endpoint_rbac` rows for the protected API modules, using `*` as the host so the defaults work on any deployment address

Credential policy for user creation:

- Username and password cannot be identical.
- The only allowed identical credential pair is the one-time bootstrap default superadmin account (`superadmin` / `superadmin123`).

Local auth endpoints:

- `POST /api/login/default` for username/password login.
- `POST /api/login/default/register` for local account registration.
- Local auth is always available and independent from Google/GitHub OAuth setup.

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

Database behavior is controlled by `db` in config:

- `engine`: selects DB adapter (`postgres` or `mariadb`).
- Runtime DB adapter and bootstrap are implemented for both `postgres` and `mariadb`.
- Core bootstrap seed SQL is dialect-portable for both supported engines.

Telemetry behavior is controlled by `telemetry` in config:

- `enabled`: enables shared telemetry wiring.
- `prometheus.enabled`: exposes Prometheus-format metrics.
- `prometheus.metricsPath`: scrape endpoint path, default `/metrics`.
- `prometheus.apiDurationThresholdMs`: threshold used for slow API request metrics.

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
export JWT_SECRET=replace-with-strong-secret
export GOOGLE_CLIENT_SECRET=replace-with-google-secret
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=postgres
export DB_NAME=mymatasandb
export DB_SSL_MODE=disable
export DB_ENGINE=postgres
export CACHE_PROVIDER=default
export CACHE_TTL_SECONDS=30
export CACHE_KEY_PREFIX=kopiv2
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD='Simpnify@123'
export REDIS_DB=0
export REDIS_USE_TLS=false
go run . -app mymatasan
```

## Run with Docker

Build image:

```bash
docker build --build-arg APP=mymatasan -t kopiv2:mymatasan .
```

Run container against external PostgreSQL:

```bash
docker run --rm -p 3000:3000 \
  -e ENVIRONMENT=dev \
  -e SERVER_HOSTNAMES=* \
  -e SERVER_NON_TLS_PORTS=3000 \
  -e SERVER_TLS_PORTS= \
  -e JWT_SECRET=replace-with-strong-secret \
  -e GOOGLE_CLIENT_SECRET=replace-with-google-secret \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=5432 \
  -e DB_USER=postgres \
  -e DB_PASSWORD=postgres \
  -e DB_NAME=mymatasandb \
  -e DB_SSL_MODE=disable \
  -e CACHE_PROVIDER=default \
  -e CACHE_TTL_SECONDS=30 \
  -e CACHE_KEY_PREFIX=kopiv2 \
  -e REDIS_ADDR=host.docker.internal:6379 \
  -e REDIS_PASSWORD='Simpnify@123' \
  -e REDIS_DB=0 \
  -e REDIS_USE_TLS=false \
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

## Telemetry

Prometheus telemetry is enabled by default in app config and exposed at `/metrics`.

Initial metrics:

- `kopiv2_api_requests_total`
- `kopiv2_api_request_duration_ms`
- `kopiv2_api_slow_requests_total`
- `kopiv2_api_slow_request_duration_ms`

API metrics use restrained labels: `app`, `method`, route-template `path`, and `status`.
Slow-request metrics increment only when API duration is greater than or equal to `telemetry.prometheus.apiDurationThresholdMs`.

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
- Key list/create/update endpoints now use endpoint-specific wrapper schemas (for example: `PagingUserGroupResponse`, `DefaultUserGroupResponse`, `PagingCameraStreamResponse`) so FE clients can generate stricter typed models.
- Non-JSON routes are documented with explicit response contracts as well (for example: OAuth login redirects with `302`, MJPEG stream with `206 multipart/x-mixed-replace`, binary file download with `application/octet-stream`).
- Cache admin endpoints are included in the generated spec (`cache-service` tag) for list, health, and wipe operations.
- Runtime log listing and monthly deletion are included in the generated spec (`log-service` tag).
- Runtime version is included in the generated spec (`system` tag).
- Cache wipe is available in both query-based (`DELETE`) and payload-based (`POST /wipe`) contracts.
- Each app can enrich summaries/descriptions by implementing the shared API docs provider in its app module.

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
  - PostgreSQL backup schedule is configured and tested.
  - restore drill is validated on staging.

## Operational Hardening Included

Implemented baseline hardening:

- JWT auth + RBAC route protection.
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
  - if `server.tlsPorts` or `SERVER_TLS_PORTS` contains ports, ensure cert/key paths in config are valid.
  - for plain HTTP local runs, leave `tlsPorts` empty and set `nonTlsPorts` to the desired ports.

4. Startup fails with server port error:
  - ensure at least one of `server.tlsPorts` or `server.nonTlsPorts` contains a valid port.
  - ensure the same port is not listed in both `tlsPorts` and `nonTlsPorts`.
  - if using env overrides, set `SERVER_TLS_PORTS` and/or `SERVER_NON_TLS_PORTS` with valid comma-separated integers.

5. Startup fails with camera autostart query error:
  - if no camera rows are marked `AutoStart=true`, startup now continues normally.
  - if you still see camera startup errors, verify camera stream rows and source URLs.
