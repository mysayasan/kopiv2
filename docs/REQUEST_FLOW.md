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
   - auth middleware reads the HttpOnly session cookie, validates the JWT, and injects claims into context.
   - unsafe authenticated methods (`POST`, `PUT`, `PATCH`, `DELETE`) must send `X-CSRF-Token` matching the readable CSRF cookie.
   - RBAC middleware validates role access for host + path segment boundary + method.
5. Handler decodes payload, calls service, and writes response.

Shared JSON response helpers include `durationMs`, measured from request middleware start time to response serialization.

## Health and Readiness Flow

- `GET /health`: immediate alive response.
- `GET /ready`: performs DB and cache pings with timeout (`2s`), reports up/down.
- `GET /api/version`: returns the selected app SemVer and shared core SemVer from the embedded version manifest.

## Startup Flow

1. Launcher selects app module (`-app` flag or `cmd/<app>` build target).
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
14. Initialize DB, cache, repositories, embedded version manifest, telemetry recorder, shared API modules, selected app routes, and the shared scheduler for built-in or app-specific jobs.
15. Register Swagger/OpenAPI routes (`/swagger`, `/swagger/openapi.json`) from the shared docs module.
16. Start app workers (for example camera autostart).
17. Start one or more listeners based on host and explicit TLS/non-TLS port lists.

Bootstrap seeding also ensures a default `system` group and `superadmin` role exist before the app becomes ready.
The default `superadmin` login password is inserted as a bcrypt hash; legacy plain-text passwords still migrate after successful local login.
It also seeds wildcard-host RBAC endpoint rows for the protected API modules so the default access map is ready on a fresh install.

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
3. Stop camera stream workers via `Shutdown(ctx)`.
4. Shutdown HTTP server gracefully.

## Camera Stream Worker Flow

1. `ReadMjpeg(id)` checks active worker map.
2. If no worker, load camera source and create worker.
3. Worker loop calls ffmpeg netcam `ReadMjpeg(uri)`.
4. Frames are parsed from byte stream and pushed to buffered channel.
5. On transient EOF, sends fallback frame (`nosignal.gif`) and retries up to threshold.
6. On cancellation, worker exits and channel closes.
