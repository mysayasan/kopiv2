# Module: infra/config/config_models.go

## Purpose

Defines the top-level app configuration model loaded from app config JSON — the blocks a
*second* app already uses or obviously will. It is deliberately no longer every app's whole
config: see "Per-app config seam" below.

## Responsibilities

- Model optional OAuth provider configuration for Google and GitHub.
- Model server listener hostnames and explicit TLS/non-TLS ports.
- Model bootstrap, JWT, SSO, local app auth, notification startup defaults, login-lockout
  security, password strength policy, MFA enforcement policy, encryption-at-rest,
  pairing/fleet, file storage, cache, rate limiting, transaction coordination, logging,
  API log cleanup, telemetry, TLS, and DB settings.
- Retain the raw config document (`raw []byte`, unexported/untagged) that
  `LoadAppConfiguration` (`app_config.go.md`) decoded it from, exposed via `Raw()`, so an app
  that owns config blocks of its own can decode them from the same bytes. See "Per-app
  config seam" below.

## Per-app config seam

`Camera`, `Decoder`, `Stream`, `Vision`, `Health`, and `Recording` — six blocks that were
mymatasan-only (nothing else in the codebase read them) — are **no longer part of this
model**. They moved to `apps/mymatasan/config` (`Config`, decoded via
`apphost.AppConfigDecoder.DecodeAppConfig`, see `infra/apphost/types.go.md`), along with the
model types `StreamConfigModel`, `WebRTCConfigModel`, `MJPEGFallbackConfigModel`,
`HealthConfigModel`, `RecordingConfigModel`, `VisionConfigModel`,
`VisionTrainingConfigModel`, and `VisionDetectorConfigModel` (all now under
`apps/mymatasan/config`, not here). `WebRTCICEServerModel` stayed here because `NodeStream`
(shared, the fleet media relay) also uses it.

This is **not** a nested `"app"` key in `config.json` — the moved blocks stay exactly where
they were, at the top level, so no deployed config file has to change. An app decodes its
own blocks from the same raw document this model already parsed (`Raw()`). What moved was
ownership (who has a Go type for the block, and who resolves its data-relative paths), not
the file format. See `docs/modules/apps/mymatasan/config/config.go.md` and
`docs/MYMATASAN_TIER2_PLAN.md` (phase C).

What stayed here is anything a second app already uses or obviously will: `Security`
(encryption-at-rest — `myseliasan` uses it too), `Pairing`/`NodeStream` (the fleet),
`Notification`, `LoginSecurity`, `Kerberos`, and every infra block (`server`, `db`, `cache`,
`rateLimit`, `sso`, `tls`, `telemetry`, `fileStorage`, `logging`, `apiLog`, `transaction`,
`securityHeaders`, `bootstrap`, `localAuth`, `login`).

## Notes

