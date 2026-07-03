# Technical Specification

## Scope

`kopiv2` is a Linux-first, lightweight Go backend that provides:

- HTTP API with cookie-backed JWT auth and RBAC enforcement.
- Standalone ONVIF device discovery and RTSP stream probing for camera setup.
- Reusable visual detection primitives for camera rules, rule-level schedules, motion detection, external object detection, persistent object workers, line crossing, multi-line crossing, crowd detection, license-plate recognition (LPR), hybrid dispatch, and alert events.
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
- **Shared frontend module** (`frontend/shared/`): plain ESM, no build step; consumed by both myidsan and myseliasan via a `@shared` webpack alias + `resolve.modules` entry in each app's `webpack.config.js`. Exports: `DataTable`, `Toast`/`ToastStack`, `SideNav` (brand/footer slots + bespoke item injection via a `render` hook — myseliasan injects its Nodes tree), and `icons` (`Ico` + `icoSvg` union). Theming via `--ui-*` design tokens each app maps to its own palette. Per-app copies (`data_table.js`, `icons.js`) have been deleted from both apps. Three themes are supported in both SPAs: Light, Dark, and **High contrast** (black surfaces, white text, bright accents, strong borders). Side-nav colors follow the active theme via `--nav-*` tokens.
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
- OAuth login providers (Google/GitHub) are optional and do not disable local credential auth when not configured. A provider block whose `client_id` or `client_secret` is blank (after config + env) is **disabled with a warning** rather than blocking startup. The federated login page (`GET /api/auth/login`) renders Google/GitHub buttons only for providers that are currently active; the SPA checks `GET /api/login/providers` for the same signal.
- Local credential passwords are stored as bcrypt hashes, with lazy migration from legacy plain-text values on successful login.
- Authorization: shared "accessrbac" RBAC core — single-app, no `app_code` dimension. Each app (myidsan, myseliasan) has its own `access_role` and `access_role_permission` tables in its own database. Built-in roles `superadmin` (matrix bypass) and `viewer` (no default permissions) are seeded on startup. The legacy `GET /api` read-everything wildcard previously seeded for viewer is **stripped** on startup by `EnsureViewerDefaults`; viewer now starts with no permissions. The permission matrix is a prefix-match table: each row grants a role access to an endpoint path prefix per HTTP verb; longest prefix wins; no match = deny. Menu visibility in both SPAs is derived from the same matrix via `GET /api/access-rbac/me` (auth-only; superadmin gets `isSuperadmin: true` / wildcard; others get their permission rows; `pending: true` when no role is assigned). Role is **dynamic**: `AccessSessionMidware` re-stamps `claims.RoleId` from the live user store on every request so a role change takes effect immediately without a re-login, and the session role-mismatch check is removed from `validateSession`. `IsSuperadmin` also resolves live. A new `RequireSuperadmin` middleware locks a subrouter to superadmins regardless of matrix grants; it is applied to `/api/user-credential`, `/api/user-group`, and `/api/rbac/users` in myidsan/myseliasan. `ListForRole` (permissions + node grants) sorts by path ASC for stable RBAC matrix UI updates. All shared admin APIs and app-local admin APIs are enforced by `AccessSessionMidware`. **Critical bug fix**: `SelectByUnique` in both the SQLite and PostgreSQL adapters now fails closed when the key group matches no struct field — previously it issued an unfiltered `LIMIT 1` query, silently returning the first row. Five callers that used `GetByUnique(ctx,"","id",id)` (matching no field) have been switched to `GetById` (primary key); `genericRepo.GetById` and `GetByUnique` now also return `(nil, nil)` when the underlying query returns a nil map, so every `x == nil` not-found check in auth/RBAC is correct. A regression test was added to `db_crud_test.go`.
- `myidsan` is the SSO identity provider for cross-app browser login. Tokens carry issuer (`iss`), audience (`aud`), expiry, session id (`sid`), resource app code, and policy version. Redis should be used for shared session cache in multi-process deployments. `myidsan` no longer issues RBAC decisions to relying apps; each app enforces its own accessrbac matrix locally.
- `mymatasan` is a standalone device app and does not mount MyIDSan login, SSO browser callback, user/group, accessrbac, app-registry, endpoint-RBAC, file-storage, log, runtime-log, or cache-service management APIs. App-local ONVIF and vision routes use DB-backed local Basic Auth (with a session cookie for media elements). Saved-camera browser live view uses configurable RTSP-to-WebRTC H264 forwarding first, with MJPEG fallback retained for compatibility. WebRTC sessions forward G.711 A-law (PCMA) and µ-law (PCMU) audio tracks unchanged (browsers decode both natively); for cameras whose audio is **not** G.711 (e.g. AAC) a dedicated ffmpeg leg transcodes the audio to **Opus** so live view still has sound. When WebRTC is disabled, the frontend uses MJPEG directly (video only).
- `mymatasan` local-auth hardening (appliance posture, independent of MyIDSan RBAC): the seeded `admin`/`Admin123` is forced through a password change on first login (`must_change_password`; gated to the auth-session + change-password routes until cleared, or provisioned via `LOCAL_ADMIN_PASSWORD`); failed logins are rate-limited per source IP with escalating backoff (`loginSecurity` config; `429` + `Retry-After`); a role split gives admins full control while non-admin local users are view-only + acknowledge (mutations outside a small viewer allow-list return `403`, enforced by a write-authorization middleware that is secure by default). A wrong Basic credential is authoritative — it returns `401` and never rides a stale session cookie. Media (recordings, snapshots/alert images, training images) is **encrypted at rest** by default (AES-256-GCM, reusable `infra/atrest` module; `security.encryptAtRest`/`keyPath`) so the factory reset can **crypto-erase** it by destroying the key. The on-disk master key (DEK) is itself protected by a `KeyProtector` (`security.keyProtector`): the plaintext `file` protector (default/backward-compatible), an OS-native keystore (`dpapi` on Windows, `systemd-creds` on a systemd Linux host, both machine-scoped/host-bound and selected automatically by `"auto"`), or a portable Argon2id-derived `passphrase` protector (the right fit for Docker, sourced from `security.passphrase`/`passphraseFile`/`passphraseEnv`). Switching protectors re-wraps the same DEK, so existing ciphertext always stays readable. Because host-bound protectors cannot be unwrapped off-box, an admin can export a passphrase-protected **recovery escrow** of the key (`POST /api/system/recovery/export`, Settings → Backup & Recovery) and later verify it (`POST /api/system/recovery/verify`); a non-secret **init marker** written beside the key distinguishes "key missing because this is a new install" from "key missing but data encrypted with it still exists" — the latter case never silently mints a replacement key. If the key is missing but a marker shows one existed, the app boots into a public pre-login **recovery gate** (`GET/POST /api/system/recovery/gate`, `/unlock`) where the escrow file + passphrase restore access, or restores automatically with no prompt when `security.recoveryPath` + a passphrase are configured. A **Secure Wipe & Reset** (factory reset: crypto-erase key → erase media → drop/rebuild DB → TRIM + free-space scrub → restart) is available only when `bootstrap.allowReset` is enabled. `mymatasan` also accepts tunneled commands from the `myseliasan` control plane over the control channel (parent→node reverse tunnel); these are re-injected into the node's own API router with a synthetic principal so the node's existing auth stack enforces viewer/admin without extra auth code.
- `myseliasan` is a relying control-plane app and has no public landing page. It redirects unauthenticated users to MyIDSan and creates its own local session only after MyIDSan returns a valid authorization code. It maintains its own `ControlUser` store (local stock superadmin + federated myidsan users), with roles governed by the shared accessrbac core. It communicates with `mymatasan` nodes over LAN using the pairing protocol (see below) and the reverse command tunnel (see below); it does not relay through `myidsan` for either pairing or authorization.
- **LAN discovery + single-parent adoption + mTLS hardening (pairing protocol)**: `mymatasan` nodes and the `myseliasan` control plane share a fleet key (PSK, operator-set). Discovery uses authenticated UDP multicast (HMAC-SHA256 signed probes + announces, nonce + timestamp replay protection). A node is discoverable only when it is unpaired and a fleet key is set; it goes silent once adopted. Adoption is an HTTPS call carrying a fleet-key assertion (proves key possession without transmitting it) and a short-lived operator-generated claim code; the node issues a single-use pairing token and stores only its hash. The control plane stores the token for later enrollment calls. After adoption the node immediately generates an ECDSA P-256 key + CSR locally (the private key never leaves the node) and POSTs the CSR to the control plane's `POST /api/nodes/enroll`, which is authenticated by the pairing token; the control plane's on-prem fleet CA (ECDSA P-256, 10-year self-signed root, persisted in `ControlSetting`) signs the CSR, setting the CN to the node ID authoritatively (ignoring the CSR subject). The node parses `result.nodeCert` from the enrollment response (with `data.result.nodeCert` as a legacy fallback) — prior to this fix, the cert was silently empty on the standard envelope and the control channel never connected. The node stores the issued cert + CA root (AES-256-GCM encrypted at rest); the control plane records `CertExpiresAt`. Ongoing management runs over mutual TLS: the node serves a dedicated mTLS management listener (default port `pairing.mtlsPort`, default 49532) presenting its node cert, requiring a client cert signed by the fleet CA. The control plane probes it periodically (`GET /heartbeat`, configurable interval via `pairing.heartbeatIntervalSeconds`, default 60 s) as a **fallback** liveness mechanism — the authoritative signal is whether the node holds a live control-channel connection (`ControlServer.IsConnected`); the mTLS poll can no longer flap a control-connected node offline. A node is declared `lost` only after a grace window (3× heartbeat interval, floor 90 s) with no contact on either path. Release over mTLS (`POST /release`) is preferred over the token leg; on `Release`, the control plane first revokes the node's cert (refuses future renewals) then deletes its registry row. Nodes renew certs automatically via the same CSR flow before `pairing.renewBeforeHours` (default 48h) of expiry. Revocation is "refuse to renew + short TTL" — no CRL/OCSP. Identity in the mTLS channel is carried in the cert CN (SAN-free); both sides verify CA chain + CN rather than hostname, because appliances are dialed by IP. Both node admin self-drop and control-plane release are supported; a best-effort fleet-key-signed notice keeps the registry consistent on self-drop. The `ManagedNode.Fingerprint` field is reserved but not used; identity verification uses CN, not a pinned fingerprint. In addition to the periodic heartbeat channel, the node dials a persistent bi-directional control channel to the parent's `pairing.controlPort` (default 49533) over fleet-CA mTLS; this channel carries parent→node tunneled commands and node→parent event pushes (notifications, going-offline). A second dedicated media channel (`pairing.mediaPort`, default 49534) carries binary camera RTP frames for node-camera live view relay (see below).
- **Node camera WebRTC media relay**: the node dials the parent's `pairing.mediaPort` (default 49534) with a dedicated `MediaChannelManager`. On a browser `POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`, myseliasan's `MediaRelayHub` asks the node to stream the camera RTP, waits for codec metadata (`FrameMeta`), then creates a WebRTC peer (`stream.Manager` via `relayConnector`) with the same H264+audio track path used for local cameras. A shared `stream.WebRTCEngine` (from `nodeStream.publicIps`/`nodeStream.udpPort`) advertises the parent's external IP and binds a fixed UDP port for cross-network browser connections. STUN/TURN (`nodeStream.iceServers`) is offered to the browser for the parent↔browser leg. `GET /api/node-stream/config` returns the current ICE server list. For same-LAN deployments no `nodeStream` config is needed. The `ParentBaseURL` the node dials for control and media channels defaults to `sso.redirectBaseUrl` but is overridden by `pairing.parentBaseUrl` in config when node and parent are on separate machines.
- **myseliasan federated identity security**: `UpsertFederated` looks up existing accounts by `ssoUserId` only. The email-fallback path was removed — `myidsan` can emit a non-unique placeholder email (`"admin"`) for multiple accounts, and matching on it would allow a new SSO identity to inherit the role of an existing (potentially privileged) account (account takeover / privilege escalation). A stable, positive `ssoUserId` is now mandatory; a missing or zero id is rejected with an error. New federated users are provisioned with **no role** (`RoleId = 0`) instead of the `viewer` role; they see an "access pending" screen until a superadmin assigns a role.
- Registered apps are stored in `app_registry`; endpoint tier metadata is scoped by `api_endpoint.appCode` for rate limiting.
- Per-client SSO policy is stored in `app_auth_config`, and exact callback allow-list entries are stored in `app_redirect_uri`.
- Browser cross-app login uses MyIDSan `GET /api/auth/authorize`, `GET|POST /api/auth/login`, and `POST /api/auth/token`.
- `POST /api/sso/introspect` validates token/session state for service-to-service fallback (token validity check only; RBAC decisions are local).
- API endpoint metadata includes `accessTier` (`0=DevOnly`, `1=AuthOnly`, `2=Public`) for route classification. The tier does not replace auth/RBAC; `DevOnly` endpoints still require authorization when registered behind protected handlers.
- Browser-readable MyIDSan UI cookies are limited to presentation state such as the active page and table filters, sorters, and page position. They are not authentication or authorization material; identity remains in the HttpOnly JWT cookie and server-side accessrbac checks.
- **Reverse command tunnel (parent→node control channel)**: myseliasan's `ControlServer` accepts node-initiated WebSocket-over-fleet-mTLS connections on a dedicated port (default `49533`). `/api/nodes/{id}/proxy/<node-path>` on myseliasan forwards a tunneled `control.Request` to the connected node; the node's `ControlDispatcher` re-injects it into the node's own API router with a synthetic principal (role from the parent's per-node access grant). Node-pushed event frames (kind `"notification"`, `"going-offline"`) are ingested into the control plane's unified notification feed. Per-node read/write grants are managed via `GET/POST/DELETE /api/nodes/access` (node owner or superadmin). `GET /api/nodes/access?roleId=ID` returns a role's grants across all nodes for the central RBAC node-access matrix (superadmin-only). Superadmin roles implicitly have full access to every node (no explicit grant needed). The proxy, media, and access-grant APIs now resolve the caller's **live role** via `AccessSessionMidware.CurrentPrincipal` on every request so a just-demoted operator cannot access nodes with a stale superadmin token.
- **myseliasan self-RBAC**: myseliasan owns its own user store (`control_user` table: local stock superadmin + federated myidsan users) and uses the shared accessrbac core for its own admin endpoints. On first startup, a local stock superadmin is seeded (credentials from `localAuth.username`/`localAuth.password` in config, defaults `admin`/`admin`; must change password on first login). The local login handler ensures the issued JWT carries a non-empty `Email` claim (falls back to `username` when the account has no real email) so the shared auth middleware's empty-email check does not reject the session. After a real superadmin is elevated (`POST /api/rbac/users/{id}/elevate`), the stock account should be manually disabled. Role and permission-matrix management is the shared `/api/access-rbac` surface; user management and handoff are at `/api/rbac/users/*` (superadmin-only). `POST /api/rbac/users/{id}/role` accepts `roleId: 0` to revoke a role (pending state); superadmin self-role changes and self-elevation are rejected. `GET /api/session/me` resolves `roleId`, `roleName`, `isSuperadmin`, and `permissions` live from the user store and includes `pending: true` when the user has no role assigned. The RBAC page in the SPA includes a central **Node Access** matrix for assigning per-role node access.
- API rate limiting uses a sliding-window counter per endpoint access tier. Redis-backed cache shares counters across instances; in-memory cache is process-local.
- Secrets:
  - `JWT_SECRET` required. On startup an empty, too-short (<16 char), or known-placeholder `jwt.secret` is auto-replaced with a generated 32-byte CSPRNG secret written back into the config file (preserving formatting), unless `JWT_SECRET` is set in the environment.
  - `GOOGLE_CLIENT_SECRET` can be used to supply the Google OAuth client secret when the `login.google` block is present in config; if both the config value and this env var are empty, the Google provider is disabled with a warning (not a fatal error).
  - `GITHUB_CLIENT_SECRET` can be used to supply the GitHub OAuth client secret when the `login.github` block is present in config; same tolerate-and-disable behavior as Google.
