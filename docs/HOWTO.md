# How-To Guide

## Local Development

1. Provide required environment variables.
2. Ensure PostgreSQL is reachable.
3. `apps/mymatasan/config.dev.json` defaults PostgreSQL to port `5433`; set `DB_PORT` if your local database uses another port.
4. Use `CACHE_PROVIDER=default` for local in-process memory cache; ensure Redis is reachable only when `CACHE_PROVIDER=redis`.
5. Run:

```bash
go run . -app mymatasan
```

Run the identity app instead:

```bash
go run . -app myidsan
```

Or with make:

```bash
make run APP=mymatasan
```

```bash
make run APP=myidsan
```

Build only one app binary:

```bash
make build APP=mymatasan
```

```bash
make build APP=myidsan
```

## Run Tests

```bash
make test-mid
make test-app
make test
make test-bootstrap-mariadb
```

Run only the MariaDB bootstrap integration test directly:

```bash
RUN_MARIADB_IT=1 go test ./infra/db/bootstrap -run TestBootstrapEnsureMariaDBIntegration -v
```

## Build and Run Docker

```bash
docker build --build-arg APP=mymatasan -t kopiv2:mymatasan .
```

Build the identity app image:

```bash
docker build --build-arg APP=myidsan -t kopiv2:myidsan .
```

Run against external DB:

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

## Run Full Stack (App + PostgreSQL + Redis)

```bash
make up
```

Health checks:

```bash
curl http://localhost:3000/health
curl http://localhost:3000/ready
curl http://localhost:3000/api/version
curl http://localhost:3000/metrics
```

## Prometheus Metrics

Prometheus metrics are enabled by default in app config:

```json
"telemetry": {
  "enabled": true,
  "prometheus": {
    "enabled": true,
    "metricsPath": "/metrics",
    "apiDurationThresholdMs": 1000
  }
}
```

Scrape the metrics endpoint:

```bash
curl http://localhost:3000/metrics
```

Slow API metrics are emitted when request duration is greater than or equal to `apiDurationThresholdMs`.

Transaction lock metrics are emitted by the file-storage transaction coordinator:

```text
kopiv2_tx_lock_events_total
kopiv2_tx_lock_wait_ms
kopiv2_tx_lock_stuck_total
```

Use Redis transaction locking for multi-instance deployments:

```json
"transaction": {
  "lockProvider": "redis",
  "lockWaitTimeoutMs": 30000,
  "lockLeaseMs": 10000,
  "operationTimeoutMs": 30000,
  "stuckTimeoutMs": 30000,
  "jobWorkerEnabled": true,
  "jobWorkerFrequencySeconds": 5,
  "maxAttempts": 3
}
```

For single-process development only, `lockProvider` can be `memory` or `inmemory`.

Queue file-storage work through the durable backend worker:

```bash
curl -X POST -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H "Idempotency-Key: upload-20260607-001" \
  -F "documents=@receipt.pdf;type=application/pdf" \
  -F "securityLvl=1" \
  -F "expiresIn=2" \
  -F "expiresInUnit=month" \
  "http://localhost:3000/api/file-storage/upload-async"
```

Check job status:

```bash
curl -b cookies.txt "http://localhost:3000/api/file-storage/job?id=1"
```

Use the synchronous upload endpoint only when the caller can safely wait for the complete DB plus file-storage transaction:

```bash
curl -X POST -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" \
  -F "documents=@receipt.pdf;type=application/pdf" \
  -F "securityLvl=3" \
  "http://localhost:3000/api/file-storage/upload"
```

File-storage security levels:

- `0` = `SystemOnly`: only internal service callers can retrieve the file.
- `1` = `Group`: authenticated users whose role belongs to the file owner's group can retrieve the file.
- `2` = `Role`: authenticated users with the owner's role or an ancestor role can retrieve the file.
- `3` = `Public`: any caller can retrieve the file, authenticated or not.

Expiry can be supplied either as absolute `expiredAt` Unix seconds or as a countdown with `expiresIn` plus `expiresInUnit`. Countdown units accept `second`, `minute`, `hour`, `day`, `week`, `month`, and `year` with plural and short aliases. Do not send `expiredAt` together with countdown fields. Empty values mean no expiry. Expired files are denied on download and swept by the scheduler configured in `fileStorage.cleanup`:

```json
"fileStorage": {
  "path": "./uploads",
  "cleanup": {
    "enabled": true,
    "frequencySeconds": 60,
    "batchSize": 100
  }
}
```

Download one stored file by metadata ID:

```bash
curl -b cookies.txt -o receipt.pdf "http://localhost:3000/api/file-storage/download?id=1"
```

