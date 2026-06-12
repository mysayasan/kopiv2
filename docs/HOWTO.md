# How-To Guide

## Local Development

1. Provide required environment variables.
2. Ensure the selected DB engine is reachable; SQLite only needs a writable `DB_NAME` path.
3. `apps/mymatasan/config.dev.json` defaults to SQLite at `./data/mymatasan.db`.
4. Use `CACHE_PROVIDER=default` for local in-process memory cache; ensure Redis is reachable only when an app is configured with `CACHE_PROVIDER=redis`.
5. Run:

```bash
go run . -app mymatasan
```

Run the identity app instead:

```bash
go run . -app myidsan
```

Run the MySeliaSan control app after MyIDSan is running:

```bash
export ENVIRONMENT=dev
export JWT_SECRET=replace-with-strong-secret
go run . -app myseliasan
```

Open `https://localhost:3002`. The root page redirects to MyIDSan when no MySeliaSan session exists. Dev config expects MyIDSan at `https://localhost:3001`, client ID `myseliasan`, client secret `dev-myseliasan-secret`, and callback `https://localhost:3002/api/auth/callback`.

For local HTTPS, replace `apps/myidsan/certs/cert.pem`, `apps/myidsan/certs/key.pem`, `apps/myseliasan/certs/cert.pem`, and `apps/myseliasan/certs/key.pem` with certificates signed by a CA trusted by the machine running MySeliaSan. The browser redirect is not enough; MySeliaSan also performs a backend HTTPS call to MyIDSan during `/api/auth/callback`.

If that CA is not installed in the OS trust store, configure MySeliaSan `sso.caCertPath` with a PEM CA bundle path. The default dev value points at `../myidsan/certs/cert.pem`; when using a private CA, replace it with the CA PEM path or override it with `SSO_CA_CERT_PATH`.
This is intentionally stricter than `insecureSkipVerify`: hostname, expiry, and certificate-chain validation still run. If the callback returns `403 limited access` outside dev mode, check MySeliaSan logs for the hidden token-exchange error; expired or wrong-host certificates commonly surface there.

Or with make:

```bash
make run APP=mymatasan
```

```bash
make run APP=myidsan
```

```bash
make run APP=myseliasan
```

Build only one app binary:

```bash
make build APP=mymatasan
```

```bash
make build APP=myidsan
```

```bash
make build APP=myseliasan
```

`mymatasan` defaults to SQLite and the in-process default cache for small single-process runs:

```bash
export ENVIRONMENT=dev
go run . -app mymatasan
```

For SQLite, `DB_NAME` is the database file path. Relative paths resolve from the selected app directory, so the default writes under `apps/mymatasan/data/`.

Default local `mymatasan` credentials are seeded into SQLite on first startup:

```text
admin / Admin123
```

Manage local users from the Settings page. Passwords are stored as bcrypt hashes, and `mymatasan` prevents deleting or disabling the last active admin user.

MJPEG fallback conversion and RTSP-based vision frame capture use the runtime Decoder settings. `config.json` provides the startup defaults, and the Settings page stores live changes in SQLite. The default `decoder.mjpeg.ffmpegPath` value can be `ffmpeg`, which resolves from the process `PATH`; set an absolute path such as `C:\ffmpeg\bin\ffmpeg.exe` or `/usr/bin/ffmpeg` when running as a service.

Decoder tuning starts safely with CPU software decode:

```json
"decoder": {
  "mjpeg": {
    "ffmpegPath": "ffmpeg",
    "quality": 7,
    "threads": 1
  },
  "ffmpeg": {
    "rtspTransport": "tcp",
    "hwaccel": "none",
    "hwaccelDevice": "",
    "initHwDevice": "",
    "videoDecoder": "",
    "probeSize": 1000000,
    "analyzeDuration": 1000000,
    "lowDelay": true,
    "noBuffer": true
  }
}
```

Use `hwaccel: "none"` unless the installed ffmpeg build and device drivers are known to support the target accelerator. Common hardware modes include `vaapi` on Linux GPU stacks, `cuda`/NVDEC on NVIDIA builds, `qsv` for Intel Quick Sync, `d3d11va` or `dxva2` on Windows, and `videotoolbox` on macOS. Hardware decode can reduce CPU load, but MJPEG fallback still filters/scales/encodes frames, so some systems may not improve if ffmpeg has to copy frames back from the GPU. Keep `threads` low on Raspberry Pi or Jetson-style devices, raise `probeSize` or `analyzeDuration` only when a camera stream is slow to identify, and disable `lowDelay` or `noBuffer` only if a camera becomes unstable with low-latency flags.

The Settings page Decoder panel includes an Auto Tune action. It inspects saved camera RTSP track metadata and the local `ffmpeg -hwaccels` output, then saves a conservative profile. It keeps `hwaccel: "none"` when ffmpeg is missing, camera RTSP metadata is absent, or the platform hardware decoder cannot be safely verified. Run each camera's RTSP Test first so auto-tune can see whether streams are H264, H265/HEVC, or another codec.

Discover ONVIF devices:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"timeoutMs":3000}' \
  "http://localhost:3000/api/onvif/discover"