- OAuth redirect state is generated per login request and validated against an HTTP-only state cookie on callback.

## Cache Model

- Cache abstraction is runtime-selected via configuration (`default`, `redis`, `inmemory`, or `memory`).
- Primary shared cache backend for multi-instance deployments: Redis.
- `default`, `inmemory`, and `memory` all select the local in-process memory cache.
- SSO sessions are cached as `sso:session:<sid>`.
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
- `myidsan` seeds a minimal core identity dataset (`system` group and first-run `superadmin` login account with bcrypt password storage) during bootstrap. The shared accessrbac core (`EnsureBuiltins`) seeds the `superadmin` and `viewer` roles on startup for any app that enables `AccessRbac` in its `SharedAPIConfig`.
- The app seeds wildcard-host endpoint rows with access tiers for protected API modules so rate-limit classification and endpoint catalog metadata are ready on a fresh install. Protected shared management APIs seed as `DevOnly`. Per-endpoint RBAC seed rows are no longer inserted; the bootstrap `superadmin` bypasses the accessrbac matrix entirely.
- `mymatasan` registers app-local `detection_rule`, `alert_event`, `recording_config`, and `recording_segment` entities. Detection rules store camera binding, detection type, polygon JSON, optional rule config JSON, per-rule schedule policy JSON, threshold, minimum frames, cooldown, sound setting, enabled state, and last trigger time. Alert events store the triggering rule and camera, label, confidence, polygon, optional bounding box/snapshot path, metadata JSON, acknowledgement fields, and audit timestamps.

