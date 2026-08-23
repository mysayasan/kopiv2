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

## `leaderOnly` / `leaderTicker` (deployment mode / Phase 1 multi-instance safety)

Two small package-level helpers that gate a background task on `deps.Leader.IsLeader()`
(`infra/coordination/leader.go.md`), used throughout `RegisterAppRoutes` to make myseliasan's
scheduled singletons — retention purges, the notification rollup, correlator sweep, dedup-ledger
prune, heartbeat reconciliation, digest generation/purge — run once for the DEPLOYMENT rather than
once per process:

- `leaderOnly(leader *coordination.Leader, fn func(context.Context)) func(context.Context)` —
  wraps `fn` so a call is a no-op unless `leader.IsLeader()` at the moment it runs. Used with
  `periodic` (below) for interval-ticker tasks.
- `leaderTicker(ctx, leader, interval, fn func(context.Context))` — its own ticker goroutine that
  checks `leader.IsLeader()` on every tick before calling `fn`. Used for the tasks that previously
  ran their own inline `for { select { ...; case <-ticker.C: ... } }` loop (correlator sweep, dedup
  prune, heartbeat), now folded into this one helper.

Both check leadership **inside** the task body / on every tick, never once at registration or
loop start — leadership moves while the process runs, and a loop that decided once would either
never start working when promoted, or never stop when demoted. The ticker itself keeps running for
a follower (only the `fn` call is skipped), so a promoted instance picks the work up on its next
tick with no restart. Standalone (the default per-process lock provider), the single instance
always holds the lease, so wrapping a task with either helper changes nothing — deliberately, since
the guard has to be safe enough to apply everywhere without a second behavior to reason about.

## `llmSidecarDeploymentCheck` (deployment mode / Phase 1 multi-instance safety)

`llmSidecarDeploymentCheck(deps) func(context.Context) []sharedservices.PreflightCheck` —
myseliasan's one app-specific row in the deployment-mode readiness checklist (`domain/shared/services/deployment.go.md`),
passed as `DeploymentEnv.ExtraChecks` (below). Reports `agent.llm.mode == "sidecar"` as a
`SeverityWarning`, not a blocker: in sidecar mode every instance starts its OWN `llama.cpp` and
loads its own copy of the model, so N instances cost N× the memory for one logical capability, and
each downloads the model separately — on a metered or air-gapped link, the part that actually
hurts. External mode points every instance at one shared inference endpoint instead. It is a
warning because the deployment still works; an operator who genuinely wants a model per instance is
entitled to that.

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
- `20260723-01-floor-3d` — add `floor_plan.grid`/`scale`/`wall_height`/`elevation` (the 3D floor
  view: painted wall/floor cells plus real-world scale, wall height and stacking elevation),
  backfilled the same NULL-safe way as `design`/`fov` (`grid` to `''`, the three `float64`
  columns to `0`) via `ensureFloor3DColumns`.
- `20260723-02-placement-mount` — add `node_placement.mount_height`/`pitch` (a camera's 3D
  coverage cone: mount height above the floor and downward tilt), backfilled to `0` via
  `ensurePlacementMountColumns`, mirroring the existing `heading`/`fov` migration for the same
  NULL-scan reason.
- `20260724-01-site-kind` — add `site.kind` (`building`/`outdoor`/`point` — see
  `entities/site.go.md`'s "Site kinds"), backfilled to `'building'` (not `''`) so an existing
  fleet's rows read the same in the database as in the UI (`entities.NormalizeSiteKind` treats
  both as building, but backfilling to the real value avoids relying on that everywhere).
