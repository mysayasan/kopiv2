# How-To Guide

## Local Development

1. Provide required environment variables.
2. Ensure PostgreSQL is reachable.
3. Use `CACHE_PROVIDER=default` for local in-process memory cache; ensure Redis is reachable only when `CACHE_PROVIDER=redis`.
3. Run:

```bash
go run . -app mymatasan
```

Or with make:

```bash
make run APP=mymatasan
```

Build only one app binary:

```bash
make build APP=mymatasan
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

## Cache Admin API

Login once and store the session cookies:

```bash
curl -c cookies.txt -H "Content-Type: application/json" \
  -d '{"username":"superadmin","password":"superadmin123"}' \
  "http://localhost:3000/api/login/default"
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
