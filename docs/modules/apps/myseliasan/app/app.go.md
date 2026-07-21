# Module: apps/myseliasan/app/app.go

## Purpose

Implements the `myseliasan` relying control-plane app for the shared runtime host.

## Key Type: module

The module struct is stateful: it holds the `*services.ControlServer` and an `*atomic.Bool` media-listening flag (both set during `RegisterAppRoutes`, once those listeners are constructed) so `ReadinessStatus` can read them for the lifetime of the process — apphost uses one module instance per run.

## `ReadinessStatus` (implements `apphost.ReadinessReporter`)

`myseliasan` implements the shared `apphost.ReadinessReporter` interface, so apphost's `/ready` and `/api/ready` handlers merge these fields into the readiness payload alongside `ok`/`db`/`cache`:

- `controlChannel` — `"up"`/`"down"` from `ControlServer.IsListening()`.
- `connectedNodes` — the live control-connection count from `ControlServer.ConnectedCount()`, stringified.
- `mediaRelay` — `"up"`/`"down"` from the module's media-listener `atomic.Bool`, flipped true/false around the media relay's `srv.Run(bgCtx)` call (`defer` clears it back to false on exit).

Each field is only added once its backing listener has been wired (nil-guarded), so a call before `RegisterAppRoutes` finishes reports no advisory fields rather than a false "down".

**These are advisory only** — per the `apphost.ReadinessReporter` contract (see `docs/modules/infra/apphost/run.go.md` / `types.go`), they never flip the process's `ok`/HTTP status, which stays gated on db + cache alone. A dead control-channel or media listener alone does not make `/ready` return 503; it surfaces as `"down"` for an operator/monitor to notice. This closes the gap where a dead fleet listener still reported the process fully ready, without touching the core readiness contract — actually gating readiness on the fleet listeners would be a separate, core-scoped decision affecting every app and was deliberately not done here.

## `Migrations` (implements `apphost.Migrator`)

Returns a fixed list of `bootstrap.Migration`s that run **before** the auto-migrator and
**independently of the `autoMigrate` config flag**, so the fleet-map columns exist even in a
deployment that has auto-migration turned off — without them, node adoption's `INSERT` (or a
floor upload) fails with a 500 after the fact. Every migration is idempotent (checks
`information_schema`/`pragma_table_info` before `ALTER TABLE`, so it is a no-op wherever the
auto-migrator already added the column) and, where a plain `ADD COLUMN` would leave existing
rows `NULL`, immediately backfills them to the entity's zero value — the non-pointer
`float64`/`bool`/`string` entity fields cannot scan a `NULL`, so an un-backfilled column breaks
`List`/read-back with `"converting NULL to float64 is unsupported"` on every pre-existing row:

- `20260718-01-managed-node-geo` / `20260718-02-managed-node-geo-backfill` — add
  `managed_node.lat`/`lon`/`map_placed` (the geographic fleet map) and separately backfill any
  `NULL`. Split into two migration IDs (rather than one edited migration) because editing an
  already-applied migration's `Exec` does not re-run it; a database that had already applied `01`
  before the backfill logic existed needed a **new** ID to guarantee it runs once.
- `20260719-01-placement-fov` — add `node_placement.heading`/`fov` (camera coverage arcs),
  backfilled to `0`.
- `20260719-02-floor-design` — add `floor_plan.design` (drawn-plan vector JSON), backfilled to
  `''`.
- `20260719-03-floor-bgpath` — add `floor_plan.bg_path` (pristine background image path),
  backfilled to `''`.
- `20260720-01-site-geo` — add `site.lat`/`lon`/`map_placed` (the digital-twin building marker on
  the geographic map), backfilled to `0`/`0`/`false` (`ensureSiteGeoColumns`). Mirrors the
  `20260718` `managed_node` geo migration for the same reason: without it, an existing `site`
  table has no lat/lon/map_placed, so both creating a site and listing sites fail.
- `20260720-02-site-icon` — add `site.icon` (the building glyph shown on the geo-map marker),
  backfilled to `''`.
- `20260720-03-node-site` — add `managed_node.site_id` (the building an appliance resides in),
  backfilled to `0`.
