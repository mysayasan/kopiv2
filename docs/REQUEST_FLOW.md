# Request and Runtime Flow

## HTTP Request Path

1. Client hits server at one of the configured runtime listeners (`server.hostnames x server.tlsPorts/server.nonTlsPorts`, optionally overridden by `SERVER_HOSTNAMES`, `SERVER_TLS_PORTS`, and `SERVER_NON_TLS_PORTS`).
2. Router (`gorilla/mux`) matches route.
3. Global middleware executes:
   - greet middleware
   - CORS middleware
   - request log middleware (adds/propagates `X-Request-ID` and writes through runtime logger)
4. For `/api/*` routes:
   - API activity log middleware records the completed request into `api_log`, including elapsed `durationMs`.
   - API telemetry records request count, duration histogram, and slow-request metrics when enabled.
   - rate-limit middleware classifies the API endpoint tier (`0=DevOnly`, `1=AuthOnly`, `2=Public`) and applies config-driven sliding-window limits.
   - routes that opt into MyIDSan auth read the HttpOnly session cookie, validate the JWT, and inject claims into context.
   - unsafe JWT-authenticated methods (`POST`, `PUT`, `PATCH`, `DELETE`) must send `X-CSRF-Token` matching the readable CSRF cookie.
   - auth middleware validates signed JWT, configured issuer/audience, and cache-backed SSO session when a `sid` claim is present.
   - RBAC middleware validates resource-app scoped role access for host + path segment boundary + method when the route uses RBAC.
   - standalone `mymatasan` ONVIF and vision routes use app-local Basic Auth instead of MyIDSan JWT/RBAC.
5. Handler decodes payload, calls service, and writes response.

Shared JSON response helpers include `durationMs`, measured from request middleware start time to response serialization.

## Health and Readiness Flow

- `GET /health`: immediate alive response.
- `GET /ready`: performs DB and cache pings with timeout (`2s`), reports up/down.
- `GET /api/version`: returns the selected app SemVer and shared core SemVer from the embedded version manifest.

## Startup Flow

1. Launcher selects app module (`-app` flag or `cmd/<app>` build target), such as `mymatasan` or `myidsan`.
2. Load `.env`.
3. Resolve app config file based on `ENVIRONMENT` from the selected app directory.
4. Apply sensitive config requirements (`JWT_SECRET`, optional Google/GitHub OAuth secrets).
5. Apply DB env overrides.
6. Apply logging env overrides.
7. Apply API log cleanup env overrides.
8. Apply server env overrides (`SERVER_HOSTNAMES`, `SERVER_TLS_PORTS`, `SERVER_NON_TLS_PORTS`, plus legacy `SERVER_ADDR`, `SERVER_PORTS`, `SERVER_ENABLE_TLS`, `SERVER_ENABLE_NON_TLS`).
9. Initialize runtime logger and route standard library logs through it.
10. Run shared bootstrap engine with registered entity types.
11. If bootstrap is enabled, create missing database/schema and update the manifest state table.
12. Build router and middleware chain.
13. Expose setup status page and JSON endpoint at the configured setup path.
14. Initialize DB, cache, transaction lock coordinator, repositories, embedded version manifest, telemetry recorder, enabled shared API modules, selected app routes, and the shared scheduler for built-in or app-specific jobs.
15. Register the durable file-storage upload job repository and start the backend upload worker when `transaction.jobWorkerEnabled=true`.
16. Register Swagger/OpenAPI routes (`/swagger`, `/swagger/openapi.json`) from the shared docs module.
17. Start app workers when the selected app registers any.
18. Start one or more listeners based on host and explicit TLS/non-TLS port lists.

Bootstrap seeding also ensures a default `system` group and `superadmin` role exist before the app becomes ready.
The default `superadmin` login password is inserted as a bcrypt hash; legacy plain-text passwords still migrate after successful local login.
It also seeds wildcard-host endpoint rows with `accessTier` metadata and RBAC rows for the protected API modules so the default access map is ready on a fresh install. Protected shared management APIs seed as `DevOnly`.