- `login.google` and `login.github` are independently optional.
- A provider block whose `client_id` or `client_secret` (config or env) is blank is **disabled with a warning** rather than refusing to boot. This means a half-configured social provider (block present but credentials absent) is silently skipped; the identity service continues serving local and other configured social logins. Operators who want to enforce that a provider is correctly configured must verify the `login.google` / `login.github` disabled-with-warning log line on startup.
- `server.tlsPorts` and `server.nonTlsPorts` are the preferred listener config fields.
- `tls.certPath` and `tls.keyPath` are required when HTTPS listeners are enabled; relative paths are app-relative.
- Legacy `server.ports`, `server.enableTls`, and `server.enableNonTls` remain available only as a fallback when explicit port lists are empty.
- `logging.path` is app-relative unless absolute, and is resolved with Go `filepath` for Windows, Linux, and macOS.
- `LoginSecurity` is `LoginSecurityConfigModel` (`enabled *bool`, `maxAttempts`, `windowSeconds`, `lockoutSeconds`, `lockoutMaxSeconds`, `failedDelayMs`, `notifyOnLockout`) — read only through its `Effective() EffectiveLoginSecurity` resolver, never field-by-field. `Enabled` is a pointer so an **absent** `loginSecurity` block is distinguishable from an explicit `"enabled": false`: it used to be a plain `bool`, so any `config.json` without the block silently decoded to `Enabled=false` and the failed-login guard became a no-op — which is what shipped in `deploy/dist/myseliasan-config.json` and in the myidsan/myseliasan dev configs. `Effective()` now treats an absent block as **ON** with tuned defaults (`maxAttempts` 8, `windowSeconds` 300, `lockoutSeconds` 60, `lockoutMaxSeconds` 3600, `failedDelayMs` 400 — matching what `deploy/dist/myidsan-config.json` already shipped explicitly), and fills any zero-valued tunable the same way (a zero `maxAttempts` would otherwise lock a user out on their first failed attempt). Turning the lockout off is still possible; it just now takes a deliberate `"enabled": false` rather than an omission. Every caller (`apps/mymatasan/app/config_map.go`'s `loginGuardConfigFromAppConfig`, `apps/myidsan/app/app.go`'s `loginGuardConfig`, `apps/myiotsan/app/app.go`'s `loginGuardConfig`) was updated to call `.Effective()` instead of reading the struct directly. Covered by `infra/config/login_security_test.go`.
- `security.keyProtector` selects how the on-disk encryption-at-rest master key is protected: `""`/`"file"` (plaintext, default/backward-compatible), `"auto"` (platform default: DPAPI on Windows, systemd-creds on a systemd Linux host, else file), `"dpapi"` (Windows, machine-scoped, host-bound), `"systemd-creds"` (Linux, TPM2-backed when present, host-bound), or `"passphrase"` (Argon2id-derived KEK, portable — the right choice for Docker). Switching protectors re-wraps the same key on the next boot, so existing encrypted data stays readable; host-bound protectors cannot be unwrapped on another machine.
- `security.passphrase`/`passphraseFile`/`passphraseEnv` source the KEK for the `passphrase` protector, resolved in that order, then `$ATREST_PASSPHRASE`; prefer `passphraseFile` (a mounted Docker secret) or `passphraseEnv` over inlining the passphrase in config.
- `security.recoveryPath` (default `recovery.atrestkey` beside `keyPath`) is where a disaster-recovery escrow (exported from Settings → Backup & Recovery) is looked for on first boot. When the master key is missing but this file is present and a passphrase resolves, the app restores the key from it automatically (no prompt) and migrates it to the configured protector; see `infra/atrest/startup.go.md`.
- `securityHeaders` (unrelated to the `security` encryption-at-rest block above) configures the `middlewares.SecurityHeaders` response-header hardening middleware (see `domain/utils/middlewares/security_headers.go.md`), wired in `infra/apphost/run.go` for every app. Every field is optional; unset fields fall back to hardened defaults (`nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: strict-origin-when-cross-origin`, HSTS on TLS, `Server` header stripped). `securityHeaders.contentSecurityPolicy` is the one field that is opt-in — the CSP header is only sent when non-empty, since a wrong policy silently breaks a SPA. `securityHeaders.disabled` skips the middleware entirely. `securityHeaders.hsts.{enabled,maxAgeSeconds,includeSubDomains,preload}` tune `Strict-Transport-Security`, sent only over TLS connections. `mymatasan`'s and `myseliasan`'s `config.json`/`config.dev.json` set a tested `contentSecurityPolicy` (same policy string); `myidsan` currently relies on the hardened defaults.
- `passwordPolicy` (`PasswordPolicyConfigModel`, Productization Phase 3) constrains
  human-chosen passwords across all four paths that ever set one (self-registration,
  admin-provisioned account creation, change-password, self-service token-based reset);
  server-generated credentials (bootstrap/temporary passwords, 16-character CSPRNG output)
  are deliberately exempt. Read only through `Effective() EffectivePasswordPolicy`, never
  field-by-field. `minLength` (`int`) defaults to `12` when absent/`<= 0` — up from the
  `len >= 8` rule that used to be hard-coded on only two of the four password paths, and
  not enforced at all on the other two. `requireUpper`/`requireLower`/`requireDigit`/
  `requireSymbol` (`bool`) all default **OFF**: composition rules push people toward
  predictable substitutions (`P@ssw0rd1`) and a longer passphrase beats a short password
  with a symbol bolted on; they exist for customers contractually obliged to enable them.
  `blockCommon` (`*bool`) defaults **ON** when the field is absent (pointer so "absent"
  is distinguishable from an explicit `false`) and rejects an embedded denylist of
  widely-reused passwords (`apps/myidsan/services/password_policy.go.md`'s
  `commonPasswords`) — there is no HIBP k-anonymity lookup, since myidsan is positioned
  to run air-gapped. `ValidatePassword` (same file) also rejects a password equal to the
  account's username/email-local-part regardless of policy toggles.
- `mfa` (`MfaPolicyConfigModel`, Productization Phase 3) decides who is **required** to
  hold a second factor; MFA enrollment itself remains self-service either way. Read only
  through `Effective() EffectiveMfaPolicy`. `policy` is `"off"` | `"optional"` | `"required"`;
  an absent or unrecognised value resolves to `"optional"` (the pre-existing opt-in-only
  behaviour) rather than silently becoming `"required"` (which would lock everyone out) or
  `"off"` (which would silently drop a control the operator asked for). `requiredRoleIds`
  (`[]int64`) narrows `"required"` to specific roles — empty means every role.
  `applyToDirectory` (`bool`) defaults `false`: an LDAP-bound account's factor policy
  usually belongs to its directory, and forcing a second factor here can duplicate one the
  domain already enforces. `EffectiveMfaPolicy.RequiresFactor(roleId, isDirectoryAccount)`
  is the single decision point `apps/myidsan/app/app.go`'s `userLoginResolver` calls to set
  `AccessPrincipal.MustEnrollMfa`. Enforcement happens **after** a successful password
  login (the account gets a session but is pinned to the enrollment screen) — refusing the
  login outright would lock out every existing administrator the moment the policy is
  switched on. See `docs/MYIDSAN_MFA_PLAN.md` §6 and
  `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 3.
- `fileStorage.path` is app-relative unless absolute.
- `fileStorage.cleanup.enabled` starts the expired file cleanup scheduler.
- `fileStorage.cleanup.frequencySeconds` controls scheduler check frequency and defaults to 60 seconds in apphost.
- `fileStorage.cleanup.batchSize` controls the maximum expired rows removed per scheduler run and defaults to 100 in apphost.
- `logging.path` is used as the base filename for dated daily log files.
- `logging.maxLineBytes` bounds each listed log line to avoid oversized API responses.
- `logging.cleanup.enabled` starts the runtime log cleanup scheduler.
- `logging.cleanup.maxRetentionDays` controls the retention cutoff.
- `logging.cleanup.frequencyMinutes` controls scheduler check frequency and defaults to 60 minutes in apphost.
- `apiLog.cleanup.enabled` starts database-backed API log retention cleanup.
- `apiLog.cleanup.maxRetentionDays` controls the API log row retention cutoff.
- `apiLog.cleanup.frequencyMinutes` controls API log cleanup frequency and defaults to 60 minutes in apphost.
- `telemetry.enabled` enables shared telemetry wiring.
- `telemetry.prometheus.enabled` exposes Prometheus-format metrics.
- `telemetry.prometheus.metricsPath` controls the metrics scrape route.
- `telemetry.prometheus.apiDurationThresholdMs` controls slow API request metrics.
- `rateLimit.enabled` enables API sliding-window rate limiting.
- `rateLimit.trustedProxies` lists IPs/CIDRs of reverse proxies allowed to supply the client's real address via `X-Forwarded-For`/`X-Real-IP`; empty (default) means those headers are ignored and the direct TCP peer is used, so a directly-exposed instance can't have its rate-limit/login-lockout bucket spoofed. Set it to your proxy's address(es) when deploying behind one (see `domain/utils/middlewares/rate_limit.go.md`).
- `rateLimit.devOnly`, `rateLimit.authOnly`, and `rateLimit.public` configure per-tier request counts and windows.
- `sso.issuer` configures the expected/issued JWT issuer.
- `sso.audience` configures comma-separated accepted JWT audiences.
- `sso.sessionTtlSeconds` controls cookie/session-cache lifetime.
- `sso.policyCacheTtlSeconds` controls RBAC policy cache lifetime.
- `sso.internalToken` protects myidsan service-to-service introspection and authorization APIs. `applySSOConfigFromEnv` (`infra/apphost/run.go.md`) now refuses to carry forward a known placeholder value (e.g. the literal `change-me-in-production` shipped in example configs) — it drops the value to empty instead, which disables `/api/sso/introspect` (a nil token makes `authorizeInternal` reject everything) and logs a `WARNING`, rather than leaving a publicly-known token protecting the endpoint. Set a long random value (`sso.internalToken` or `SSO_INTERNAL_TOKEN`) if a relying app cannot share the session cache.
- `sso.providerBaseUrl` points relying apps to MyIDSan for authorization-code login.
- `sso.caCertPath` optionally points to a PEM CA/certificate bundle used by relying-app backend HTTPS calls to MyIDSan.
- `sso.clientId` and `sso.clientSecret` configure relying-app token exchange credentials.
- `sso.redirectBaseUrl` configures the relying-app public callback origin used in authorization requests.
- `sso.redirectPath` configures the relying-app callback path.
- `sso.authCodeTtlSeconds` and `sso.accessTokenTtlSeconds` provide MyIDSan defaults when per-client DB config does not override them.
- `localAuth.enabled`, `localAuth.username`, and `localAuth.password` configure each standalone app's bootstrap local admin: `mymatasan` reads `localAuth.username`/`localAuth.password` at startup to seed its DB-backed first admin user (`ILocalUserService.EnsureDefaultAdmin`), falling back to `admin`/`admin` when empty, same as `myseliasan`'s stock superadmin.
- `smtp.enabled` turns on the OPTIONAL internal mail relay for myidsan's self-service
  account-recovery email link (see `infra/mailer/mailer.go.md`,
  `apps/myidsan/services/password_reset.go.md`); `false` (the default — the block is
  absent from the shipped `config.json`/`config.dev.json`, so it decodes to the Go
  zero value) means the operator recovery queue is the only recovery channel and
  nothing ever reaches for a network, keeping an air-gapped install unaffected.
  `smtp.host`/`smtp.port` (default `587` when `0`) address the relay; `smtp.from`
  falls back to `smtp.username` when blank; `smtp.username`/`smtp.password`
  authenticate to the relay and, when set, **require** `smtp.useStartTls` — the
  sender refuses to transmit credentials over an un-upgraded connection.
- `kerberos.enabled` turns on Kerberos SPNEGO single sign-on (`myidsan`, Phase 2 of `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`); `false` (the shipped default in both `config.json` and `config.dev.json`) means the login endpoint isn't even registered as "offered" — no button, no challenge. `kerberos.keytabPath` is the exported service keytab (required when enabled; a missing/unreadable file logs a `WARNING` at boot and disables Kerberos rather than failing startup — see `apps/myidsan/app/app.go.md`). `kerberos.servicePrincipal` must match the SPN the keytab was exported for (e.g. `HTTP/myidsan.corp.local`); empty lets the acceptor match any principal present in the keytab. `kerberos.onlyRealms` optionally allow-lists accepted realms (case-insensitive); empty accepts any realm the keytab can decrypt tickets for. `kerberos.displayLabel` names the SSO button on both login surfaces (default `"Windows (SSO)"` when blank). See `infra/login/kerberos.go.md` and `docs/HOWTO.md`'s Kerberos SSO subsection for keytab provisioning (`ktpass`/`samba-tool`) and operational requirements (clock skew, browser trust).
- `camera`, `decoder`, `stream`, `vision`, `health`, and `recording` are documented in
  `docs/modules/apps/mymatasan/config/config.go.md` — they are `mymatasan`-owned blocks, no
  longer part of this model (see "Per-app config seam" above).
- `transaction.lockProvider` selects Redis or in-memory FIFO transaction locking; empty inherits `cache.provider`.
- `transaction.lockWaitTimeoutMs` bounds queue wait time.
- `transaction.lockLeaseMs` controls Redis owner lease duration.
- `transaction.operationTimeoutMs` bounds coordinated file-storage transaction work.
- `transaction.stuckTimeoutMs` emits telemetry when a lock is held too long.
- `transaction.jobWorkerEnabled` starts the durable file-storage upload worker.
- `transaction.jobWorkerFrequencySeconds` controls worker polling frequency.
- `transaction.maxAttempts` caps retry attempts before a durable upload job fails and cleans up.
- `pairing.enabled` turns the node-side discovery responder on/off in `mymatasan`; omitted defaults to `true`.
- `pairing.multicastAddr` overrides the UDP multicast group and port for discovery; empty defaults to `"239.255.90.21:49531"` (the `infra/pairing` package default). Both node and control plane must agree on this value.
- `pairing.replayWindowSeconds` overrides probe/announce freshness bounds; `0` defaults to `30s`.
- `pairing.mtlsPort` is the node's mutual-TLS management listener port (heartbeat + release). Used by `EnrollmentManager` on the node and stamped into `ManagedNode.MTLSPort` on the control plane at adoption. `0` defaults to `49532`.
- `pairing.certTtlHours` sets the lifetime of issued node certificates on the control plane. `0` defaults to `2160` (90 days) — raised from a 7-day default now that renewal is operator-gated per node (see `ManagedNode.AutoRenew` below), so an un-renewed node stays in the fleet for a meaningful window instead of lapsing weekly. The shipped `apps/myseliasan/config.json` sets it explicitly to `2160`.
- `pairing.renewBeforeHours` makes the node request renewal when its cert is within this many hours of expiry. `0` defaults to `48` (node-side; unchanged — the node still auto-attempts renewal on this schedule regardless of the control-plane gate). `myseliasan`'s control plane also reads this value (`NodeRegistryConfig.CertWarnBefore`) to raise a fleet-health "certificate expiring" notification when a node whose auto-renew is OFF is this close to expiry (a node with auto-renew ON never warns, since its renewal will be honoured); when the key is `0`/unset the control-plane side falls back to a 7-day warn window instead of the node's 48-hour default, so the two sides can differ unless the key is explicitly set. The shipped `apps/myseliasan/config.json` sets it to `336` (14 days) to give operators more warning given the longer 90-day cert lifetime.
- `pairing.heartbeatIntervalSeconds` controls how often the `myseliasan` background loop calls `INodeRegistry.Heartbeat` to probe all adopted nodes over mTLS. `0` defaults to `60`.
- `pairing.controlPort` is the port myseliasan listens on for node-dialed control-channel WebSocket-over-fleet-mTLS connections. `0` defaults to `49533`. Must be reachable from nodes.
- `pairing.mediaPort` is the port myseliasan listens on for node-dialed media-channel connections (camera RTP relay). Separate from `controlPort` so high-rate media never competes with control traffic. `0` defaults to `49534`. The node derives the parent host from its stored `ParentBaseURL`.
- `pairing.parentBaseUrl` (parent/myseliasan only) overrides the base URL recorded on each adopted node for callbacks (enroll / release / self-drop) and as the host the node dials for the control and media channels. When empty, falls back to `sso.redirectBaseUrl`. Must be the parent's LAN-reachable URL (e.g. `https://192.168.1.10:3002`) — never `localhost` — when node and parent are on separate machines.
- `nodeStream.publicIps` lists the parent's externally reachable IPs, advertised as WebRTC host candidates (NAT 1:1) for cross-network browser-to-parent media. Leave empty for same-LAN/local dev.
- `nodeStream.udpPort` binds a single shared WebRTC UDP port for all browser peers connecting to relayed node cameras (one firewall rule). `0` = pion's default ephemeral ports.
- `nodeStream.iceServers` are STUN/TURN servers offered to the browser for the parent↔browser WebRTC leg of node camera relay. A TURN server is only needed when the parent is itself behind NAT. Omit or leave empty for same-LAN use.