Render one stored image, PDF, or text file inline when the browser supports it:

```bash
curl -b cookies.txt "http://localhost:3000/api/file-storage/download?id=1&view=true"
```

Download multiple stored files as a ZIP archive:

```bash
curl -b cookies.txt -o files.zip "http://localhost:3000/api/file-storage/download?ids=1,2,3"
```

## Add a Version Changelog Entry

Create one pending change folder before merging work that should bump runtime version metadata:

```text
changes/pending/YYYYMMDD-HHMMSS-short-title/change.json
```

Example app-only patch:

```json
{
  "level": "patch",
  "scope": "app",
  "app": "mymatasan",
  "summary": "Fix camera stream pagination"
}
```

Supported `level` values are `major`, `minor`, and `patch`.
Supported `scope` values are `core`, `app`, and `both`.
For multi-app or service-split changes, `type` can be used as a level alias and `scope` can be a comma-separated target list:

```json
{
  "type": "minor",
  "scope": "core,myidsan,mymatasan",
  "summary": "Add cross-app SSO policy cache"
}
```

Supported `type` values that map to version levels include `major`, `minor`, `patch`, `added`, `changed`, `removed`, `deprecated`, `security`, `fixed`, `docs`, `cleanup`, and `refactor`.
Comma-separated `scope` values can include `core` aliases (`core`, `shared`, `apphost`, `infra`, `domain`, `bootstrap`, `config`) and app names from `infra/versioning/version.json`.

When pushed to `main`, GitHub Actions updates `infra/versioning/version.json` and moves processed entries to `changes/applied`.

Run Redis integration cache test (local dev):

```bash
RUN_REDIS_IT=1 REDIS_ADDR=localhost:6379 REDIS_PASSWORD='Simpnify@123' go test ./infra/cache -run TestRedisStoreIntegration -v
```

Stop:

```bash
make down
```

## Access API Docs

After app starts, open:

```text
http://localhost:3000/swagger
```

Raw OpenAPI JSON:

```text
http://localhost:3000/swagger/openapi.json
```

FE teams can import `/swagger/openapi.json` into API clients/codegen tools.

For `myidsan`, the default dev URL is:

```text
http://localhost:3001/swagger
```

## Identity App

`myidsan` runs its own login, user, group, and role APIs, plus app-registry, endpoint, endpoint-RBAC, cache, log, runtime-log, file-storage, and version APIs as an identity-management app.

Start it locally:

```bash
export ENVIRONMENT=dev
export JWT_SECRET=replace-with-strong-secret
go run . -app myidsan
```

The dev config defaults to PostgreSQL database `myidsandb` on port `5433`, Redis at `localhost:6379`, and HTTP listener port `3001`.
The non-dev config starts HTTPS on port `3001`, so place certificates at `apps/myidsan/certs/cert.pem` and `apps/myidsan/certs/key.pem` or change `tls.certPath` and `tls.keyPath`.
It also sets `sso.issuer=myidsan`, `sso.audience=myidsan,mymatasan`, and a dev-only `sso.internalToken=dev-internal-token`.

Login with the bootstrapped account after first startup:

```bash
curl -c cookies.txt -H "Content-Type: application/json" \
  -d '{"username":"superadmin","password":"superadmin123"}' \
  "http://localhost:3001/api/login/default"
```

SSO fallback examples:

```bash
curl -H "Content-Type: application/json" \
  -H "X-Myidsan-Internal-Token: dev-internal-token" \
  -d '{"token":"<jwt>","audience":"mymatasan"}' \
  "http://localhost:3001/api/sso/introspect"
```

```bash
curl -H "Content-Type: application/json" \
  -H "X-Myidsan-Internal-Token: dev-internal-token" \
  -d '{"token":"<jwt>","audience":"mymatasan","host":"localhost:3000","path":"/api/camera/stream","method":"GET"}' \
  "http://localhost:3001/api/sso/authorize"
```

Use Redis for multi-app deployments so session/RBAC cache entries can be shared. Use in-memory cache only for isolated development, or call the fallback APIs above when a relying app cannot see myidsan cache state.

The MyIDSan admin UI is served from the app shell and builds its sidebar from `/api/endpoint-rbac/ep/me` plus `api_endpoint.metadata` menu entries. The same RBAC method grants control toolbar actions: `POST` enables create, `PUT` enables edit, and `DELETE` enables delete. Table filter, sort, and page position are remembered per table resource in browser cookies, and the table clear control resets that remembered state. If a refreshed session remembers a page that the current role can no longer access, the UI shows the unauthorized access page.

## Filter Shared List APIs

Shared DB-backed list endpoints accept backend filters and sorters in addition to `limit` and `offset`.