- `20260720-04-node-auto-renew` — add `managed_node.auto_renew` (the certificate renewal gate,
  `BOOLEAN`), backfilled to `false`. This only seeds the column's zero value; the separate
  one-time `registry.BackfillAutoRenew` (below) then flips already-*enrolled* nodes to `true` so
  an existing fleet is not surprise-expired.

`geoColumnType(base, engine)` maps a base SQL type (`DOUBLE PRECISION`/`BOOLEAN`) to the exact
per-engine concrete type the auto-migrator itself generates (mirroring
`infra/db/bootstrap`'s `normalizeSQLType`), so a column added here is byte-identical to one the
auto-migrator would have added and never trips a schema-drift warning. `tableColumns` is the
shared per-engine "does this column exist" probe (`information_schema.columns` on
postgres/mariadb, `pragma_table_info` on sqlite).

## Responsibilities

- Provides app identity and base directory.
- Registers `ManagedNode`, `ControlSetting`, `NodeAccessGrant`, `ControlUser`, `AuditLog`, the shared `AccessRole`/`AccessRolePermission`, `FleetRule` + `FleetRuleClause` (cross-domain correlation, the reason the suite has a fourth app), and — the fleet map — `Site`, `FloorPlan`, `NodePlacement` entities for DB bootstrap.
- Enables only `Version` and `AccessRbac` from the shared API surface (`SharedAPIs()`); operational APIs (log, file storage, cache, app registry, etc.) are disabled for the control plane.
- Seeds the local endpoint catalog for rate limiting and runtime metadata, including `Notifications`, `Node Access`, `Audit` (`/api/audit`), and — the fleet map — `Basemap` (`/api/basemap`), `Sites` (`/api/sites`), `Floors` (`/api/floors`), and `Placements` (`/api/placements`) endpoints, all `AuthOnly`.
- On startup, first calls `consumeAdminResetMarker` (see `app/firstrun.go.md`) to service a pending `RESET_ADMIN` lock-out-recovery marker if present; otherwise seeds the stock superadmin local account via `EnsureStockSuperadmin` using credentials from `localAuth.username` / `localAuth.password` in config. An empty config password no longer becomes the literal `admin`/`admin` default — `EnsureStockSuperadmin` resolves `LOCAL_ADMIN_PASSWORD`, then config, else generates a strong per-install password (see `services/rbac.go.md`). When the returned `StockSeedResult.Seeded` is true, `announceFirstRunAdmin` prints the console banner and writes `INITIAL_ADMIN_LOGIN.txt` to the data dir.
- Binds the `ControlUser` service as the `AccessUserResolver` for `deps.Access`, so the shared accessrbac middleware enforces the permission matrix on myseliasan's own endpoints.
- Registers auth/session routes (`NewAuthApi`, `NewSessionApi`).
- Builds the shared, append-only `IAuditService` (`services.NewAuditService(deps.Db, logf)`) before any API that records to it; `logf` routes write-failure diagnostics through `deps.Logger.Warnf("myseliasan.audit", ...)`. It is passed into `NewRbacAdminApi`, `NewNodesApi`, `NewNodeAccessApi`, and `NewNodeProxyApi` so each can record sensitive actions (see each file's own `.go.md` "Audit trail" section), and into `NewAuditApi` (`/api/audit`, superadmin-only read) which is the only way to read the trail back.
- Registers myseliasan-specific RBAC admin surface (`NewRbacAdminApi` at `/api/rbac/*`) for user management and the bootstrap superadmin handoff.
- Before building the node registry, resolves fleet-secret encryption at rest via `openFleetSecretCipher(deps)` (mirroring mymatasan's own `infra/atrest` boot sequence): reads the shared `security` block (`security.encryptAtRest`, default **true**; `security.keyPath`, default `<dataDir>/secret/atrest.key`; `security.keyProtector`/`passphrase`/`passphraseFile`/`passphraseEnv`; `security.recoveryPath`) and calls `atrest.OpenForStartup`. Returns `nil` (no encryption) when `encryptAtRest` is false. On `atrest.ModeRecoveryPending` (a key existed here before but is now missing) it **fails closed** — returns an error and refuses to boot rather than mint a replacement key and silently reset the whole fleet's trust; the operator must restore the key file or configure `security.recoveryPath` and restart. The resulting `*atrest.Cipher` (or `nil`) is passed into `NodeRegistryConfig.SecretCipher`.
- Builds `INodeRegistry` with `ParentBaseURL` derived from `pairing.parentBaseUrl` (when set) or `sso.redirectBaseUrl` (fallback). `pairing.parentBaseUrl` must be the parent's LAN-reachable URL for deployments where node and parent are on separate machines.
- Immediately after building the registry, calls `registry.BackfillAutoRenew(context.Background())` once at startup — this one-time pass turns on the new per-node certificate auto-renew gate (`ManagedNode.AutoRenew`) for every already-enrolled node, so upgrading an existing fleet (which was renewing automatically before this gate existed) does not silently start expiring certificates; a failure only logs a warning (`myseliasan.nodes`) rather than blocking boot. New adoptions after this point still start with `AutoRenew` off. See `services/node_registry.go.md`.
- Registers the offline vector basemap for the fleet map (`NewBasemapApi`, `apis/basemap.go.md`), resolving the basemap **directory** (may hold several `.pmtiles` region archives) via `apis.ResolveBasemapDir(deps.DataDir, "")` — an empty/absent directory is a supported state (the map renders without cartography), so this never blocks boot. Also passes `os.Getenv("MYSELIASAN_BASEMAP_SOURCE")` and `os.Getenv("MYSELIASAN_PMTILES_BIN")` through: when the source env var is set, an operator can download a new region on demand (the one action here that reaches the internet); both are empty/unset by default, keeping the app fully offline.
- Registers node-management routes (`NewNodesApi`, now passed a `logf` closure so a failed adopt logs its raw cause server-side even though the client gets a friendlier message), which now also exposes `PUT /api/nodes/{id}/position` for the geographic map, `PUT /api/nodes/{id}/building` for the digital-twin building assignment, and `GET /api/nodes/unrecognized` + `POST /api/nodes/{id}/block`/`forget` for stranded-node visibility (see `apis/nodes.go.md`).
- Starts the control channel server (`ControlServer`) on a dedicated fleet-mTLS port (`pairing.controlPort`, default `49533`). After the server is built, wires `ControlServer.IsConnected` into the registry via `SetControlPresence` so the heartbeat treats a live control connection as the authoritative online signal (mTLS poll becomes a fallback); also stashes the server on the module (`m.controlServer`) for `ReadinessStatus`.
- Starts a background heartbeat goroutine (after `SetControlPresence` is wired) to reconcile node liveness with grace-window flap protection.
- Wires proactive fleet-health alerting: before registering the sink, calls `services.DescribeMyseliasanMetrics(deps.Metrics)`. `registry.SetFleetEventSink` is set (before the heartbeat loop starts) to a closure that calls `publishFleetEvent`, so a node going online→lost, a lost node recovering, or a certificate nearing expiry (per `CertWarnBefore`, derived from `pairing.renewBeforeHours`) is surfaced in the unified notification feed instead of failing silently — the same closure also increments `MetricFleetEventsTotal` (`myseliasan_fleet_events_total{kind}`) via the package-level `fleetEventKind(e.Kind)` helper, so a burst of lost/recovered transitions or a trickle of cert-expiring warnings is visible on `/metrics` even if nobody is watching the notification feed at the time.
- After the control server starts, runs `services.RunFleetMetricsSampler(bgCtx, deps.Metrics, controlServer, <closure over registry.List>, 10*time.Second)` — samples `myseliasan_control_channel_up`, `myseliasan_nodes_connected`, and `myseliasan_nodes_adopted` off the control server every 10s, keeping the control-channel accept path free of a metrics lock. See `services/metrics.go.md`.
- Builds the unified notification service and registers `NewNotificationApi`. Node-pushed events are ingested into the notification feed via `ingestNodeEvent`.
- **Builds the cross-domain correlator** (`services.NewCorrelator`, `services/correlate.go.md`) — THIS is the reason the fourth app exists: `motion on Camera 3 (mymatasan) AND a door contact opening (myiotsan) AND no badge swipe (myiotsan) -> intrusion`. No single node can see that; only the control plane, which already receives every node's events in one feed, is in a position to notice the conjunction. The `nodeKind` resolver passed in is a closure over `registry.List` — the node's kind is always resolved from the **adopted node's own record**, never from anything an event body claims, so a door sensor cannot assert it is a camera and satisfy a camera-scoped clause. Calls `correlator.SetMetrics(deps.Metrics)` right after construction, then `Reload`s the rule cache once at startup (fails boot on error) and registers `apis.NewFleetRulesApi(api, *deps.Auth, controlSession, correlator)`.
- Starts a 1-second-ticker goroutine that calls `correlator.Sweep(bgCtx)` — this is what makes an ABSENCE decidable, since nothing ever arrives to say "the badge was never swiped"; the passage of time has to.
- `onNodeEvent` (passed to `NewControlServer`) now does two things per node-pushed frame: `ingestNodeEvent` (unified feed, as before) AND `observeForCorrelation` (`app/correlate_bridge.go.md`) — the correlator is fed the **node's own event**, deliberately never the control plane's own re-published copy of it, because correlating on our own output would let one fleet rule's alert satisfy another fleet rule's clause and let two rules trigger each other forever.
- Registers sites + floor plans for the indoor map (`NewSitesApi`, `apis/sites.go.md`), built with `services.NewSiteService(deps.Db, secretCipher, planDir)` where `planDir` is `<dataDir>/floorplans` — floor-plan images are encrypted at rest with the same fleet cipher that protects the CA key/PSK. `NewSitesApi` also now exposes `GET /api/sites/overview` (per-building rollup) and `PUT /api/sites/{id}/position` (drag a building's marker) for the geographic map's digital-twin building layer, and `GET /api/sites/{id}/floorplans` (multi-node building drill-down).
- `SetRejectTracker` wires the `ControlServer` (once built, further down) into the already-registered `nodesApi` as its `rejectTracker` — the control server exposes `Unrecognized()`/`ForgetRejected()` for stranded (row-less or revoked) node connections, and `NewNodesApi` now returns the handler so this later wiring is possible. See `apis/nodes.go.md` and `services/control_server.go.md`.
- Registers per-node access-grant management (`NewNodeAccessApi`). The node access service is constructed with the roles service (`NewNodeAccessService(db, roleService)`) so superadmin roles receive implicit full node access. All three node APIs (`NewNodeAccessApi`, `NewNodeMediaApi`, `NewNodeProxyApi`) now accept the `controlSession *AccessSessionMidware` so they resolve the caller's live role on every request.
- Starts the node camera media relay: builds a `stream.WebRTCEngine` (from `nodeStream.publicIps` / `nodeStream.udpPort`; nil for same-LAN), starts a `mediarelay.Server` on `pairing.mediaPort` (default `49534`) using the fleet-CA mTLS server config, registers `NewNodeMediaApi` (`POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`, `GET /api/node-stream/config`). The listener goroutine flips `m.mediaListening` (an `*atomic.Bool`) true around `srv.Run(bgCtx)` so `ReadinessStatus` can report `mediaRelay` up/down.
- Registers the reverse command tunnel proxy (`NewNodeProxyApi` at `/api/nodes/{id}/proxy/...`).
- The `ShutdownFunc` cancels the background context (stops heartbeat, control server, and media relay server).

## `ingestNodeEvent`

Maps a node-pushed event frame to the control plane's notification feed:
- `"notification"` — re-published as-is (re-tagged with `nodeId` and a new parent-side ID) via `republishNodeNotification`.
- `"going-offline"` — converted to a system warning notification.
- Any other kind (`health`, `disk-full`, `alert`, `system`, …) is no longer dropped: the frame is parsed as a `notification.Notification` when it carries a `Title`/`Body` (category/severity filled in via `categoryForNodeKind`/`severityForNodeKind` if unset), otherwise it is wrapped in a generic message (`"Node <kind> event"`, body truncated to 500 chars) tagged with the raw kind — so a node reporting trouble is never silently lost.
- `categoryForNodeKind` buckets by substring match: `health`/`disk`/`cert` → `CategoryHealthCheck`, `alert` → `CategoryVisionAlert`, else `CategorySystem`. `severityForNodeKind` similarly guesses `Critical` (`alert`/`full`/`critical`/`fail`), `Warning` (`health`/`warn`/`disk`), else `Info`.

## `publishFleetEvent`

Turns a `services.FleetEvent` detected by the registry's heartbeat reconciler into a `notification.Notification` (category `CategoryHealthCheck`, source `"node:<id>"`, `Data["nodeId"]` set) published to the unified feed:
- `FleetEventNodeLost` → `Critical` "Node offline" — unreachable past the grace window, marked lost.
- `FleetEventNodeRecovered` → `Info` "Node back online".
- `FleetEventCertExpiring` → `Warning` "Node certificate expiring", body humanized via `humanizeHours` (e.g. "about 2d 3h" / "about 6 hours"), or "has expired" when `HoursLeft <= 0`; `Data` also carries `certExpiresAt` and `hoursLeft`.

## `fleetEventKind`

Maps a `services.FleetEventKind` to a stable, low-cardinality metric label:
`FleetEventNodeLost` → `"node_lost"`, `FleetEventNodeRecovered` → `"node_recovered"`,
`FleetEventCertExpiring` → `"cert_expiring"`, anything else → `"other"`. Used only by the
`MetricFleetEventsTotal` increment inside the fleet-event sink above.

## Notes

- **Runtime metrics (was 0 series on `/metrics` → 5):** a live scrape before this change showed
  myseliasan exposing nothing app-specific. As a control plane with no sensors of its own, its
  failures are fleet failures with no other symptom — a node dropping off the control channel
  looks in the UI identical to one an operator released, and a certificate creeping toward expiry
  has no symptom until it expires. See `services/metrics.go.md` for the full catalog
  (`myseliasan_nodes_connected`/`_adopted`/`control_channel_up`, `fleet_events_total{kind}`,
  `fleet_rule_fired_total{severity}`).
- The `security` config block consumed by `openFleetSecretCipher` is the same shared block mymatasan documents (`infra/config.SecurityConfigModel`; see `docs/modules/apps/mymatasan/app/app.go.md` and `docs/modules/infra/atrest/cipher.go.md`) — it is not myseliasan-specific and is not re-specified here. Here it protects the fleet CA private key + fleet PSK (see `services/fleet_ca.go.md` / `services/node_registry.go.md` / `services/secret_store.go.md`) instead of mymatasan's media files.
- The accessrbac middleware (`deps.Access`) only gates myseliasan's own management endpoints; the node tunnel (`/api/nodes/{id}/proxy/...`) is authorized by a separate axis (per-node `NodeAccessGrant` + owner role). The node media API (`/api/nodes/{id}/cameras/{cam}/webrtc/offer`) uses the same per-node access grant axis.
- Opening `/` without a valid myseliasan session redirects to `/api/auth/start`.
- myidsan remains the SSO identity provider; myseliasan issues its own local session after code exchange.
- `pairing.controlPort` (default 49533) and `pairing.mediaPort` (default 49534) are both distinct from `pairing.mtlsPort` (49532). All three ports must be reachable from nodes.
- The media relay goroutine fetches the TLS config asynchronously (`registry.ParentServerTLS`) — if the fleet CA has not yet initialized, a warning is logged and the goroutine exits without starting the listener (the node's media channel will reconnect once the parent is enrolled).
- `nodeMedia` routes are registered before the proxy catch-all so their exact paths win over the prefix match.
- Adopted nodes now carry a `Kind` (`"camera"` / `"iot"`; empty = camera, for pre-existing nodes) on `ManagedNode`, set from the node's own fleet-key-signed adopt reply — see `entities/managed_node.go.md` and `services/node_registry.go.md`. The unsigned discovery-announce `Kind` hint (`DiscoveredNode.Kind`) is display-only and never trusted for adoption or correlation.
- `RegisterWebRoutes` resolves `static/index.html` from `deps.HomeDir`, not `m.BaseDir()`. `BaseDir()` is the dev-mode CWD-relative path (`apps/myseliasan`); a packaged install runs with the binary and `static/` side by side and the service's working directory pointed elsewhere, so the old `BaseDir()`-relative lookup 404'd on `/` and `/index.html` outside a dev checkout. apphost's shared SPA catch-all already resolves off `HomeDir`; this now matches it.