```

Create a MyMataSan AI detection rule for a saved camera:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Porch person after hours","detectionType":"person","zonePolygon":"[[0.1,0.1],[0.9,0.1],[0.9,0.8],[0.1,0.8]]","schedulePolicy":"{\"preset\":\"custom\",\"timezone\":\"Asia/Kuala_Lumpur\",\"mode\":\"allow\",\"windows\":[{\"days\":[\"mon\",\"tue\",\"wed\",\"thu\",\"fri\"],\"start\":\"18:00\",\"end\":\"07:00\"}]}","threshold":0.35,"minFrames":2,"cooldownSeconds":30,"soundEnabled":true,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

Review alert events raised by the monitor:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/vision/alerts?limit=50&offset=0"
```

Configure semantic detector routing with `vision.detector`. `motion` mode is dependency-free. `external` mode starts one detector command per sampled frame. `hybrid` mode combines external object detection with motion fallback for configured rule types such as intrusion. `persistent` mode keeps one detector worker process alive and is the recommended YOLO path.

```json
"vision": {
  "enabled": true,
  "intervalMs": 2000,
  "captureTimeoutMs": 12000,
  "diagnosticCooldownSeconds": 30,
  "detector": {
    "mode": "persistent",
    "command": "python",
    "args": ["./apps/mymatasan/ai/yolo_worker.py"],
    "timeoutMs": 8000,
    "useMotionFallback": true,
    "useMotionIntrusion": true,
    "minObjectConfidence": 0.25,
    "classMap": {
      "fire": ["fire"],
      "smoke": ["smoke"],
      "person": ["person"],
      "vehicle": ["vehicle", "car", "truck", "bus", "motorcycle", "bicycle"],
      "animal": ["animal", "bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra", "giraffe", "mouse", "rat"],
      "intrusion": ["person", "vehicle", "car", "truck", "bus", "motorcycle", "bicycle"],
      "line_crossing": ["person", "vehicle", "car", "truck", "bus", "motorcycle", "bicycle"],
      "multi_line_crossing": ["person", "vehicle", "car", "truck", "bus", "motorcycle", "bicycle"]
    }
  }
}
```

Detector output can be a JSON array or an object with `detections` or `objects`:

```json
[
  {"label":"person","confidence":0.91,"box":{"x":0.2,"y":0.1,"w":0.3,"h":0.5}}
]
```

Install the bundled YOLO worker dependencies in the Python environment used by MyMataSan:

```bash
python -m pip install -r apps/mymatasan/ai/requirements-yolo.txt
```

The default `yolo11n.pt` model detects COCO classes such as `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`. Use `MYMATASAN_YOLO_MODEL=/path/to/fire-smoke.pt` for fire/smoke classes, or a custom animal model for labels such as `mouse` and `rat`. For CCTV or IR scenes, start person, vehicle, and animal rules around `threshold: 0.35` and `minFrames: 2`, then tune upward if false positives are too noisy.

Create a line-crossing sequence rule. Lines are normalized from `0` to `1`, and `multi_line_crossing` requires the same tracked object to cross each line in the configured order:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Entry sequence","detectionType":"multi_line_crossing","zonePolygon":"[[0,0],[1,0],[1,1],[0,1]]","ruleConfig":"{\"classes\":[\"person\",\"car\"],\"direction\":\"both\",\"maxSecondsBetweenLines\":20,\"lines\":[{\"id\":\"start\",\"points\":[[0.35,0.2],[0.35,0.8]]},{\"id\":\"end\",\"points\":[[0.65,0.2],[0.65,0.8]]}]}","threshold":0.55,"minFrames":1,"cooldownSeconds":10,"soundEnabled":true,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
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

Run standalone MyMataSan with SQLite/default cache:

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
https://localhost:3001/swagger
```

## Identity App

`myidsan` runs its own login, user, group, and role APIs, plus app-registry, endpoint, endpoint-RBAC, cache, log, runtime-log, file-storage, and version APIs as an identity-management app.

Start it locally:

```bash
export ENVIRONMENT=dev
export JWT_SECRET=replace-with-strong-secret
go run . -app myidsan
```

The dev config defaults to PostgreSQL database `myidsandb` on port `5433`, Redis at `localhost:6379`, and HTTPS listener port `3001`.
Both dev and non-dev configs expect certificates at `apps/myidsan/certs/cert.pem` and `apps/myidsan/certs/key.pem`, unless you change `tls.certPath` and `tls.keyPath`.
It also sets `sso.issuer=myidsan`, `sso.audience=myidsan,mymatasan`, and a dev-only `sso.internalToken=dev-internal-token`.

Login with the bootstrapped account after first startup:

```bash
curl -c cookies.txt -H "Content-Type: application/json" \
  -d '{"username":"superadmin","password":"superadmin123"}' \
  "https://localhost:3001/api/login/default"
```

SSO fallback examples:

```bash
curl -H "Content-Type: application/json" \
  -H "X-Myidsan-Internal-Token: dev-internal-token" \
  -d '{"token":"<jwt>","audience":"mymatasan"}' \
  "https://localhost:3001/api/sso/introspect"
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

Login through `myidsan` and store the session cookies for apps that consume MyIDSan SSO and mount the shared cache-service API. `mymatasan` no longer consumes MyIDSan cookies or mounts cache-service; use its local Basic Auth credentials for ONVIF APIs.

```bash
curl -c cookies.txt -H "Content-Type: application/json" \
  -d '{"username":"superadmin","password":"superadmin123"}' \
  "https://localhost:3001/api/login/default"
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