Example:

```bash
curl "http://localhost:3000/api/log?limit=25&offset=0&filters=%5B%7B%22fieldName%22%3A%22statsCode%22%2C%22compare%22%3A1%2C%22value%22%3A200%7D%5D&sorters=%5B%7B%22fieldName%22%3A%22createdAt%22%2C%22sort%22%3A2%7D%5D"
```

The JSON filter shape is `{"fieldName":"createdAt","compare":5,"value":1700000000}`. Compare values are `1` equals, `2` not equals, `3` greater than, `4` less than, `5` greater than or equal, and `6` less than or equal. The sorter shape is `{"fieldName":"createdAt","sort":2}` where `1` is ascending and `2` is descending.

`filters` and `sorters` may be a JSON object or array. Repeated `filter` and `sorter` query parameters are also accepted. Multiple filters are combined with `AND`, multiple sorters are applied in request order, and boolean filters should be omitted or cleared for the neutral state.

## Cache Admin API

Login through `myidsan` and store the session cookies. The issued token includes the `mymatasan` audience in dev config, so the same cookie can be sent to `mymatasan` on localhost:

```bash
curl -c cookies.txt -H "Content-Type: application/json" \
  -d '{"username":"superadmin","password":"superadmin123"}' \
  "http://localhost:3001/api/login/default"
```

For unsafe methods, set `CSRF_TOKEN` to the value of the `kopiv2_csrf` cookie from `cookies.txt` before sending `X-CSRF-Token`.

List cache keys by prefix:

```bash
curl -b cookies.txt "http://localhost:3000/api/cache-service?prefix=000001:&limit=20&offset=0"
```

Check cache health:

```bash
curl -b cookies.txt "http://localhost:3000/api/cache-service/health"
```

Wipe one cache key:

```bash
curl -X DELETE -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" "http://localhost:3000/api/cache-service?key=000001:1"
```

Wipe cache by prefix:

```bash
curl -X DELETE -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" "http://localhost:3000/api/cache-service?prefix=000001:"
```

Wipe all cache entries (explicit):

```bash
curl -X DELETE -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" "http://localhost:3000/api/cache-service?wipeAll=true"
```

Wipe cache with JSON payload:

```bash
curl -X POST -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" -H "Content-Type: application/json" \
  -d '{"prefix":"000001:"}' \
  "http://localhost:3000/api/cache-service/wipe"
```

Note:

- Successful cache wipe actions are written to API logs for audit tracing.

## API Log API

List API activity logs after login:

```bash
curl -b cookies.txt "http://localhost:3000/api/log?limit=50&offset=0"
```

Delete API activity logs for one month:

```bash
curl -X DELETE -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" \
  "http://localhost:3000/api/log?year=2026&month=5"
```

Current-month deletion is blocked in the API log service. Scheduled database cleanup can be enabled with `apiLog.cleanup.enabled`, `apiLog.cleanup.maxRetentionDays`, and `apiLog.cleanup.frequencyMinutes`; the frequency defaults to `60` minutes when omitted.

## Runtime Log API

List runtime service logs after login:

```bash
curl -b cookies.txt "http://localhost:3000/api/log-service?limit=50&offset=0"
```

Delete dated runtime log files for one month:

```bash
curl -X DELETE -b cookies.txt -H "X-CSRF-Token: $CSRF_TOKEN" \
  "http://localhost:3000/api/log-service?year=2026&month=6"
```

The log base path is configured by `logging.path` or `LOG_PATH`. Relative paths resolve from the selected app directory and work across Linux, Windows, and macOS. A base path such as `./logs/mymatasan.log` writes dated files like `mymatasan-2026-06-07.log`.

Current-month deletion is blocked in the runtime log service. Scheduled cleanup can be enabled with `logging.cleanup.enabled`, `logging.cleanup.maxRetentionDays`, and `logging.cleanup.frequencyMinutes`; the frequency defaults to `60` minutes when omitted.

## systemd Deployment

1. Copy binary to `/opt/kopiv2/kopiv2-server`.
2. Copy app runtime files to `/opt/kopiv2/apps/mymatasan`.
3. Copy env file to `/etc/kopiv2/kopiv2.env`.
4. Install unit file and run:

```bash
sudo systemctl daemon-reload
sudo systemctl enable kopiv2
sudo systemctl start kopiv2
sudo systemctl status kopiv2
```

## Documentation Maintenance

When changing code:

1. Update affected `docs/modules/...` file.
2. Update one of `TECHNICAL_SPEC.md`, `REQUEST_FLOW.md`, `HOWTO.md` if behavior changed.
3. Update root `README.md` for user-facing run or architecture changes.