## Configuration Contract

Config source:

- `ENVIRONMENT=dev` -> `apps/<selected-app>/config.dev.json`
- otherwise -> `apps/<selected-app>/config.json`

Home/data directory split (`infra/apphost`, packaged installs):

- `resolveHomeDir` locates the read-only app root (static assets, bundled AI scripts, the default `config.json`): an `<APP>_HOME`/`KOPIV2_HOME` env override, else the app's dev `BaseDir()` if present, else the running executable's directory (packaged/portable installs).
- `resolveDataDir` locates the writable state root (mutable config, database, recordings, logs, encryption key): an `<APP>_DATA`/`KOPIV2_DATA` env override, else it defaults to `homeDir` — a source/dev checkout keeps the single-directory layout unchanged.
- On first boot, if `dataDir` differs from `homeDir` and no config exists yet at `dataDir`, the shipped default `config.json` is seeded (copied) from `homeDir` into `dataDir` so the app has a writable copy to persist secrets/runtime settings back to.
- Relative `fileStorage.path`, `logging.path`, `tls.certPath`/`keyPath`, `sso.caCertPath`, a SQLite `db.db_name`, the recordings/snapshot root, and the AI training data dir all resolve against `dataDir` (`apphost.ResolveWritablePath`), not the app's dev `BaseDir()`.
- `ResolveWritablePath` is upgrade-safe: if the resolved `dataDir` target does not yet exist but a copy is found at the pre-packaging legacy location (CWD-relative, as an unpackaged dev checkout resolved it), the legacy path is returned instead so an upgrade never orphans in-place recordings, keys, or a database. Installed services set `WorkingDirectory=dataDir`, making the legacy path identical to the target (a no-op fallback).
- `apphost.Dependencies.HomeDir` / `DataDir` expose both resolved roots to app modules; `mymatasan` resolves its at-rest encryption key (`secret/atrest.key`) against `DataDir` the same way.
- On Windows, `apphost.Run` (`infra/apphost/run.go` + `service_windows.go`/`service_other.go`) detects whether the process was launched by the Service Control Manager (`svc.IsWindowsService()`) and, if so, runs under `svc.Run` so `sc.exe`/`services.msc` Stop/Shutdown requests trigger the same graceful shutdown as an OS signal elsewhere; an interactive run or any non-Windows OS is unaffected. The Windows installer (`packaging/windows/mymatasan.iss`) registers the service this way, splitting home (`Program Files`) from data (`%ProgramData%\MyMataSan`) via per-service `Environment`.

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
- Relative TLS paths resolve from the writable data directory (see home/data directory split above).
- If an HTTPS listener is configured and either `certPath` or `keyPath` does not exist, `apphost` generates a long-lived (10-year) self-signed ECDSA P-256 keypair covering `localhost`, every configured `server.hostnames` entry, and every local IP address, so a fresh install serves HTTPS immediately without manual cert setup. Existing files are left untouched (bring your own cert, or front the app with a TLS-terminating reverse proxy — see `deploy/README.md`).