`myidsan` uses this same bootstrap flow to seed the identity-provider management surface, app registry, SSO fallback endpoints, and selected relying-app policies. It is the cross-app sign-on and RBAC authority. `mymatasan` is standalone: it seeds only local endpoint metadata for rate-limit classification and app bootstrap, mounts public version plus app-local ONVIF and vision routes, and protects those app-local routes with Basic Auth from local users.

## Browser SSO Callback Flow

1. A browser opens a relying app such as `myseliasan`.
2. Without a local relying-app session, the app redirects to MyIDSan `/api/auth/authorize` with `client_id`, `audience`, exact `redirect_uri`, and state.
3. MyIDSan validates the client config and redirect URI allow-list. If the browser has no MyIDSan session, it serves `/api/auth/login` and resumes authorization after local credential login.
4. MyIDSan redirects the browser back to the relying-app callback with a short-lived one-time code and state.
5. The relying app validates state, then performs a backend HTTPS `POST` to MyIDSan `/api/auth/token` with its client secret.
6. For HTTPS token exchange, the relying app uses the OS trust store plus optional `sso.caCertPath`/`SSO_CA_CERT_PATH`. This trusts private CA bundles without disabling hostname, expiry, or chain validation.
7. After a valid token response, the relying app issues its own HttpOnly session cookie and redirects the browser to its app root.

## Bootstrap Flow

The shared bootstrap engine is called before the DB adapter is used by the rest of the app.

It performs:

1. maintenance DB check
2. target DB creation when allowed
3. schema table creation from registered entity structs
4. additive migration for missing columns when allowed
5. unique index reconciliation from `ukey` tags
6. manifest hash persistence in `bootstrap_schema_state`
7. optional config-driven SQL seed execution when enabled

## Shutdown Flow

1. Wait for `SIGINT` or `SIGTERM`.
2. Create shutdown context (`10s`).
3. Stop any selected-app workers via `Shutdown(ctx)` when one is registered.
4. Shutdown HTTP server gracefully.

## ONVIF To RTSP Setup Flow

1. `POST /api/onvif/discover` sends WS-Discovery probes on the local network, upserts matching ONVIF devices by XAddr, and returns them enriched with best-effort unauthenticated device information, capabilities, stream URI, and snapshot URI fields when the camera exposes them.
2. `POST /api/onvif/probe` checks one manually entered host or device-service URL.
3. `POST /api/onvif/devices/discovered` saves or updates the device record by ONVIF XAddr.
4. `POST /api/onvif/devices/{id}/stream-options` calls ONVIF `GetCapabilities`, `GetProfiles`, and `GetStreamUri` for every media profile so the UI can show stream1/stream2 style choices.
5. `POST /api/onvif/devices/{id}/stream-uri` saves the preferred profile or the selected `profileToken` as the camera RTSP URI, probes it immediately, and persists a working same-host VIGI-style `/stream1` or `/stream2` fallback when the ONVIF URL itself returns 406.
6. `POST /api/onvif/devices/{id}/rtsp-test` uses `infra/rtsp` to DESCRIBE/SETUP the RTSP URI and save observed transport and track metadata. If the saved URL fails, the service may try a same-host VIGI-style `/stream1` or `/stream2` path derived from the selected profile and save the working candidate.
7. `POST /api/onvif/devices/{id}/live-view` resolves ONVIF `GetSnapshotUri` for the saved media profile and keeps the selected RTSP profile intact.
8. `GET /api/onvif/devices/{id}/live.mjpeg` emits a browser-friendly multipart MJPEG stream. Browser fallback passes `preferSnapshot=1`, so the endpoint tries ONVIF snapshot frames first and falls back to RTSP-to-MJPEG conversion when snapshots are not available.
9. Browser live view uses WebRTC only when the saved RTSP track metadata includes H264 video. If the selected stream exposes video tracks without H264, such as an H265/HEVC main stream, the frontend skips WebRTC and uses MJPEG fallback when enabled.

## Vision Detection Flow

