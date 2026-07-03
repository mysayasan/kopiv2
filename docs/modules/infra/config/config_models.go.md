# Module: infra/config/config_models.go

## Purpose

Defines the top-level app configuration model loaded from app config JSON.

## Responsibilities

- Model optional OAuth provider configuration for Google and GitHub.
- Model server listener hostnames and explicit TLS/non-TLS ports.
- Model bootstrap, JWT, SSO, local app auth, decoder startup defaults, live stream startup defaults, vision detector startup settings, file storage, cache, rate limiting, transaction coordination, logging, API log cleanup, telemetry, TLS, and DB settings.

## Notes

- `login.google` and `login.github` are independently optional.
- A provider block whose `client_id` or `client_secret` (config or env) is blank is **disabled with a warning** rather than refusing to boot. This means a half-configured social provider (block present but credentials absent) is silently skipped; the identity service continues serving local and other configured social logins. Operators who want to enforce that a provider is correctly configured must verify the `login.google` / `login.github` disabled-with-warning log line on startup.
- `server.tlsPorts` and `server.nonTlsPorts` are the preferred listener config fields.
- `tls.certPath` and `tls.keyPath` are required when HTTPS listeners are enabled; relative paths are app-relative.
- Legacy `server.ports`, `server.enableTls`, and `server.enableNonTls` remain available only as a fallback when explicit port lists are empty.
- `logging.path` is app-relative unless absolute, and is resolved with Go `filepath` for Windows, Linux, and macOS.
- `recording.shred.*` configures secure-overwrite of deleted footage; `recording.storage.{codec,quality,maxConcurrentEncodes}` seeds the at-rest recording codec defaults (codec default `copy` = no host re-encode) which are then runtime-editable via Settings → Recording.
- `security.keyProtector` selects how the on-disk encryption-at-rest master key is protected: `""`/`"file"` (plaintext, default/backward-compatible), `"auto"` (platform default: DPAPI on Windows, systemd-creds on a systemd Linux host, else file), `"dpapi"` (Windows, machine-scoped, host-bound), `"systemd-creds"` (Linux, TPM2-backed when present, host-bound), or `"passphrase"` (Argon2id-derived KEK, portable — the right choice for Docker). Switching protectors re-wraps the same key on the next boot, so existing encrypted data stays readable; host-bound protectors cannot be unwrapped on another machine.
- `security.passphrase`/`passphraseFile`/`passphraseEnv` source the KEK for the `passphrase` protector, resolved in that order, then `$ATREST_PASSPHRASE`; prefer `passphraseFile` (a mounted Docker secret) or `passphraseEnv` over inlining the passphrase in config.
- `security.recoveryPath` (default `recovery.atrestkey` beside `keyPath`) is where a disaster-recovery escrow (exported from Settings → Backup & Recovery) is looked for on first boot. When the master key is missing but this file is present and a passphrase resolves, the app restores the key from it automatically (no prompt) and migrates it to the configured protector; see `infra/atrest/startup.go.md`.
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
- `rateLimit.devOnly`, `rateLimit.authOnly`, and `rateLimit.public` configure per-tier request counts and windows.
- `sso.issuer` configures the expected/issued JWT issuer.
- `sso.audience` configures comma-separated accepted JWT audiences.
- `sso.sessionTtlSeconds` controls cookie/session-cache lifetime.
- `sso.policyCacheTtlSeconds` controls RBAC policy cache lifetime.
- `sso.internalToken` protects myidsan service-to-service introspection and authorization APIs.
- `sso.providerBaseUrl` points relying apps to MyIDSan for authorization-code login.
- `sso.caCertPath` optionally points to a PEM CA/certificate bundle used by relying-app backend HTTPS calls to MyIDSan.
- `sso.clientId` and `sso.clientSecret` configure relying-app token exchange credentials.
- `sso.redirectBaseUrl` configures the relying-app public callback origin used in authorization requests.
- `sso.redirectPath` configures the relying-app callback path.
- `sso.authCodeTtlSeconds` and `sso.accessTokenTtlSeconds` provide MyIDSan defaults when per-client DB config does not override them.
- `localAuth.enabled`, `localAuth.username`, and `localAuth.password` configure each standalone app's bootstrap local admin: `mymatasan` reads `localAuth.username`/`localAuth.password` at startup to seed its DB-backed first admin user (`ILocalUserService.EnsureDefaultAdmin`), falling back to `admin`/`admin` when empty, same as `myseliasan`'s stock superadmin.
- `decoder.mjpeg.ffmpegPath` configures the startup default ffmpeg executable used by `mymatasan` MJPEG fallback live view and RTSP frame capture; empty defaults to resolving `ffmpeg` from `PATH`.
- `decoder.mjpeg.quality` and `decoder.mjpeg.threads` tune MJPEG output quality and ffmpeg thread count.
- `decoder.ffmpeg` carries RTSP transport, hardware decode mode/device, optional decoder name, probe/analyze limits, and low-latency flags for ffmpeg-backed RTSP conversion.
- Legacy `camera.ffmpegPath` remains in the config model only as a migration fallback.
- `stream.webrtc.enabled` controls whether browser live view attempts WebRTC first; omitted defaults to disabled.
- `stream.webrtc.iceServers` optionally configures STUN/TURN servers as browser-compatible `urls`, `username`, and `credential` entries.
- `stream.mjpegFallback.enabled` controls whether the MJPEG endpoint can be used as fallback or primary mode when WebRTC is disabled; omitted defaults to enabled.
- `vision.enabled` controls whether the MyMataSan vision monitor worker starts; omitted defaults to enabled.
- `vision.intervalMs`, `vision.captureTimeoutMs`, and `vision.diagnosticCooldownSeconds` control monitor polling, per-frame capture timeout, and diagnostic alert throttling.
- `vision.persistSampledDiagnostics` (default `false`) — when `false`, the noisy per-frame heartbeat diagnostic (frame captured; nothing detected) is suppressed. Capture and detect failures are always logged.
- `vision.diagnosticRetentionDays` — purge Vision-monitor diagnostic alert rows older than N days (default no purge when 0).
- `vision.alertRetentionDays` — purge all alert event rows (real detections included) older than N days; 0 = keep forever.
- `vision.alertPurgeIntervalHours` — how often the background purge job runs (default 6 hours).
- `vision.detector.mode` selects `motion`, `external`, `hybrid`, or `persistent`; `motion` is the dependency-free default.
- `vision.detector.command` and `vision.detector.args` configure either a per-frame detector process (`external`/`hybrid`) or a long-lived newline-JSON worker (`persistent`).
- `vision.detector.classMap` maps rule detection types such as `fire`, `smoke`, `person`, `vehicle`, `animal`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`, and `lpr` to model labels.
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
- `pairing.certTtlHours` sets the lifetime of issued node certificates on the control plane. `0` defaults to `168` (7 days).
- `pairing.renewBeforeHours` makes the node request renewal when its cert is within this many hours of expiry. `0` defaults to `48`.
- `pairing.heartbeatIntervalSeconds` controls how often the `myseliasan` background loop calls `INodeRegistry.Heartbeat` to probe all adopted nodes over mTLS. `0` defaults to `60`.
- `pairing.controlPort` is the port myseliasan listens on for node-dialed control-channel WebSocket-over-fleet-mTLS connections. `0` defaults to `49533`. Must be reachable from nodes.
- `pairing.mediaPort` is the port myseliasan listens on for node-dialed media-channel connections (camera RTP relay). Separate from `controlPort` so high-rate media never competes with control traffic. `0` defaults to `49534`. The node derives the parent host from its stored `ParentBaseURL`.
- `pairing.parentBaseUrl` (parent/myseliasan only) overrides the base URL recorded on each adopted node for callbacks (enroll / release / self-drop) and as the host the node dials for the control and media channels. When empty, falls back to `sso.redirectBaseUrl`. Must be the parent's LAN-reachable URL (e.g. `https://192.168.1.10:3002`) — never `localhost` — when node and parent are on separate machines.
- `nodeStream.publicIps` lists the parent's externally reachable IPs, advertised as WebRTC host candidates (NAT 1:1) for cross-network browser-to-parent media. Leave empty for same-LAN/local dev.
- `nodeStream.udpPort` binds a single shared WebRTC UDP port for all browser peers connecting to relayed node cameras (one firewall rule). `0` = pion's default ephemeral ports.
- `nodeStream.iceServers` are STUN/TURN servers offered to the browser for the parent↔browser WebRTC leg of node camera relay. A TURN server is only needed when the parent is itself behind NAT. Omit or leave empty for same-LAN use.