Database config contract (`db` in app config):

- `engine`: DB engine selector (`postgres`, `mariadb`, or `sqlite`).
- Runtime DB adapter and bootstrap implementation support all three engines.
- For SQLite, `db_name` is a database file path and relative paths resolve from the selected app directory. `:memory:` is supported for tests/dev experiments only.
- SQLite is intended for single-process small-device deployments; use PostgreSQL or MariaDB for multi-instance production deployments.
- `apps/mymatasan/config.dev.json` defaults to SQLite at `./data/mymatasan.db` with the in-process default cache for standalone small-device deployment.

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

`myseliasan` local auth config contract (`localAuth` in `myseliasan/config.json`):

- `localAuth.username`: username for the stock bootstrap superadmin. Defaults to `admin` when empty.
- `localAuth.password`: password for the stock bootstrap superadmin. Defaults to `admin` when empty. Changed on every startup while the account is still stock and untouched (MustChangePassword=true, not Disabled); once the operator has set their own password or the account is retired, config no longer overrides it.
- The stock account carries `mustChangePassword=true`; the accessrbac session middleware blocks requests from this account (returning `password_change_required`) until the password is changed via `POST /api/auth/change-password`.

Standalone `mymatasan` local auth contract:

- `mymatasan` uses app-local HTTP Basic Auth backed by the local SQLite database instead of MyIDSan JWT/RBAC.
- On first startup (no local users exist), `EnsureDefaultAdmin` seeds the admin account from the `localAuth` config block: `localAuth.username` (falls back to `admin` when empty) and `localAuth.password` (env `LOCAL_ADMIN_PASSWORD` overrides the configured value; falls back to `admin` when both are empty, mirroring `myseliasan`'s stock superadmin default). The seeded account is always created with `MustChangePassword=true`, so the bootstrap credential — whichever source it came from — is never usable past the first login.
- On subsequent startups (users already exist), `flagDefaultAdminPassword` still force-flags any admin account left on the shipped legacy default (`admin` / `Admin123`) as must-change, protecting old installs that predate the config-driven seed.
- Local user passwords are stored as bcrypt hashes.
- User management is exposed under Settings and `/api/settings/users`.
- The app prevents deleting, disabling, or demoting the last active admin user.

Decoder startup-default config contract (`decoder` in app config):

- `mjpeg.ffmpegPath`: ffmpeg executable used only by `mymatasan` MJPEG fallback live view. Empty or `ffmpeg` resolves from `PATH`; use an absolute path when running as a service or on systems where `PATH` differs.
- `mjpeg.quality`: MJPEG encoder quality used for ffmpeg RTSP conversion; lower values are higher quality and higher CPU/bandwidth. Omitted or invalid values default to `7`.
- `mjpeg.threads`: ffmpeg MJPEG output thread count. Omitted or invalid values default to `1` and are bounded for small-device safety.
- `ffmpeg.rtspTransport`: RTSP transport passed to ffmpeg (`tcp`, `udp`, `udp_multicast`, `http`, or `https`); omitted defaults to `tcp`.
- `ffmpeg.hwaccel`: hardware decode mode (`none`, `auto`, `d3d11va`, `dxva2`, `vaapi`, `cuda`, `qsv`, `videotoolbox`, `vdpau`, or `vulkan`). `none` uses CPU software decode and is the safest default.
- `ffmpeg.hwaccelDevice`: optional ffmpeg hardware device/GPU selector.
- `ffmpeg.initHwDevice`: optional ffmpeg `init_hw_device` value for advanced hardware contexts.
- `ffmpeg.videoDecoder`: optional ffmpeg video decoder name such as `h264_cuvid`; empty lets ffmpeg auto-select.
- `ffmpeg.probeSize` and `ffmpeg.analyzeDuration`: ffmpeg input probing limits used for RTSP conversion and frame capture; larger values can improve unusual stream detection at the cost of startup latency.
- `ffmpeg.lowDelay` and `ffmpeg.noBuffer`: low-latency ffmpeg flags used for live conversion; default to enabled.
- Legacy `camera.ffmpegPath` is still read as a migration fallback when seeding runtime settings.
- `decoder.browseRoots`: optional array of extra directories the server-side ffmpeg file picker may browse, on top of the built-in defaults (app dir + `bin/`, user home, OS-specific common install locations). Default empty.
- Runtime settings expose `POST /api/settings/runtime/auto-tune`, which inspects saved camera RTSP track metadata plus local `ffmpeg -hwaccels` output and immediately saves a conservative decoder profile. Hardware decode is selected only for safely verifiable platform paths; otherwise auto-tune keeps CPU software decode.
- ffmpeg availability and provisioning: `GET /api/settings/decoder/status` reports whether a usable ffmpeg is found; `POST /api/settings/decoder/ffmpeg/install` + `GET /api/settings/decoder/ffmpeg/install/status` run and poll the in-app installer (admin-only; on success the resolved path is persisted into runtime settings); `GET /api/settings/fs/browse` is an admin-only, read-only directory picker for choosing the binary, confined to the whitelist above (`services/filesystem_browse.go`). The same installer backs the first-run setup wizard's video-engine check.
- Runtime settings expose `GET /api/settings/runtime/gpu-devices`, which returns selectable hardware decoder device values for the Settings UI. Linux discovery checks VAAPI render nodes and NVIDIA CUDA indices, Windows discovery lists display adapter indices, and macOS discovery documents VideoToolbox default-device behavior.

Stream startup-default config contract (`stream` in app config):

- `webrtc.enabled`: enables browser WebRTC live view; omitted defaults to disabled.
- `webrtc.iceServers`: optional STUN/TURN server list with `urls`, `username`, and `credential` fields.
- `mjpegFallback.enabled`: enables MJPEG fallback and MJPEG-only mode when WebRTC is disabled; omitted defaults to enabled.
- These config values seed the SQLite-backed runtime settings row on first startup or reset. Settings page changes apply without app restart.

WebRTC live audio contract:

- When the camera RTSP stream announces a G.711 (PCMA or PCMU) audio media, `infra/stream/rtsp.go` subscribes to it and broadcasts audio RTP packets alongside video packets.
- `infra/stream/webrtc.go` creates an additional `audio/PCMA` or `audio/PCMU` track in the Pion peer connection when audio packets are available.
- The browser offer always includes an `a=recvonly` audio transceiver. When the server has no audio track, Pion responds with `a=inactive` and the browser's `ontrack` never fires for audio.
- The browser frontend routes audio to a dedicated `<audio>` element independent of the `<video>` element, so the video element stays permanently muted (required for browser autoplay policy) while audio is user-controlled via a mute/unmute overlay button.
- The mute/unmute button appears only when an audio track is received (`ontrack` fires with `kind === "audio"`).
- Audio play/pause is triggered directly in the button click handler (user gesture context) to satisfy browser autoplay restrictions.

Vision monitor startup config contract (`vision` in `mymatasan` app config):

- `enabled`: starts or disables the background monitor; omitted defaults to enabled.
- `intervalMs`: monitor polling interval; omitted or invalid values default to two seconds.
- `captureTimeoutMs`: timeout for one sampled JPEG capture; omitted or invalid values default to twelve seconds.
- `diagnosticCooldownSeconds`: throttles diagnostic alert rows for repeated capture, detect, or sample states.
- `detector.mode`: `motion`, `external`, `hybrid`, or `persistent`; omitted defaults to `motion`.
- `detector.command` and `detector.args`: external detector command. `external`/`hybrid` receive raw JPEG bytes on stdin per process run; `persistent` starts a long-lived worker that receives newline-delimited JSON with base64 JPEG bytes and returns one JSON response per request.
- `GET /api/settings/vision/ai-tool/status` checks the configured AI command, Python packages, worker script, model file, and native fallback status without downloading dependencies. If the external AI tool is unavailable and `useMotionFallback` is enabled, startup falls back to native motion detection; person, vehicle, animal, fire, and smoke labels require the external tool, while native motion and motion-centroid line crossing can still run.
- `detector.timeoutMs`: external detector command timeout or persistent worker request timeout.
- `detector.useMotionFallback`: lets `hybrid` mode fall back to motion-only startup when no external command is configured.
- `detector.useMotionIntrusion`: routes `intrusion` rules to motion in `hybrid` or `persistent` mode.
- `detector.minObjectConfidence`: optional lower bound applied before object candidates can match rules.
- `detector.classMap`: maps rule detection types to model labels, including semantic aliases such as `animal` -> `cat`, `dog`, and other model labels.
- YOLO-backed CCTV rules should use empirical rule thresholds. MyMataSan UI defaults semantic rules to `threshold: 0.35` and `minFrames: 2`; reusable infra defaults remain `0.75` and `3` when a caller omits rule values.

Vision runtime settings contract (DB-backed, edited in Settings → AI, applied without restart):

- `vision.yolo`: per-frame inference overrides (conf, iou, augment, imgsz, half, maxDet); zero means "use the worker env default".
- `vision.capture`: AI frame-sourcing config — `mode` (`auto`/`siphon`/`standalone`), shared `intervalMs`/`frameWidth`, `standalone.captureTimeoutMs`, `siphon.fps`/`siphon.staleLimitMs`. Zero fields use safe fixed built-in defaults; `POST /api/settings/runtime/capture-auto-config` derives values from detected GPU/CPU and saved camera count. The mode is honored: `siphon`/`auto` read decoded frames off the recorder's MJPEG tee (the recording stream); `standalone` (and auto's fallback) grab a one-frame JPEG from the **same recording stream** when a camera records (so `auto` never alternates streams), else the live stream. `GET /api/vision/cameras/{id}/frame` returns the exact frame the detector samples, which the rule-editor zone/line preview draws on so the geometry matches what is detected.
- `vision.alertNotification`: which detection-alert fields/media populate the notification payload — `includeRuleName`, `includeLabel`, `includeConfidence`, `includeBoundingBox`, `includeZonePolygon`, `includeSnapshot`. A nil/unset value (legacy rows) means include everything; an explicit struct with false fields is preserved.