1. The operator opens the AI page and selects a saved camera from the left navigation.
2. The page lists that camera's detection rules and opens a live-preview drawing view when the operator creates or edits a rule.
3. The frontend saves each rule through `POST /api/vision/rules`, including camera ID, detection type, normalized polygon points, optional `ruleConfig` for line definitions, threshold, minimum frame count, cooldown, alert sound setting, enabled state, and optional rule-level `schedulePolicy`.
4. `schedulePolicy` is evaluated per rule. Empty policy means always active; weekly windows and RFC3339 date ranges can either allow detection only inside matches or deny detection during matches.
5. The MyMataSan vision monitor runs as an app worker when `vision.enabled` is true, loads enabled rules, filters out rules whose schedule is inactive, groups active rules by camera, and captures a JPEG frame from the saved RTSP URI or snapshot URI.
6. The configured reusable `infra/vision` detector runs in `motion`, `external`, `hybrid`, or `persistent` mode. Motion mode compares consecutive frames inside each rule polygon. External object mode maps model candidates to rule detection types and polygons. Hybrid mode can use external object detection for semantic rules while routing configured rule types such as `intrusion` to motion. Persistent mode keeps a worker such as `yolo_worker.py` alive, sends each JPEG frame as newline-delimited JSON, and receives normalized object candidates without reloading the model per frame.
7. A detection is raised only when type/class matching, threshold, polygon or line-crossing geometry, minimum frame count, sequence state, and cooldown requirements are satisfied.
8. Detection results are persisted as `alert_event` rows through `POST /api/vision/alerts` service logic. Diagnostic alert rows are throttled and written when capture or detection fails, or when frames are sampled without crossing the rule threshold.
9. The AI alert table and live-view camera tiles read alert events so operators can see which monitored camera has recent activity. Operators can acknowledge handled alerts through `POST /api/vision/alerts/{id}/ack`.

## File Storage Upload Transaction Flow

1. Upload API validates multipart files and supported content types.
2. Upload API parses batch-level `securityLvl` and optional expiry from the multipart form. Expiry can be absolute `expiredAt` or countdown `expiresIn` plus `expiresInUnit`.
3. Each accepted file is streamed once into the file-storage staging directory while computing its checksum.
4. If any file fails validation or staging, staged files are removed and no database write is attempted.
5. Synchronous upload calls the file storage service directly; async upload creates an `operation_job` row and returns job status to the caller.
6. The backend upload worker recovers stale `running` jobs, then processes queued or retrying jobs in FIFO order.
7. File storage service acquires the FIFO transaction lock for the `file-storage` resource.
8. The service opens a request-scoped DB transaction.
9. For each staged file, metadata is inserted and the staged file is copied into its final GUID path through an atomic final-path swap.
10. On success, the DB transaction commits, staging files are removed, and the lock is released.
11. On insert, copy, timeout, or commit failure, the DB transaction rolls back, final files created by the attempt are removed, and the lock is released.
12. Sync request failures clean staged files immediately. Async job failures keep staged files for retry until `maxAttempts` is exhausted, then clean staging/final paths.
13. Lock wait timeout, cancellation, acquisition, and stuck lock observations are exported through telemetry.

## File Storage Download and Expiry Flow

1. Download requests use metadata IDs only: `id` for one file or comma-separated `ids` for ZIP output.
2. The route itself is public so `Public` files can be retrieved without login.
3. When auth cookies are present, the API passes the caller user and role to the service as a download actor.
4. The service rejects expired files before reading the physical file.
5. `SystemOnly` files require an internal service actor, `Group` requires matching owner/actor role group, `Role` allows the owner role or its ancestors, and `Public` allows any caller.
6. Single-file responses use attachment disposition by default; `view=true` changes the response to inline disposition so browsers can render supported images, PDFs, and text.
7. ZIP downloads always use attachment disposition.
8. The expiry scheduler runs every `fileStorage.cleanup.frequencySeconds`, lists up to `fileStorage.cleanup.batchSize` files where `expiredAt <= now`, removes the physical GUID file, then deletes metadata.