- `20260724-02-floor-has-plan-image` — add `floor_plan.has_plan_image` (`BOOLEAN`, uploaded vs.
  generated-blank-canvas — see `entities/site.go.md`). Existing rows have no record of which they
  are, so they are classified by shape: a row with no `bg_path`, `content_type = 'image/png'` and
  the generated-blank dimensions (`1600×1000`) is backfilled `false`; every other row is
  backfilled `true`. This can only ever hide the "Remove plan" button on a floor that never had
  one, never hide it on a real plan — the one misjudged case (an upload that happens to match the
  blank canvas's exact size and was never annotated) is recovered by re-uploading it.
- `20260724-03-placement-unique-camera` — adds the `(node_id, camera_id)` unique index
  (`ux_node_placement_camera`) that makes camera placement exclusive (see
  `entities/node_placement.go.md`). Must run **before** the auto-migrator, which would otherwise
  try to create the same index (derived from the entity's `ukey` tag) and fail on a database that
  already contains duplicate placements — so this migration first deletes placements on a floor
  that no longer exists, then dedupes any remaining `(node_id, camera_id)` duplicates (oldest pin
  per camera kept), before adding the index. A fresh install (empty `node_placement` table) is a
  no-op here; the auto-migrator creates the table with the index already in place. Uses the same
  index name the auto-migrator derives, so whichever of the two runs second is a no-op.
- `20260806-01-notification-rollup-source` — shared with `mymatasan`
  (`domain/notification.MigrateRollupSourceColumn`, `docs/modules/domain/notification/
  rollup_migrate.go.md`): adds `notification_rollup.source` (the per-node-baseline dimension,
  `docs/modules/domain/entities/notification_rollup.go.md`) to an existing table, backfills `''`,
  and drops the old `ux_notification_rollup_slot` unique index so the auto-migrator rebuilds it
  **with** `source` included — without the drop, the rollup maintainer's first source-split insert
  violates the stale index and folding stops advancing.

`geoColumnType(base, engine)` maps a base SQL type (`DOUBLE PRECISION`/`BOOLEAN`) to the exact
per-engine concrete type the auto-migrator itself generates (mirroring
`infra/db/bootstrap`'s `normalizeSQLType`), so a column added here is byte-identical to one the
auto-migrator would have added and never trips a schema-drift warning. `tableColumns` is the
shared per-engine "does this column exist" probe (`information_schema.columns` on
postgres/mariadb, `pragma_table_info` on sqlite).

## Responsibilities

- Provides app identity and base directory.
- Registers `ManagedNode`, `ControlSetting`, `NodeAccessGrant`, `ControlUser`, `AuditLog`, the shared `AccessRole`/`AccessRolePermission`, `FleetRule` + `FleetRuleClause` (cross-domain correlation, the reason the suite has a fourth app), `FleetPolicy` + `FleetPolicyItem` (fleet configuration policy + drift detection, W2-1 — see `entities/fleet_policy.go.md`), `FleetRollout` + `RolloutNode` (staged version rollout, W2-5/F-07 — see `entities/fleet_rollout.go.md`), — the fleet map — `Site`, `FloorPlan`, `NodePlacement`, `RelayedNotif` (the reconnect-replay dedup ledger, see `entities/relayed_notif.go.md`), the shared `RuntimeSetting` (backs the first-run setup wizard's completion flag, the notification-rollup cursor, and the AI digest's schedule watermark; `ControlSetting` is deliberately left alone, since it holds the fleet key and node-adoption state), the shared `NotificationRollup` (the hourly-folded substrate the baseline/heatmap analytics and the AI digest's anomaly findings read — see `docs/modules/domain/entities/notification_rollup.go.md`), and `appentities.AgentDigest` (stored fleet digests — see `entities/agent_digest.go.md`) entities for DB bootstrap.
- Enables only `Version` and `AccessRbac` from the shared API surface (`SharedAPIs()`); operational APIs (log, file storage, cache, app registry, etc.) are disabled for the control plane.
- Seeds the local endpoint catalog for rate limiting and runtime metadata, including `Notifications`, `Node Access`, `Audit` (`/api/audit`), — the fleet map — `Basemap` (`/api/basemap`), `Sites` (`/api/sites`), `Floors` (`/api/floors`), and `Placements` (`/api/placements` — "list, reposition and remove node/camera markers on floor plans", reflecting the new `GET /api/placements` fleet-wide index) endpoints, `AI Agent` (`/api/agent` — "fleet digest, ask-the-fleet chat, and LLM sidecar management", see "Fleet AI agent" below), and `Settings` (`/api/settings`) + `System` (`/api/system`, the config editor + its process-restart action, both superadmin-gated in-handler) + `Setup Wizard` (`/api/setup`, first-run state and completion), all `AuthOnly` — plus `User Manual` (`/api/manual`, "public so help works on the sign-in screen and in the first-run wizard"), the one row seeded `Public`.
- On startup, first calls `consumeAdminResetMarker` (see `app/firstrun.go.md`) to service a pending `RESET_ADMIN` lock-out-recovery marker if present; otherwise seeds the stock superadmin local account via `EnsureStockSuperadmin` using credentials from `localAuth.username` / `localAuth.password` in config. An empty config password no longer becomes the literal `admin`/`admin` default — `EnsureStockSuperadmin` resolves `LOCAL_ADMIN_PASSWORD`, then config, else generates a strong per-install password (see `services/rbac.go.md`). When the returned `StockSeedResult.Seeded` is true, `announceFirstRunAdmin` prints the console banner and writes `INITIAL_ADMIN_LOGIN.txt` to the data dir.
- Binds the `ControlUser` service as the `AccessUserResolver` for `deps.Access`, so the shared accessrbac middleware enforces the permission matrix on myseliasan's own endpoints.
- Registers auth/session routes (`NewAuthApi`, `NewSessionApi`).
- Builds the shared, append-only `IAuditService` (`services.NewAuditService(deps.Db, logf)`) before any API that records to it; `logf` routes write-failure diagnostics through `deps.Logger.Warnf("myseliasan.audit", ...)`. It is passed into `NewRbacAdminApi`, `NewNodesApi`, `NewNodeAccessApi`, and `NewNodeProxyApi` so each can record sensitive actions (see each file's own `.go.md` "Audit trail" section), and into `NewAuditApi` (`/api/audit`, superadmin-only read) which is the only way to read the trail back.
- Registers myseliasan-specific RBAC admin surface (`NewRbacAdminApi` at `/api/rbac/*`) for user management and the bootstrap superadmin handoff.
- Before building the node registry, resolves fleet-secret encryption at rest via `openFleetSecretCipher(deps)` (mirroring mymatasan's own `infra/atrest` boot sequence): reads the shared `security` block (`security.encryptAtRest`, default **true**; `security.keyPath`, default `<dataDir>/secret/atrest.key`; `security.keyProtector`/`passphrase`/`passphraseFile`/`passphraseEnv`; `security.recoveryPath`) and calls `atrest.OpenForStartup`. Returns `nil` (no encryption) when `encryptAtRest` is false. On `atrest.ModeRecoveryPending` (a key existed here before but is now missing) it **fails closed** — returns an error and refuses to boot rather than mint a replacement key and silently reset the whole fleet's trust; the operator must restore the key file or configure `security.recoveryPath` and restart. Now returns `(*atrest.Cipher, *atrest.KeyStore, error)` — the `*atrest.KeyStore` is returned alongside the cipher purely so the factory reset (below) can crypto-erase the key; destroying it also clears the init marker, so the next boot after a reset mints a fresh key rather than hitting the recovery-pending path above. The resulting `*atrest.Cipher` (or `nil`) is passed into `NodeRegistryConfig.SecretCipher`.
- Builds `INodeRegistry` with `ParentBaseURL` derived from `pairing.parentBaseUrl` (when set) or `sso.redirectBaseUrl` (fallback). `pairing.parentBaseUrl` must be the parent's LAN-reachable URL for deployments where node and parent are on separate machines.
- Immediately after building the registry, calls `registry.BackfillAutoRenew(context.Background())` once at startup — this one-time pass turns on the new per-node certificate auto-renew gate (`ManagedNode.AutoRenew`) for every already-enrolled node, so upgrading an existing fleet (which was renewing automatically before this gate existed) does not silently start expiring certificates; a failure only logs a warning (`myseliasan.nodes`) rather than blocking boot. New adoptions after this point still start with `AutoRenew` off. See `services/node_registry.go.md`.
- Registers the offline vector basemap for the fleet map (`NewBasemapApi`, `apis/basemap.go.md`), resolving the basemap **directory** (may hold several `.pmtiles` region archives) via `apis.ResolveBasemapDir(deps.DataDir, "")` — an empty/absent directory is a supported state (the map renders without cartography), so this never blocks boot. Also passes `os.Getenv("MYSELIASAN_BASEMAP_SOURCE")` and `os.Getenv("MYSELIASAN_PMTILES_BIN")` through: when the source env var is set, an operator can download a new region on demand (the one action here that reaches the internet); both are empty/unset by default, keeping the app fully offline.
- Registers node-management routes (`NewNodesApi`, now passed a `logf` closure so a failed adopt logs its raw cause server-side even though the client gets a friendlier message), which now also exposes `PUT /api/nodes/{id}/position` for the geographic map, `PUT /api/nodes/{id}/building` for the digital-twin building assignment, and `GET /api/nodes/unrecognized` + `POST /api/nodes/{id}/block`/`forget` for stranded-node visibility (see `apis/nodes.go.md`).
- Starts the control channel server (`ControlServer`) on a dedicated fleet-mTLS port (`pairing.controlPort`, default `49533`). After the server is built, wires the registry's `SetControlPresence` (see "Node-owner registry + instance forwarding (Phase 2)" below) so the heartbeat treats a control-connected node — anywhere in the deployment, not just on this instance — as the authoritative online signal (mTLS poll becomes a fallback); also stashes the server on the module (`m.controlServer`) for `ReadinessStatus`.
- Starts the background heartbeat reconciler (after `SetControlPresence` is wired) via `leaderTicker(bgCtx, deps.Leader, hbInterval, func(ctx) { registry.Heartbeat(ctx) })` (replacing a previous inline ticker goroutine) to reconcile node liveness with grace-window flap protection. **Leader-gated (deployment mode / Phase 1 multi-instance safety)** so there is a SINGLE writer of node status — unguarded, every instance would reconcile the same rows from its own partial view and overwrite each other, flapping a node's status and firing the lost/recovered operator alerts once per instance. **Phase 1 known gap now CLOSED by Phase 2**: control-channel presence used to be an in-process map, so the leader saw only nodes whose channels terminated on ITSELF, and a node attached to another instance could be falsely marked `lost`. `SetControlPresence` is now wired to the node-owner registry's `ConnectedAnywhere` (below), a deployment-wide lookup, so the leader correctly sees a node as online regardless of which instance its control channel terminates on. See "Deployment mode / Phase 1+2+3 multi-instance safety" below.
- Wires proactive fleet-health alerting: before registering the sink, calls `services.DescribeMyseliasanMetrics(deps.Metrics)`. `registry.SetFleetEventSink` is set (before the heartbeat loop starts) to a closure that calls `publishFleetEvent`, so a node going online→lost, a lost node recovering, or a certificate nearing expiry (per `CertWarnBefore`, derived from `pairing.renewBeforeHours`) is surfaced in the unified notification feed instead of failing silently — the same closure also increments `MetricFleetEventsTotal` (`myseliasan_fleet_events_total{kind}`) via the package-level `fleetEventKind(e.Kind)` helper, so a burst of lost/recovered transitions or a trickle of cert-expiring warnings is visible on `/metrics` even if nobody is watching the notification feed at the time.
- After the control server starts, runs `services.RunFleetMetricsSampler(bgCtx, deps.Metrics, controlServer, <closure over registry.List>, 10*time.Second)` — samples `myseliasan_control_channel_up`, `myseliasan_nodes_connected`, and `myseliasan_nodes_adopted` off the control server every 10s, keeping the control-channel accept path free of a metrics lock. See `services/metrics.go.md`.
- Builds the unified notification service and registers `NewNotificationApi`. Node-pushed events are ingested into the notification feed via `ingestNodeEvent`. Builds `services.NewRelayDedup(deps.Db)` and wires it into `ingestNodeEvent`/`republishNodeNotification`, and wires `controlServer.SetOnConnect` to claim node ownership (below) then `replayNodeNotifications` — see "Replay on reconnect" below.
- **Notification rollups and retention purge** — the analytics substrate mymatasan already had, wired onto myseliasan for the first time (previously the notifications table just grew forever with no baseline/heatmap analytics behind it). `notificationService` is now built `.WithRollups(rollupRepo)` (`dbsql.NewGenericRepo[sharedentities.NotificationRollup]`), and a `notification.RollupMaintainer` (`notification.NewRollupMaintainer(notificationRepo, rollupRepo, services.NewRollupCursor(runtimeSettingRepo), 0, 0).WithGate(deps.Leader.IsLeader)`, `services/rollup_cursor.go.md`) is `Start`ed on `bgCtx` right after `NewNotificationApi` — the maintainer's first sweep backfills every historical row past the persisted cursor (starts at `0` on a fresh upgrade), so an existing fleet's history gets scored, not just new events. **`.WithGate(deps.Leader.IsLeader)` is new (deployment mode / Phase 1 multi-instance safety)**: the sweep is a read-cursor → page → increment → write-cursor cycle with no locking of its own, so two instances sweeping concurrently can both read the cursor before either writes it, both fold the same page, and every bucket gets incremented twice — a RACE (two SEQUENTIAL sweeps are harmless; the cursor already makes the second one a no-op), which is exactly why it would surface late, as numbers on the heatmap/baseline/anomaly-detection substrate that someone eventually stopped trusting. Standalone, the gate is a no-op. `runtimeSettingRepo` (`dbsql.NewGenericRepo[sharedentities.RuntimeSetting]`) is now built earlier in `RegisterAppRoutes` than before (it used to be built just for the setup wizard) specifically so the rollup cursor can use it. Separately, `periodic(bgCtx, "myseliasan.purge.notifications", purgeInterval(deps.Config.Notification.PurgeIntervalHours), leaderOnly(deps.Leader, ...))` runs the config-driven retention purge (`notification.retentionDays`, `0` keeps everything; `notification.purgeReadOnly` keeps unread rows regardless of age) via `notificationService.PurgeOlderThanDays`, incrementing `services.MetricNotificationsPurgedTotal` on every run that actually deletes rows. **Now `leaderOnly`-wrapped** (deployment mode / Phase 1 multi-instance safety) — a whole-table delete run concurrently by several instances is wasted, duplicate work, not corruption, but still work with no reason to run more than once. See `apis/notifications.go.md`'s new `GET /api/notifications/baseline` (the HTTP surface this substrate backs), `services/metrics.go.md`, and `domain/notification/rollup.go`'s `RollupMaintainer.WithGate` doc comment for the exact race.
- `periodic(ctx, name, interval, fn)` (package-level helper, new) runs `fn` once immediately then every `interval` until `ctx` is cancelled, `safego.Supervise`d under `name` so a panic inside `fn` restarts the loop with backoff instead of killing the process — "these loops are the retention purges, and a dead purge loop is invisible" (same contract as mymatasan's equivalent helper). `purgeInterval(hours)` turns a configured purge cadence into a `time.Duration`, defaulting to 6 hours when unset. Both the notification purge above and the digest retention purge (below) go through `periodic`.
- Registers the on-demand printable PDF report surface (`NewReportsApi` at `/api/reports/*`, built right after the notification service): `reportService := services.NewReportService(registry, siteService, notificationService, auditService, userService, roleService, deps.AccessPerms)` gathers Fleet Health / Site & Asset Inventory / Security & Access / Incident Detail from the existing fleet services and renders each through the shared, pure-Go `domain/report` builder (`domain/report/doc.go.md`) — no headless browser, so generation works air-gapped. The security report is superadmin-gated inside the API (`apis/reports.go`'s `requireSuper`), not by the endpoint catalog (no `api_endpoint` row is seeded for `/reports`, unlike `Audit`/`Notifications`); every generation, on every route, is written to the audit trail. See `apis/reports.go.md` and `services/reports.go.md`.
- **Builds the cross-domain correlator** (`services.NewCorrelator`, `services/correlate.go.md`) — THIS is the reason the fourth app exists: `motion on Camera 3 (mymatasan) AND a door contact opening (myiotsan) AND no badge swipe (myiotsan) -> intrusion`. No single node can see that; only the control plane, which already receives every node's events in one feed, is in a position to notice the conjunction. The `nodeKind` resolver passed in is a closure over `registry.List` — the node's kind is always resolved from the **adopted node's own record**, never from anything an event body claims, so a door sensor cannot assert it is a camera and satisfy a camera-scoped clause. Calls `correlator.SetMetrics(deps.Metrics)` right after construction, then `Reload`s the rule cache once at startup (fails boot on error) and registers `apis.NewFleetRulesApi(api, *deps.Auth, controlSession, correlator)`.
- Starts the correlator sweep via `leaderTicker(bgCtx, deps.Leader, time.Second, func(ctx) { correlator.Sweep(ctx) })` (replacing the previous inline 1-second-ticker goroutine) — this is what makes an ABSENCE decidable, since nothing ever arrives to say "the badge was never swiped"; the passage of time has to. **Leader-gated (deployment mode / Phase 1 multi-instance safety)** so an armed rule fires ONCE rather than once per instance — a fired fleet rule raises an alert and can actuate, so a duplicate is not cosmetic. **Phase 1 gap now CLOSED by Phase 3**: each instance used to see events only from the nodes whose control channels terminate on IT, with the armed set living in process memory, so a rule whose clauses spanned nodes attached to DIFFERENT instances never armed at all. The cross-instance event bus (below) now feeds every instance's correlator the SAME raw node events regardless of origin, so the armed set converges across the deployment. See "Cross-instance event bus (Phase 3)" and "Deployment mode / Phase 1+2+3 multi-instance safety" below.
- `onNodeEvent` (passed to `NewControlServer`) now does THREE things per node-pushed frame: `ingestNodeEvent` (unified feed, as before), `observeForCorrelation` (`app/correlate_bridge.go.md`) — the correlator is fed the **node's own event**, deliberately never the control plane's own re-published copy of it, because correlating on our own output would let one fleet rule's alert satisfy another fleet rule's clause and let two rules trigger each other forever — and, new in Phase 3, `services.PublishNodeEvent` to put the same raw event on the shared bus for every OTHER instance's correlator (`services/node_events.go.md`).
- Registers sites + floor plans for the indoor map (`NewSitesApi`, `apis/sites.go.md`), built with `services.NewSiteService(deps.Db, secretCipher, planDir)` where `planDir` is `<dataDir>/floorplans` — floor-plan images are encrypted at rest with the same fleet cipher that protects the CA key/PSK. `NewSitesApi` also now exposes `GET /api/sites/overview` (per-site rollup) and `PUT /api/sites/{id}/position` (drag a site's marker) for the geographic map's digital-twin layer, `GET /api/sites/{id}/floorplans` (multi-node building/outdoor-area drill-down), `POST /api/sites/{id}/areas` (create a floor with a server-generated blank canvas — the asset wizard's per-area step and the building editor's "add an area" button), `PUT /api/floors/{id}/model` (autosave a floor's 3D layout: painted grid + scale + wall height + elevation), and `GET /api/placements` (fleet-wide "what is placed, and where" index the floor editor's palette uses to grey out an already-placed camera — placement is now exclusive, see `entities/node_placement.go.md`). `CreateSite`/`UpdateSite` now also take a `kind` (`building`/`outdoor`/`point`, `entities/site.go.md`'s "Site kinds") that decides how many plans the site can hold and what its map marker looks like.
- `SetRejectTracker` wires the `ControlServer` (once built, further down) into the already-registered `nodesApi` as its `rejectTracker` — the control server exposes `Unrecognized()`/`ForgetRejected()` for stranded (row-less or revoked) node connections, and `NewNodesApi` now returns the handler so this later wiring is possible. See `apis/nodes.go.md` and `services/control_server.go.md`.
- Registers per-node access-grant management (`NewNodeAccessApi`). The node access service is constructed with the roles service (`NewNodeAccessService(db, roleService)`) so superadmin roles receive implicit full node access. All three node APIs (`NewNodeAccessApi`, `NewNodeMediaApi`, `NewNodeProxyApi`) now accept the `controlSession *AccessSessionMidware` so they resolve the caller's live role on every request.
- Starts the node camera media relay: builds a `stream.WebRTCEngine` (from `nodeStream.publicIps` / `nodeStream.udpPort`; nil for same-LAN), starts a `mediarelay.Server` on `pairing.mediaPort` (default `49534`) using the fleet-CA mTLS server config, registers `NewNodeMediaApi` (`POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`, `GET /api/node-stream/config`). The listener goroutine flips `m.mediaListening` (an `*atomic.Bool`) true around `srv.Run(bgCtx)` so `ReadinessStatus` can report `mediaRelay` up/down.
- Registers the reverse command tunnel proxy (`NewNodeProxyApi` at `/api/nodes/{id}/proxy/...`) and, right beside it, `NewRecordingStreamApi` (`/api/nodes/{id}/recording-stream/{segId}`, registered first so its more specific route wins over the proxy's catch-all). Both are constructed at the very end of `RegisterAppRoutes`, once `nodeSender` — the possibly-`ForwardingSender`-wrapped `ControlSender` — exists; see "Node-owner registry + instance forwarding (Phase 2)" below.
- **Federated cross-node search (W2-4, F-10)** — registered right BEFORE `NewNodeProxyApi`, for the same reason: `fleetSearchService := services.NewFleetSearchService(registry, siteService, nodeSender, accessService)`; `apis.NewFleetSearchApi(api, *deps.Auth, controlSession, fleetSearchService, auditService)`. It answers `GET /api/nodes/search`(`/labels`) by scatter-gathering `GET /api/observations/search` and `GET /api/vision/alerts/identities` over every node the caller's role can reach (bounded parallelism 8, 15s per-node deadline), merging newest-first and reporting a `coverage` block (per node, per source: `ok|offline|timeout|denied|unsupported|error`) so an unreachable node is never mistaken for one that saw nothing. Registered **before** `NewNodeProxyApi` so its specific `/api/nodes/search` route is matched ahead of the proxy's `/api/nodes/{id}/proxy/...` catch-all. Audited as `fleet.search`. See `services/fleet_search.go.md`, `apis/fleet_search_api.go.md`. Live-verified against a real two-node fleet (`tools/fleetbench/bench_w24_search.py`, 36/36). **Federated appearance search (W3-2)** — a second method on the same `fleetSearchService` (`AppearanceSearch`, no separate constructor) and a route on the same `NewFleetSearchApi` call (`GET /api/nodes/search/appearance`): a two-hop "fetch the descriptor, then fan it out" search answering "where else did this go?" by ranking recorded sightings that *look like* one the operator picked, rather than by object class. Audited separately as `fleet.search.appearance`. See `services/fleet_appearance.go.md`, `apis/fleet_search_api.go.md`.
- **Fleet configuration policy (W2-1, F-06)** — wired immediately after, for the same reason: `policyService := services.NewFleetPolicyService(deps.Db)`; `policyReconciler := services.NewFleetPolicyReconciler(policyService, registry, nodeSender, auditService, logf)` (the reconciler reads/writes node settings through the same tunnel `nodeSender` gives the operator's own node screens — it gets no private path to an appliance); `apis.NewFleetPolicyApi(api, *deps.Auth, controlSession, policyService, policyReconciler, auditService)`. A leader-gated sweep runs every 15 minutes (`leaderTicker(bgCtx, deps.Leader, 15*time.Minute, ...)` — the same deployment-mode guard as every other scheduled singleton in this file, so N instances never race each other writing the same setting to the same appliance) plus one pass 90 seconds after boot (`safego.Go`, gated on `deps.Leader.IsLeader()`), deliberately delayed rather than immediate because nodes only dial the control channel after `RegisterAppRoutes` returns — an immediate sweep would report the whole fleet unreachable and store that as the last known state. See `services/fleet_policy.go.md`, `services/fleet_policy_reconciler.go.md`, `apis/fleet_policy_api.go.md`. **Built, not yet live-benched** — see `docs/FLAGSHIP_HARDENING_PLAN.md` W2-1.
- **Replay horizon + control-channel drop reporting (W2-6, F-11)** — wired right after
  `NewNodeProxyApi`, for the same reason `ReplayHorizonMonitor` needs `registry` and
  `controlServer.IsConnected` to already exist:
  - `replayHorizon := services.NewReplayHorizonMonitor(registry, controlServer.IsConnected,
    notifReplayWindow, notify)` where `notify` publishes a `notification.Notification` (category
    `health.check`, `Warning` for state `approaching` / `Critical` for `lapsed`, `Source:
    "node:<id>"`) and increments `services.MetricReplayHorizonTotal{state}`. A leader-gated
    `leaderTicker(bgCtx, deps.Leader, 15*time.Minute, ...)` drives the sweep; `apis.NewReplayHorizonApi(api,
    *deps.Auth, replayHorizon)` registers `GET /api/nodes/replay-horizon`. See
    `services/replay_horizon.go.md`, `apis/replay_horizon_api.go.md`.
  - `controlServer.SetDropReportHandler(fn)` — a node admitting, on its next hello, that it could
    not forward events while disconnected (the new `control.Frame.Dropped`,
    `services/control_server.go.md`'s `dispatch`) increments `services.MetricNodeEventsDroppedTotal`
    and publishes an `Info`-level "Node reconnected after dropping events" notification
    (`Source: "node:<id>"`, `Data: {nodeId, dropped}`). The replay that follows is expected to
    recover exactly these events; recording the admission is what makes that expectation checkable
    rather than merely believed.
- **Staged version rollout (W2-5, F-07)** — wired right after: `rolloutService :=
  services.NewFleetRolloutService(deps.Db, registry, nodeSender, controlServer.IsConnected,
  auditService, logf)`; `apis.NewFleetRolloutApi(api, *deps.Auth, controlSession, rolloutService,
  auditService)`. A leader-gated `leaderTicker` drives `rolloutService.Advance` every 30 seconds
  — the same multi-instance-safety guard as every other scheduled singleton in this file, so two
  instances never ask the same appliance to replace its binary twice. Thirty seconds because a
  rollout is the one fleet-wide job where latency IS the product — every tick either asks a node
  to update or judges one already asked — and the deliberate slowness is the settle window
  between rings, not the tick interval. See `services/fleet_rollout.go.md`,
  `apis/fleet_rollout_api.go.md`. **Built, live-benched** against a real two-node fleet
  (`tools/fleetbench/bench_w25_rollout.py`) — see `docs/FLAGSHIP_HARDENING_PLAN.md` W2-5.
- Registers the in-app settings editor (`NewSettingsApi` at `/api/settings/*`) over a SAFE SUBSET of `config.json` (`localAuth`, `sso`, `pairing`, `agent` (new — the fleet AI agent's digest schedule and LLM mode/endpoint/sidecar config, see "Fleet AI agent" below), `security`, `storage`, `logging`; `db`/`server`/`bootstrap` are never exposed) — `myseliasan`'s first in-app settings surface. `settingsService := services.NewSettingsService(deps.Config, deps.ConfigPath, deps.Db, secretCipher, logf)` reads/writes the live `*config.AppConfigModel` + `config.json` directly, since these are infra blocks the shared apphost reads only once at boot; a save/reset always reports `needsRestart: true`. `NewSettingsApi(api, *deps.Auth, controlSession, settingsService, auditService, []string{deps.DataDir, deps.HomeDir})` also passes `browseRoots` — the extra directories (data dir, home dir) the whitelisted server-side file/folder picker (`GET /api/settings/fs/browse`, `services/filesystem_browse.go.md`) may browse beyond its built-in roots, so an operator's `./certs`/`./uploads`/`./logs` under the data dir are reachable from the picker. Also exposes `POST /api/settings/cache/test` (`services.ISettingsService.TestCache`), a live Redis ping against the settings in the request body (blank password/address falls back to the stored value) so an operator can verify Redis connectivity before saving.
- Builds myseliasan's **factory reset** (`sharedservices.NewSystemResetService`, the shared
  `domain/shared/services/system_reset.go` orchestrator — see that doc), right before
  registering `NewSystemApi`. `ConfirmPhrase: m.Name()`; `CollectDataPaths` returns
  `planDir` (floor plans), `apis.ResolveBasemapDir(deps.DataDir, "")` (cached basemap
  tiles), `deps.Config.FileStorage.Path`, and `apphost.ResolveWritablePath(deps.DataDir, "llm")`
  (the AI sidecar's downloaded/imported binaries and GGUF model — a factory-reset control plane
  must not keep a ~1GB model file around; see "Fleet AI agent" below). `BootstrapOpts` passes `m.Migrations()` (must
  not be omitted — see `docs/DB_BOOTSTRAP_SPEC.md`'s baselining note). `KeyStore:
  secretKeyStore` (see the updated `openFleetSecretCipher` above) crypto-erases the fleet
  secret key, so the encrypted CA private key and PSK it protects become unrecoverable.
  `StopServices` stops the pollers and node-facing listeners (`stopBackground()`) before
  the wipe. **Adopted nodes are not told**: dropping the fleet here does not reach out to
  them, so every node keeps running with a certificate this control plane no longer
  recognises and has to be re-adopted — a reset that tried to notify nodes would hang on
  unreachable ones. `api.Use(sharedapis.NewResetGate(systemResetService))` is registered
  right after, so a request against the closed DB pool gets a clean `503` instead of a raw
  `500` once a reset starts. Then `apis.NewSystemApi(api, *deps.Auth, controlSession,
  deps.Restarter, systemResetService)` mounts `/api/system/restart` plus the three reset
  routes. All of `/api/settings/*` and `/api/system/*` self-gate to superadmin in-handler
  regardless of the permission matrix. Hidden unless `bootstrap.allowReset` is true, which
  myseliasan ships **false**. See `apis/settings.go.md`, `apis/system.go.md`,
  `services/settings.go.md`, `services/filesystem_browse.go.md`,
  `docs/modules/domain/shared/services/system_reset.go.md`,
  `docs/modules/domain/shared/apis/system_reset.go.md`.
- Registers the **first-run setup wizard** — a capability myseliasan previously had none of — via `sharedservices.NewSetupStateService(dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db))` and `apis.NewSetupApi(api, *deps.Auth, controlSession, setupStateService)`, right after `NewSystemApi`. Same shared `setup.state` contract mymatasan and myidsan use (`domain/shared/services/setup_state.go.md`, `domain/shared/apis/setup.go.md`). See `apis/setup.go.md` for the route gating and the new `views/components/setup.js` wizard (welcome, **deployment mode**, sign-in, first site, adopt a node, handover, done — `STEP_KEYS` gained `setup.stepDeployment` at index 1).
- Registers **Backup & Restore** via `services.NewBackupService(deps.Db, secretCipher, planDir, setupStateService, backupVersion)` and `apis.NewBackupApi(api, *deps.Auth, controlSession, backupService, auditService)`, immediately after `NewSetupApi`. It is wired here rather than beside the other settings routes because it needs three things assembled by this point: `setupStateService` (a restored instance is already configured and must not be sent back through the first-run wizard), and `secretCipher` + `planDir` (floor plan images are encrypted on disk with the same key as the CA, and are unsealed on export / re-sealed on restore so a bundle can move between hosts with different at-rest keys). `backupVersion` is resolved from `versioning.LoadDefault()` for the manifest, the same way mymatasan resolves `currentVersion` for its self-update service. **This is the only path by which the fleet CA private key leaves the machine** — see `services/backup.go.md`, and note that restoring the `fleetca` section sets `RestoreResult.RestartRequired` because `fleetCA` caches the CA in memory.
- Registers the **built-in user manual** — myseliasan's own adoption of the shared manual library, after mymatasan's — via `apis.NewManualApi(api)`, called on the bare `api` router (no auth middleware) right after `NewSetupApi` so it stays reachable from the sign-in screen and the earliest wizard steps. See `apis/manual.go.md` and `manual/manual.go.md`.
- Wires the **deployment-mode + cluster-readiness checklist** (deployment mode / Phase 1 multi-instance safety): `deploymentModeService := sharedservices.NewDeploymentModeService(runtimeSettingRepo)` and `apis.NewDeploymentApi(api, *deps.Auth, controlSession, deploymentModeService, envFn)`. myseliasan is one of the two apps in the suite (with `myidsan`) genuinely stateless enough to be clustered. `envFn` is rebuilt per request, not captured, so a live Settings-editor change to `cache.provider`/`transaction.lockProvider` is reflected without a restart; `AtRestFingerprint` reports the fleet CA key/PSK's fingerprint (two instances holding different keys look healthy right up until one reads the other's sealed rows, at which point the fleet's trust is simply gone); `ExtraChecks: llmSidecarDeploymentCheck(deps)` appends the LLM-sidecar-memory row (above). See `domain/shared/services/deployment.go.md`, `apis/deployment.go.md`.
- The `ShutdownFunc` cancels the background context (stops heartbeat, control server, and media relay server).

## Fleet AI agent

A deterministic daily (and now optionally weekly) "fleet digest" (always on, pure Go —
`services/agent_digest.go.md` + `services/agent_findings.go.md`) plus an OPTIONAL language-model
layer behind it (digest prose, report executive summaries, an "ask the fleet" chat with
single-node drill-down, `services/agent_chat.go.md`). **The LLM is never in a critical path**:
with `agent.llm.mode` `"off"` (the default) or the model unreachable/crashed/timing out, the
digest still generates from the narrator and every alerting path is untouched. Wired in **two
parts**, since part 1's `digestService` is needed by the report builder (constructed shortly
after) and part 2's `chatService` needs the control server's connectivity oracle (constructed
later still):

**Part 1 — LLM runtime + digest service** (before `NewReportService`):

- `llmDir := apphost.ResolveWritablePath(deps.DataDir, "llm")` — where the sidecar's binaries/model live; also passed into the factory reset's `CollectDataPaths` (above) so a reset erases them too.
- `llmSidecar := services.NewLLMSidecar(...)` (`services/llm_sidecar.go.md`) — the supervised `llama-server` child process, `Enabled` only when `agent.llm.mode == "sidecar"`; `SetOnRestart` wires `MetricAgentSidecarRestartsTotal`; `Start(bgCtx)`.
- `llmManager := services.NewLLMManager(agentCfg.LLM, llmSidecar)` (`services/llm_manager.go.md`) — the one façade `DigestService`/`ChatService`/`apis.NewAgentApi` see for "which client, if any."
- `llmInstaller := services.NewLLMInstaller(llmDir, llmSidecar, func() bool { return agentCfg.AllowDownloads == nil || *agentCfg.AllowDownloads }, logf)` (`services/llm_install.go.md`) — download (pinned + SHA-256-verified, `services/llm_catalog.go.md`, either the `"default"` or `"large"` model tier) or operator-import routes for the sidecar's two artifacts; `SetOnResult` wires `MetricAgentInstallTotal`.
- `digestService := services.NewDigestService(deps.Db, notificationService, registry, auditService, llmManager, func() config.AgentConfigModel { return deps.Config.Agent }, deps.Metrics, logf)` (`services/agent_digest.go.md`) — `cfg` is a getter (not a captured value) so every generation reads the live config block.
- `reportService := services.NewReportService(registry, siteService, notificationService, auditService, userService, roleService, deps.AccessPerms, digestService)` (`services/reports.go.md`) — `digestService` is passed as the report builder's `briefer`, so `FleetHealth`/`Incident` gain an AI executive-summary section built from `digestService.GenerateBriefing`.

**Part 2 — chat service + `/api/agent` surface** (after the reverse command tunnel, once `controlServer` exists):

- `correlator.SetEnricher(services.NewFleetRuleEnricher(notificationService))` (`services/correlate_enrich.go.md`) — right after the correlator is constructed, before `Reload`: appends deterministic recurrence context ("also fired N times this week") to a fired fleet rule's notification.
- `digestService.SetRuleChecker(correlator.HasRuleFor)` — wires the suggested-rule detector's dedup oracle (`services/agent_findings.go.md`'s `suggestedRuleFindings`) once the correlator exists.
- `docsService := services.NewDocsService()` (`services/agent_docs.go.md`) — the manual retriever over both myseliasan's and mymatasan's built-in manuals; indexing is lazy, so it costs nothing until a question is asked and it works with the LLM off (it also serves `GET /api/agent/docs` directly).
- `chatService := services.NewChatService(notificationService, registry, digestService, controlServer.IsConnected, controlServer, docsService, llmManager, deps.Metrics, logf)` (`services/agent_chat.go.md`) — `controlServer.IsConnected` is the grounding bundle's per-node liveness oracle; `controlServer` itself (satisfying `chatNodeSender`) is what lets a question naming one adopted node pull that node's own recent events over the control tunnel; `docsService` is the chat's second grounding source (manual excerpts alongside fleet data).
- `apis.NewAgentApi(api, *deps.Auth, controlSession, digestService, chatService, docsService, llmManager, llmInstaller, llmSidecar, auditService, digestCfg)` (`apis/agent.go.md`) — `digestCfg` is a closure owned by `app.go` (not the API package) that now builds the whole `apis.AgentDigestStatus{Enabled, LocalHour, WindowHours, LastRunDate, WeeklyEnabled, Weekday, LastWeeklyRunDate}` from `deps.Config.Agent.Digest` and **both** persisted runtime-setting rows (`agent.digest.lastRun`/`agent.digest.lastWeeklyRun`, via `runtimeSettingRepo.GetByUnique`), for `GET /api/agent/status`.
- `services.RunDigestSchedule(bgCtx, digestService, runtimeSettingRepo, func() config.AgentConfigModel { return deps.Config.Agent }, deps.Leader.IsLeader, logf)` (`services/agent_schedule.go.md`) — the sleep-until-HH:00-local, fire-once, repeat scheduler for **both** cadences; default hour 07:00, weekly opt-in and defaulting to Monday. **`deps.Leader.IsLeader` is a new trailing gate parameter** (deployment mode / Phase 1 multi-instance safety): the existing per-cadence date-watermark guard (`agent.digest.lastRun`/`lastWeeklyRun`) is a read-then-write with no lock, so two instances waking in the same second both read "not run today" and both generate — and a digest is an LLM call plus an operator-visible artefact, so a duplicate costs real money and real confusion, not just wasted work. Checked at the moment of generating (after the sleep), not at loop start, since the wait is hours long and leadership can move during it.
- `periodic(bgCtx, "myseliasan.purge.digests", 24*time.Hour, leaderOnly(deps.Leader, ...))` — daily stored-digest retention (`agent.digest.retentionDays`, default 180 when `0`) via `digestService.PurgeOld` (applies to both daily and weekly digest rows alike). **Now `leaderOnly`-wrapped** for the same reason as the notification purge above.

See `docs/modules/infra/llm/client.go.md` for the OpenAI-compatible chat-completions client both the sidecar and external modes use, and `apps/myseliasan/README.md`'s "AI Agent" section for the operator-facing feature description (grounding bundle contract, air-gap/download posture, `MYSELIASAN_AI_DOWNLOADS` env lock).

## `ingestNodeEvent`

Maps a node-pushed event frame to the control plane's notification feed. Takes a `*services.RelayDedup` alongside the notification service now, threaded through to every `republishNodeNotification` call:
- `"notification"` — re-published (re-tagged with `nodeId` and a new parent-side ID) via `republishNodeNotification`.
- `"going-offline"` — converted to a system warning notification.
- Any other kind (`health`, `disk-full`, `alert`, `system`, …) is no longer dropped: the frame is parsed as a `notification.Notification` when it carries a `Title`/`Body` (category/severity filled in via `categoryForNodeKind`/`severityForNodeKind` if unset, then also routed through `republishNodeNotification`), otherwise it is wrapped in a generic message (`"Node <kind> event"`, body truncated to 500 chars) tagged with the raw kind and published directly — so a node reporting trouble is never silently lost.
- `categoryForNodeKind` buckets by substring match: `health`/`disk`/`cert` → `CategoryHealthCheck`, `alert` → `CategoryVisionAlert`, else `CategorySystem`. `severityForNodeKind` similarly guesses `Critical` (`alert`/`full`/`critical`/`fail`, now also `duress`/`forced`/`tamper` — the door-alarm vocabulary `mypintusan` raises), `Warning` (`health`/`warn`/`disk`), else `Info`. Both apply ONLY to bare frames without a `Title`/`Body` — the live path (`republishNodeNotification`) preserves the node-authored category and severity, so a door node's duress alarm already arrives `Critical` without this fallback's help; these heuristics are the bucket for a kind nothing more specific parsed.

### `republishNodeNotification` — dedup on the node's engine id

Re-tags a node-originated notification with its origin node (`Source: "node:<id>"`, `Data["nodeId"]`) and lets the parent assign a fresh id in its own feed, exactly as before — but it is now called on **both** the live control-channel push (above) and the reconnect replay pull (below), so before publishing it checks `RelayDedup.SeenOrRecord(ctx, nodeID, n.ID, n.CreatedAt)` keyed on `n.ID`, the node's stable engine id (`infra/notification.Notification.ID`) which is identical on both paths. A hit means the event was already ingested (live or an earlier replay) and `republishNodeNotification` returns `false` without publishing; a miss records the marker and publishes, returning `true`. An empty `n.ID` (an older node build predating this feature) cannot be deduped and is always published — the accepted cost is a rare duplicate, not a dropped event.

## Replay on reconnect

The live push above only carries a notification while the node's control channel is up; one raised during a disconnect was previously dropped with no backfill, silently undercounting a busy node's feed. `controlServer.SetOnConnect(fn)` is wired to a closure over `replayNodeNotifications` (defined alongside `ingestNodeEvent` in `app.go`), invoked by `ControlServer` (in its own goroutine, see `services/control_server.go.md`) the moment a node's connection is (re)accepted:

- `replayNodeNotifications(sender, svc, dedup, nodeID, logf)` pulls `GET /api/notifications?since=<cursor>&limit=500` from the node over the control tunnel (`ControlSender.SendRequest`, 60s overall timeout), starting `cursor` at `time.Now().Add(-notifReplayWindow)` (`notifReplayWindow = 72 * time.Hour`) and paging forward (cursor advances to the last row's `CreatedAt`) up to a hard cap of 50 pages (25k events) per reconnect. **W2-6/F-11**: hitting that cap used to `break` silently — a replay that recovered only a prefix reported itself exactly like one that recovered everything. It now logs (`replay from node %s hit its %d-page ceiling after %d event(s) — older missed events were NOT recovered`) when `pagesUsed >= maxPages`, and says so deliberately in the past tense: the remainder is not coming back on a later reconnect either, since the next replay starts from the same `notifReplayWindow`-wide cursor, not from where this one stopped.
- `parseNodeNotifRows` tolerates both the plain `{result:{items}}` envelope and a wrapped `{data:{result:{items}}}` response.
- `nodeRowToNotification` rebuilds an in-memory `notification.Notification` from a pulled row, restoring the node's engine id out of the row's `metadata.__oid` (`domain/notification.OriginIDKey`) as `n.ID` — this is what lets a pulled row dedup against a live-pushed one via the identical `RelayDedup` check in `republishNodeNotification`, which every replayed row is routed back through.
- A node offline or with nothing new in the window is a cheap no-op (first `SendRequest` fails or returns zero rows). A non-2xx response or transport error aborts the pull for that reconnect; it will be retried on the node's next reconnect.
- An hourly background task (`leaderTicker(bgCtx, deps.Leader, time.Hour, ...)`, `app.go`, started alongside the control server, replacing a previous inline ticker goroutine) prunes `RelayDedup` markers older than `2 * notifReplayWindow` — a windowed pull can never reach back that far, so an older marker is dead weight. **Leader-gated (deployment mode / Phase 1 multi-instance safety)**: it is a whole-table cleanup, and N instances deleting the same rows only multiplies the work; standalone the gate is a no-op. See `services/relay_dedup.go.md`.
- **W2-6/F-11**: this whole recovery mechanism has an expiry date that nothing used to watch — a
  disconnect longer than `notifReplayWindow` loses events for good, and the fleet screens said
  "lost" identically at hour 2 and hour 90. `services.ReplayHorizonMonitor` (see "Replay horizon +
  control-channel drop reporting" above and `services/replay_horizon.go.md`) now warns as a node's
  disconnect approaches, then passes, that point.

This requires the node's own `GET /api/notifications` to accept the replay pull — see `apps/mymatasan/apis/notification.go`'s `since=` handling and `INotificationService.ListSince` (`apps/mymatasan/services/ifaces.go.md`), and the equivalent `since=` param on `myiotsan` (`apps/myiotsan/apis/notifications.go`, `docs/modules/apps/myiotsan/apis/notifications.go.md`); a node build that predates the `since=` param ignores the query param and returns its normal newest-first page, so the replay pull would ingest the wrong window from an old node — deploying both halves together is required (see the versioning entry for this feature).

## Node-owner registry + instance forwarding (Phase 2)

Phase 1 (above) made N myseliasan instances SAFE; it left two documented gaps (see "Deployment
mode / Phase 1+2+3 multi-instance safety" below). Phase 2 closes the first one and half of the
second: node liveness is now correct across instances, and node COMMANDS (a node's own screens,
its settings, recording playback) reach the node regardless of which instance the browser request
landed on. Phase 3 (below, "Cross-instance event bus") closes the rest of the second gap (fleet
rules) and, incidentally, the live notification feed.

- `clusterCfg := deps.Config.Cluster` (`ClusterConfigModel`, `infra/config/config_models.go.md`).
  `nodeOwners := services.NewNodeOwnerRegistry(deps.Cache, clusterCfg.AdvertiseURL,
  time.Duration(clusterCfg.OwnershipTTLSeconds)*time.Second, logf)` — built unconditionally
  (`services/node_owner.go.md`); with an empty `advertiseUrl` (the default) it is disabled and
  every method behaves as "only local connections exist," so this is a no-op for a standalone
  install. `nodeOwners.StartRenewal(bgCtx)` runs regardless (also a no-op when disabled). An
  `Infof` line is logged once at boot when enabled, naming the advertised URL.
- `controlServer.SetOnConnect` now claims ownership (`nodeOwners.Claim`) **before** the reconnect
  replay pull, so from the moment a node connects its commands can be served here and the rest of
  the deployment learns that as early as possible.
- `controlServer.SetOnDisconnect(func(nodeID) { nodeOwners.Release(...) })` (new — see
  `services/control_server.go.md`) withdraws the claim the instant a connection tears down, so
  another instance can take the node over immediately instead of waiting out the ownership lease.
- `registry.SetControlPresence(func(nodeID) bool { return nodeOwners.ConnectedAnywhere(ctx, nodeID) })`
  — **the false-lost fix**. Previously this was `controlServer.IsConnected`, a purely local answer;
  the heartbeat reconciler (leader-gated, above) now sees a node as online if ANY instance holds its
  control channel, not just the leader. Standalone the two answers are identical.
- `nodeSender := services.ControlSender(controlServer)` by default; when `nodeOwners.Enabled()`, it
  is replaced with `services.NewForwardingSender(controlServer, nodeOwners, peerClient, logf)`
  (`services/node_peer.go.md`), where `peerClient := services.NewPeerClient(deps.Config.Jwt.Secret,
  time.Duration(clusterCfg.ForwardTimeoutSeconds)*time.Second, clusterCfg.InsecureSkipVerify)`. Both
  `apis.NewRecordingStreamApi` and `apis.NewNodeProxyApi` (registered at the very end of
  `RegisterAppRoutes`) are now passed `nodeSender` instead of the bare `controlServer` — since both
  already depended only on the narrow `ControlSender` interface, this one decorator makes both
  cluster-aware without either learning that instances exist.
- When `nodeOwners.Enabled()`, also builds `peerHandler := services.NewPeerForwardHandler(deps.Config.Jwt.Secret, controlServer, logf)`
  (given the **local** `controlServer`, never `nodeSender` — see `node_peer.go.md`'s "terminal hop"
  note) and mounts it at `api.Handle(services.PeerForwardPath, peerHandler).Methods("POST")`
  (`POST /api/internal/cluster/node-forward`) — deliberately outside the session-auth middleware,
  since its caller is a peer instance authenticating with a derived token, not a signed-in user.
  This route only exists when clustering is configured.
- **Not covered by this section**: live camera video (`NewNodeMediaApi`) does not use
  `nodeSender`/`ForwardingSender` at all — the media channel is a separate connection from the
  control channel, tracked by its own `MediaOwnerRegistry`, and forwarded by a parallel hop. See
  "Cross-instance media forwarding (Phase 4)" below; at the time Phase 2 shipped this WAS a real
  gap (viewing a camera on a node attached to another instance failed), and the note here has been
  corrected now that Phase 4 closes it.

## Cross-instance event bus (Phase 3)

Phase 2 made node liveness and node COMMANDS correct across instances. Phase 3 closes the
remaining Phase 1 gap — fleet rules whose clauses span nodes on different instances — and,
as a side effect of the same wiring, makes the live notification feed (SSE) span every
instance too, not just the one a browser happens to be connected to.

- Built right after `relayDedup`, before `onNodeEvent` is defined: `nodeEventBus,
  busProvider, busErr := eventbus.New(eventbus.Config{...})`
  (`infra/eventbus/bus.go.md`), with `Provider:
  deps.Config.Cluster.EventBusProvider(deps.Config.Cache.Provider)` — the bus follows the
  cache provider by default (`infra/config/config_models.go.md`'s
  `ClusterConfigModel.EventBusProvider`) — and the rest of the fields mirrored from
  `deps.Config.Cache.Redis`. `busErr != nil` fails boot (an unrecognised provider name is
  an error, never a silent fall-back to in-process — see `bus.go.md`'s `New`). When
  `nodeEventBus.Distributed()`, `Ping` is called once and a failure also fails boot (an
  unreachable configured Redis must not boot into a cluster that looks configured but
  delivers to nobody); an `Infof` line names the resolved provider on success.
- `instanceID := services.NewInstanceID(deps.Config.Cluster.AdvertiseURL)` — this
  instance's publisher identity, reused from the same `cluster.advertiseUrl` Phase 2 already
  requires an operator to set per instance, so there is no new value to configure for this
  half of clustering either.
- `notificationService.Register(services.NewNotificationRelayChannel(nodeEventBus,
  instanceID, busLog))` — every notification this instance publishes (a node's, a node-lost
  alert the heartbeat raised, an anomaly, the morning digest — the hub already invokes every
  registered channel on every publish, which is exactly the set that should be relayed) is
  now also put on the bus's `notifications` topic for the other instances.
- `services.SubscribeNotifications(bgCtx, nodeEventBus, instanceID, func(n) {
  notificationService.RelayToStream(context.Background(), n) }, busLog)` — the receiving
  half: a notification relayed from another instance is pushed straight to THIS instance's
  live SSE subscribers via `domain/notification.Service.RelayToStream`
  (`docs/modules/domain/notification/service.go.md`), deliberately never persisted again
  (the origin already wrote the row) and never re-published through the hub (which would
  loop it back onto the bus). **This is what makes the SSE feed span the deployment**: a
  browser subscribed to any instance now sees every notification any instance raises, not
  just the ones its own instance happened to ingest.
- `onNodeEvent` additionally calls `services.PublishNodeEvent(context.Background(),
  nodeEventBus, instanceID, nodeID, kind, body, busLog)` after `observeIfLeader` — the
  raw node event, never the control plane's own re-published notification (see the
  correlator-sweep note above for why), goes onto the bus's separate `node-events` topic.
- `services.SubscribeNodeEvents(bgCtx, nodeEventBus, instanceID, func(ev) {
  observeIfLeader(ev.NodeID, ev.Kind, ev.Body) }, busLog)` — the receiving half for
  correlation: a raw event published by ANOTHER instance is fed into THIS instance's
  correlator exactly as if it had arrived on this instance's own control channel. This is
  what lets a fleet rule whose clauses span nodes attached to different instances finally
  arm.
- **`observeIfLeader` (review-round fix, both call sites above) wraps `observeForCorrelation`
  with `deps.Leader.IsLeader()` and returns without observing when false.** Arming and firing
  must live on the SAME instance: the sweep is what both arms a rule's absent-clause timer
  AND clears it once the grace window passes, and that sweep is already leader-gated (see the
  correlator-sweep note above), so a follower that kept observing would accumulate `armed`
  state it could never sweep — and fire a backlog of stale correlations the moment it was
  promoted. A promoted instance now starts with an empty `armed` set and rebuilds it from live
  events; the worst case is a correlation spanning the exact moment of a leadership change
  being missed, not a burst of stale alerts. Standalone, the gate is a no-op (the single
  instance is always leader).
- Standalone (the default in-process `MemoryBus`), `Distributed()` is `false`, so every
  publish/subscribe call above is a no-op — this wiring costs nothing and changes nothing
  for a single instance. See `services/node_events.go.md` for the message shapes and echo
  suppression (`Origin == instanceID` is dropped, or every event/notification would be
  handled twice at its origin).

## Cross-instance media forwarding (Phase 4)

Phase 2 made node COMMANDS (settings, recording playback) cluster-aware; live camera video was
explicitly left uncovered because a node's camera RTP arrives on a SEPARATE connection — the media
channel — which, like the control channel, terminates on exactly one instance. Phase 4 closes that
gap by forwarding the WebRTC NEGOTIATION (not the media itself) to whichever instance holds it.

- `mediaOwners := services.NewMediaOwnerRegistry(deps.Cache, clusterCfg.AdvertiseURL,
  time.Duration(clusterCfg.OwnershipTTLSeconds)*time.Second, logf)` (`services/node_owner.go.md`)
  — built right after the media listener goroutine starts, reusing the same `cluster.*` settings
  Phase 2 already requires (`ownershipTtlSeconds`), so there is nothing new to configure.
  `mediaOwners.StartRenewal(bgCtx)` runs regardless (no-op when disabled).
- `mediaHub.SetOwnershipHooks(func(nodeID) { mediaOwners.Claim(...) }, func(nodeID) {
  mediaOwners.Release(...) })` (`services/media_relay.go.md`) — claims/releases the media-channel
  ownership the same way `controlServer.SetOnConnect`/`SetOnDisconnect` already do for the control
  channel, under a SEPARATE cache-key namespace (`node_owner.go.md`'s `mediaOwnerKeyPrefix`) so a
  node's two channels are tracked independently.
- `clusterPeer` (`services.PeerClient`, built once earlier alongside `nodeOwners` — see Phase 2
  above, and now explicitly shared by both hops) is reused for the media hop too: one derived
  token, one HTTP client, for every instance-to-instance call this app makes.
- `mediaForward func(ctx, services.MediaOfferRequest) (services.MediaOfferReply, error)` — built
  only when `mediaOwners.Enabled() && clusterPeer != nil`; resolves the node's media owner and
  calls `clusterPeer.ForwardMediaOffer` (`services/media_peer.go.md`). `nil` on a standalone
  install or when clustering is off, exactly like Phase 2's `nodeSender` decorator.
- `apis.NewNodeMediaApi(...)` now takes `mediaForward` as its last argument and returns the built
  `*nodeMediaApi` (`apis/node_media.go.md`) — needed so its `AnswerLocalOffer` method can be handed
  to the receiving side below.
- When `mediaOwners.Enabled()`: `api.Handle(services.PeerMediaOfferPath,
  services.NewPeerMediaOfferHandler(deps.Config.Jwt.Secret, mediaApi.AnswerLocalOffer, logf))`
  mounts `POST /api/internal/cluster/media-offer` — outside session auth, same convention as
  Phase 2's `PeerForwardPath`. This is the receiving half: another instance's forwarded offer is
  negotiated here using THIS instance's own `mediaHub`/`mediaEngine`, and authorization is
  deliberately not repeated (the forwarding instance already resolved it against the operator's
  live session).
- **The video itself never crosses this hop.** Only the SDP offer/answer negotiation is forwarded;
  the answer carries the owning instance's own ICE candidates, so the browser's WebRTC peer
  connects DIRECTLY to that instance. This is why the operator checklist (setup wizard, Settings
  Deployment panel) has always required each instance to have its own reachable address and UDP
  port for live video — that requirement predates this change and Phase 4 is what makes it
  actually necessary rather than aspirational.

### Fleet-rule reload propagation

Separately from the media hop, this section of `app.go` also wires
`correlator.SetOnRulesChanged(func() { services.PublishRulesChanged(...) })` and
`services.SubscribeRulesChanged(bgCtx, nodeEventBus, instanceID, func() { correlator.Reload(...)
}, busLog)` (`services/node_events.go.md`'s `RulesChangedTopic`), so an operator's fleet rule edit
— which lands on and reloads only the instance that served the request — also tells every other
instance to reload its own rule cache. `Correlator.Save`/`Delete` (`services/correlate_crud.go.md`)
each call `announceRulesChanged` right after their own `Reload` succeeds, so this is reachable
end-to-end: editing or deleting a rule on any instance reloads every other instance's cache too,
not just the one that served the request. This is unrelated to, and complements, Phase 3's
fleet-rule fix (a rule whose CLAUSES span nodes on different instances arming correctly via the
`node-events` topic) — that fix is about event correlation; this one is about the rule DEFINITION
itself staying in sync after an edit.

Guarded by a source-level regression test (`correlate_announce_test.go`,
`services/correlate_crud.go.md`) rather than a behavioral one, because of how this exact call went
missing once already during development: it compiled, every other test passed, and the feature
was silently absent, because Go does not flag an unused method. Worth keeping in mind generally —
"wired up" (a callback registered, a subscriber listening) is not the same as "called", and where
a feature's failure mode is silence rather than an error, a test has to assert on the call site
itself, not just on some effect that happens to be observable downstream.

### Verification status

Unit-tested at the media hop's own seam (`media_peer_test.go`, 8 cases — forwarding + answer
round-trip, 401 on missing/wrong token, not-connected propagation, payload validation, unreachable
owner, and that control-channel and media-channel ownership are tracked and released
independently). **Not exercised end-to-end with real cameras**: the live two-instance bench used a
`mypintusan` door controller, which has no cameras, so cross-instance live video has not been
watched in an actual browser.

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

## Deployment mode / Phase 1-4 multi-instance safety — what is actually safe now, and what is not yet

Phase 1 made N myseliasan instances SAFE (no double-counted rollups, no duplicate purges/digests, a
single writer of node status). Phase 2 then made two of the fleet legs CORRECT across instances too:
node liveness and node commands. Phase 3 closes the remaining Phase 1 gap and, as a side effect of
the same event bus, fixes the live notification feed too. Phase 4 (above) closes the last
documented gap, live camera video:

1. ~~**Heartbeat/liveness**~~ — **FIXED in Phase 2.** `registry.SetControlPresence` now consults the
   node-owner registry's `ConnectedAnywhere` (a deployment-wide lookup), not a local connection map,
   so a node attached to another instance is correctly reported online rather than eventually marked
   `lost`.
2. ~~**Fleet rules**~~ (correlator.Sweep) — **FIXED in Phase 3.** Every ingested node event is now
   published to every instance over the shared event bus (`services/node_events.go.md`) and fed into
   each instance's own correlator, so a rule whose clauses span nodes attached to different instances
   arms and fires correctly instead of going quiet. The leader-only guard (Phase 1) still ensures it
   fires once, not once per instance. (Phase 4, above, separately closes the related gap of a rule
   EDIT/DELETE announcing itself cross-instance — see "Fleet-rule reload propagation" under Phase 4
   above; that one is about keeping the rule DEFINITION in sync, not event correlation.)
3. ~~**Live notification feed (SSE)**~~ — **FIXED in Phase 3, incidentally.** The same event bus
   carries every notification an instance publishes to the others' live SSE streams
   (`domain/notification.Service.RelayToStream`), so a browser subscribed to any instance now sees
   an event ingested by another, not just its own.
4. ~~**Live camera video**~~ — **FIXED in Phase 4.** A browser's WebRTC offer for a node attached to
   another instance is forwarded to the instance holding its media channel, which negotiates the
   answer and returns it verbatim; the video itself still flows browser-to-owning-instance directly,
   never through the forwarding instance or the load balancer. See "Cross-instance media forwarding
   (Phase 4)" above — including its "verification status" note (unit-tested, not yet bench-verified
   with a real camera).

Still open:

- **The Settings editor** (`apis/settings.go.md`) writes THIS instance's `config.json` and restarts
  only THIS instance — an operator changing a setting on a clustered install must repeat it (or ship
  it) on every instance.

What Phase 2 fixed: the node proxy (`/api/nodes/{id}/proxy/...`) and recording-stream playback no
longer require the request to land on the instance a node's control channel happens to be connected
to — see "Node-owner registry + instance forwarding (Phase 2)" above. This requires
`cluster.advertiseUrl` to be set on every instance; with it empty (the default) this section's
behavior is unchanged from Phase 1.

What Phase 3 fixed: fleet-rule correlation and the live SSE feed both now span every instance,
regardless of which node/browser reaches which instance — see "Cross-instance event bus (Phase 3)"
above. This requires the event bus provider (`cluster.eventBusProvider`, defaulting to
`cache.provider`) to actually reach every instance, i.e. Redis in production; with the default
in-process provider (correct for one instance) this section's behavior is unchanged.

What Phase 4 fixed: live camera video across instances, and fleet-rule edits announcing themselves
across instances — see "Cross-instance media forwarding (Phase 4)" and "Fleet-rule reload
propagation" above. Both require the same `cluster.advertiseUrl`/`clusterPeer` setup Phase 2
already needs; with clustering off this section's behavior is unchanged.

Standalone behavior is unchanged throughout: with the per-process (memory) lock provider, the
single instance always holds the leader lease and every `leaderOnly`/`leaderTicker`/`WithGate`
guard above is a no-op; with an empty `cluster.advertiseUrl`, the node-owner/media-owner registries
and `ForwardingSender`/`mediaForward` are both no-ops too; with the default in-process event bus,
every Phase 3/4 publish/subscribe call is a no-op too.

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
- Adopted nodes now carry a `Kind` (`"camera"` / `"iot"` / `"door"`; empty = camera, for pre-existing nodes) on `ManagedNode`, set from the node's own fleet-key-signed adopt reply — see `entities/managed_node.go.md` and `services/node_registry.go.md`. The unsigned discovery-announce `Kind` hint (`DiscoveredNode.Kind`) is display-only and never trusted for adoption or correlation. `"door"` is `mypintusan`, the fifth appliance to join the fleet.
- `RegisterWebRoutes` resolves `static/index.html` from `deps.HomeDir`, not `m.BaseDir()`. `BaseDir()` is the dev-mode CWD-relative path (`apps/myseliasan`); a packaged install runs with the binary and `static/` side by side and the service's working directory pointed elsewhere, so the old `BaseDir()`-relative lookup 404'd on `/` and `/index.html` outside a dev checkout. apphost's shared SPA catch-all already resolves off `HomeDir`; this now matches it.