Vision notification delivery contract:

- Actionable AI alerts are published to the in-app feed plus enabled outbound channels (webhook, Telegram). The triggering rule name is the notification title; the captured snapshot is delivered to Telegram via `sendPhoto` (uploaded photo) and embedded in the webhook payload as base64 (`snapshotBase64`/`snapshotContentType`/`snapshotFilename`).
- When the bounding-box field is enabled, the detection box and object-label tag are drawn onto the snapshot image server-side (`vision.AnnotateJPEG`) so the delivered picture matches the AI Log detail overlay; the in-app log view itself still renders the box as a frontend overlay on the raw image.
- `GET /api/vision/alerts/{id}/snapshot` returns the raw frame by default (so the UI can draw its own overlay); the opt-in `?annotated=1` query returns the same image with the detection box drawn in, used by the Log detail "Download with box" action and any download/share that should carry the box.
- The snapshot rides on a non-persisted notification attachment, so persistence, SSE, and log channels skip the image bytes; only media-capable outbound channels include it.
- Both the background monitor and the manual `POST /api/vision/alerts` path apply the `vision.alertNotification` field config.

`mymatasan` startup config blocks (config.json) added for the appliance hardening features:

- `loginSecurity`: failed-login lockout — `enabled`, `maxAttempts` (5), `windowSeconds` (300), `lockoutSeconds` (60), `lockoutMaxSeconds` (900), `failedDelayMs` (500), `notifyOnLockout`. State is in-memory and per source IP; resets on restart.
- `recording.shred`: secure deletion of footage — `enabled` (*bool, default true) and `passes` (int, default 3). When enabled, retention purge and segment deletion overwrite-then-unlink; `enabled:false` (or `passes:0`) reverts to plain unlink. Intermediate `.ts` files removed during remux are not shredded.
- **Recording compression** (`recording.storage`, runtime-editable via Settings → Recording): four levers to shrink footage without hurting performance. (1) **At-rest storage codec** — `codec` (`copy`/`h264`/`hevc`, default `copy`), `quality` (NVENC CQ, default 26), `maxConcurrentEncodes` (shared NVENC session cap, default 2). With a non-copy codec each completed segment is re-encoded **once** on the GPU (NVENC) at remux time; live capture and event clips always stay stream-copy, and a global NVENC semaphore queues encodes so recording is never blocked. The on-disk codec is stored on `recording_segment.codec`. (2) **Playback transcode** — HEVC segments stream as-is to capable browsers (Chrome/Edge with OS HEVC, Safari); incapable browsers (Firefox/older) get an on-the-fly HEVC→H.264 transcode on the serve path (`?transcode=h264`, fragmented MP4, no plaintext copy stored). (3) **Camera-side H.265** — `GET`/`POST /api/cameras/{id}/encoder` pushes a codec + bitrate cap to the camera's own encoder over ONVIF (Media2/ver20, Media1 fallback, change verified by re-read), the zero-host-cost lever; host then stream-copies. (4) Capacity estimate accounts for the added GPU encode load. Requires an NVIDIA GPU (NVENC) for the host-side levers; defaults leave existing installs recording exactly as before.
- `security.encryptAtRest` (default true) / `security.keyPath` (default `secret/atrest.key`): encryption-at-rest for media (recordings, snapshots/alert images, training images) via the reusable `infra/atrest` AES-256-GCM module. The key lives outside the media roots so the factory reset can destroy it (crypto-erase). With encryption off, data is plaintext; pre-existing plaintext is always read transparently (no migration). Model `.pt` weights stay plaintext.
- `security.keyProtector` (`""`/`file` default, `auto`, `dpapi`, `systemd-creds`, `passphrase`) / `security.passphrase`/`passphraseFile`/`passphraseEnv` / `security.recoveryPath` (default `recovery.atrestkey` beside `keyPath`): protects the on-disk master key itself. `auto` picks DPAPI (Windows) or systemd-creds (systemd Linux, TPM2-backed when present) when available, else `file`; `passphrase` (Argon2id) is portable and the recommended choice for Docker. The key is resolved at the very start of `RegisterAppRoutes`, before any other service — if it's missing but an init marker shows one existed here before, the app mounts only a public pre-login recovery gate (`GET/POST /api/system/recovery/gate`, `/unlock`) instead of starting normally, unless `recoveryPath` + a resolvable passphrase let it restore automatically first. Admin endpoints `POST /api/system/recovery/export`/`verify` (Settings → Backup & Recovery) manage the passphrase-protected recovery escrow that backs up host-bound protectors.
- `bootstrap.allowReset` (default false): gates the **Secure Wipe & Reset** factory-reset endpoint (`POST /api/system/reset`, plus `/state` and `/progress`) and its UI button. The reset stops camera services, **destroys the at-rest encryption key (crypto-erase)**, fast-erases all media (instant unlink), drops and rebuilds the database (schema + stock seed), securely scrubs the freed space (per-volume TRIM/discard then a time-budgeted HDD overwrite), and restarts via the reusable `apphost.Restarter` (the same primitive the in-app self-update apply uses to relaunch into the new binary — see below). It is deliberately unstoppable and best-effort: the Postgres drop uses `DROP DATABASE ... WITH (FORCE)` (with a plain-`DROP` fallback for pre-13 servers), and a stage problem (un-erasable file, failed key destroy, wipe error) becomes a non-fatal *warning* — the sequence always drives to a restart (which re-runs bootstrap to finish any interrupted rebuild) rather than aborting. The real wipe guarantees are the crypto-erase and instant unlink; TRIM + scrub are defense-in-depth. `config.json` is not touched (runtime settings live in the DB and reset with it).
- Capacity: `GET /api/capacity` (estimate) and `POST /api/capacity/calibrate` (benchmark the detector) compute how many cameras the host can process across AI/memory/live-view. Recording is a rolling buffer: instead of zeroing the camera count on a small disk, it caps cameras at a ~1-day minimum-retention floor and reports the retention achievable at the recommended count, balancing cameras against retention. When the at-rest storage codec re-encodes (h264/hevc), a "Recording re-encode (GPU)" workload (NVENC sessions × realtime factor) is added; with re-encode enabled but no GPU it reports 0 and flags the misconfiguration.
- Manual purge: `POST /api/recording/segments/purge` deletes recording segments already past each camera's `retentionDays` (the same sweep disk-mitigation runs); `POST /api/notifications/purge?olderThanDays=&onlyRead=` deletes old notifications. Both are surfaced as on-demand buttons (Recording page / Notification settings).
- **In-app self-update** (`services.UpdateService`, `apis/system.go`): `GET /api/system/update` reports current/latest version (`updateAvailable`), whether this install can self-update (`canSelfUpdate`), and any in-flight apply state; `POST /api/system/update/check` forces an immediate GitHub Releases check (also run on a 6-hour scheduler); `POST /api/system/update/apply` (admin-only) downloads the matching release archive, verifies its SHA-256 against the release's `checksums.txt`, swaps the binary and `static/`/`ai/` dirs into `HomeDir`, and restarts via `apphost.Restarter`. Self-update is gated off (`canSelfUpdate=false`) when the `MYMATASAN_MANAGED` env var is set to `package` (set by the `.deb`/`.rpm` systemd unit — upgrade via the package manager instead) or `docker` (set by `deploy/Dockerfile.release` — pull+recreate the container instead), or when `HomeDir` isn't writable; it is available on portable archives and the Windows service install.
- **In-app AI runtime installer** (`services.PythonInstaller`, `apis/settings.go`): `GET /api/settings/vision/ai-runtime/status` reports whether a self-contained Python + torch + ultralytics runtime is installed; `POST /api/settings/vision/ai-runtime/install` (admin-only) downloads a pinned astral `python-build-standalone` interpreter into `DataDir/pyruntime`, pip-installs a GPU (CUDA) or CPU PyTorch build (GPU-detected via `nvidia-smi`) plus `ultralytics`, and persists the resolved interpreter path into `vision.detector.command`; `GET /api/settings/vision/ai-runtime/install/status` polls the job. This is separate from the existing Train-in-app "Install GPU support" flow, which upgrades the Python the detector already uses rather than installing one from nothing.

Vision line-crossing rule config contract (`ruleConfig` on `mymatasan` detection rules):

- `line_crossing`: object-backed rule that triggers when a tracked object center crosses any configured line.
- `multi_line_crossing`: object-backed rule that triggers only after the same tracked object crosses configured lines in array order.
- `classes`: optional model labels allowed to participate, such as `person`, `car`, `truck`, `cat`, or `dog`. Empty uses the detector class map for the rule type.
- `direction`: `both`, `forward`, or `reverse`. Direction is based on the configured line point order and the object's signed side transition.
- `maxSecondsBetweenLines`: maximum time between sequence steps for `multi_line_crossing`; omitted defaults to twenty seconds.
- `maxTrackDistance`: normalized maximum center distance for frame-to-frame track matching; omitted defaults to `0.25`.
- `trackTtlSeconds`: seconds before an unseen track expires; omitted defaults to ten seconds.
- `lines`: ordered list of one to five line entries, each with `id` and two normalized points: `{"id":"start","points":[[0.35,0.2],[0.35,0.8]]}`.

Machine (host) health monitor contract (DB-backed `machineHealth` settings, edited in Settings → Machine Health, applied live):

- Samples host CPU, memory, and disk usage on `intervalMs` via gopsutil. Disk volumes monitored are auto-detected from the paths the app writes to (working dir, recordings/snapshot dir, log dir, per-camera recording storage) plus any user-defined `disk.paths`, deduplicated per underlying volume.
- Each metric has `warnPercent`/`criticalPercent`. A debounced state machine raises a Warning/Critical notification (system category) only after `sustainedSamples` consecutive breaching samples, and a recovery notice after `recoverySamples` consecutive normal samples — mirroring the camera health monitor's debounce.
- Disk mitigation (`mitigation.enabled`): at `purgeAtPercent` it triggers an immediate retention purge of expired recordings (throttled to once per 10 min); at `pauseRecordingAtPercent` it pauses NVR recording (recorder stops writing new segments) to stop the volume filling completely; recording resumes once the disk drops below `resumePercent`. Footage is not captured while paused.
- `mitigation.overwriteOldest` (default `false`) trades pausing for continuity: when the disk reaches `pauseRecordingAtPercent`, instead of pausing it deletes the oldest recorded segments across all cameras (ignoring their per-camera `retentionDays`) until usage would drop below `resumePercent`, via `IRecordingService.PurgeOldestSegments(ctx, keepAfter, wantBytes)`. `mitigation.overwriteMinKeepDays` (default `1`, range 1–365) is a safety floor — footage newer than this many days is never auto-deleted. If a wipe attempt frees nothing (all remaining footage is inside the keep floor), the monitor falls back to pausing recording as before; while paused with overwrite enabled, it keeps retrying the wipe (throttled to the same ~10 min cadence) so recording resumes automatically once footage ages past the floor. A distinct "Oldest footage overwritten" (warning) notification fires on each successful wipe, separate from the pause/resume notifications.
- Normalization keeps `criticalPercent > warnPercent` and `resumePercent < pauseRecordingAtPercent`. The recorder exposes `Pause()`/`Resume()`/`IsPaused()`; resume restarts recorders from their retained configs.

Vision rule schedule contract (`schedulePolicy` on `mymatasan` detection rules):

- Empty schedule policy means the rule is always active.
- `timezone` accepts an IANA timezone such as `Asia/Kuala_Lumpur`; empty or `local` uses the process local timezone.
- `mode` accepts `allow` or `deny`. `allow` activates only inside matching windows/ranges. `deny` activates outside matching windows/ranges.
- `windows` contains weekly windows with optional `days` (`sun` through `sat`) plus `start` and `end` in `HH:MM`. Overnight windows are supported.
- `dateRanges` contains one or more absolute RFC3339 `start`/`end` ranges.
- `preset` is optional UI metadata used by the frontend to preserve selections such as `custom` or `range`; backend rule evaluation ignores it.

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
- When a version bump on `main` changes the `mymatasan` app version, `.github/workflows/main.yml`'s `version` job also tags the commit `v<mymatasan-version>` and pushes it (skipped if that tag already exists, e.g. a bump that only touched core or another app), then calls the reusable `.github/workflows/release.yml` in the same workflow run (`workflow_call`, so no PAT is needed — a tag pushed with `GITHUB_TOKEN` alone would not trigger a separate workflow run). `release.yml` checks out the tag, runs GoReleaser (cross-compiled binaries + archives + `.deb`/`.rpm` + multi-arch Docker images per `.goreleaser.yaml`), extracts the newest `CHANGELOG.md` entry as the GitHub Release notes, and — in a follow-on `windows-installer` job — builds the Windows x64 service installer (`packaging/windows/mymatasan.iss` via Inno Setup) and uploads it to the same release. `release.yml` is also invokable manually (`workflow_dispatch` with a `tag` input) to rebuild a release.

## API Documentation Contract

- `GET /swagger`: Swagger UI.
- `GET /swagger/openapi.json`: OpenAPI 3.0 document.
- Endpoint list is generated from runtime route registration, so shared and app-local APIs are documented from one source.
- Key endpoints include reusable request/response schema components (`components.schemas`) for FE integration and code generation.
- Key list/create/update endpoints are mapped to endpoint-specific response wrappers (typed `result` payloads) instead of only generic default/paging contracts.
- Shared DB-backed list endpoints expose `limit`, `offset`, and optional `filters`/`sorters` query parameters so paging can be filtered and ordered in the backend before the response is returned. `filters` and `sorters` accept JSON object or array values, with repeated `filter` and `sorter` query parameters also supported. Multiple filters are combined with `AND`; multiple sorters keep the request order.
- Non-JSON endpoints are explicitly modeled with route-accurate status/content (for example OAuth redirect `302` and binary file download `application/octet-stream`).
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
