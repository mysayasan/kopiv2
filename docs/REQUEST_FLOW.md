# Request and Runtime Flow

## HTTP Request Path

1. Client hits server at one of the configured runtime listeners (`server.hostnames x server.tlsPorts/server.nonTlsPorts`, optionally overridden by `SERVER_HOSTNAMES`, `SERVER_TLS_PORTS`, and `SERVER_NON_TLS_PORTS`).
2. Router (`gorilla/mux`) matches route.
3. Global middleware executes:
   - security-headers middleware (nosniff, X-Frame-Options, Referrer-Policy, HSTS on TLS, opt-in Content-Security-Policy, and strips the `Server` header) — runs first so every response, including auth 401s, rate-limit 429s, static assets, and the setup page, is hardened
   - CORS middleware
   - request log middleware (adds/propagates `X-Request-ID` and writes through runtime logger)
4. For `/api/*` routes:
   - API activity log middleware records the completed request into `api_log`, including elapsed `durationMs`.
   - API telemetry records request count, duration histogram, and slow-request metrics when enabled.
   - rate-limit middleware classifies the API endpoint tier (`0=DevOnly`, `1=AuthOnly`, `2=Public`) and applies config-driven sliding-window limits.
   - routes that opt into auth read the HttpOnly session cookie, validate the JWT, and inject claims into context.
   - unsafe JWT-authenticated methods (`POST`, `PUT`, `PATCH`, `DELETE`) must send `X-CSRF-Token` matching the readable CSRF cookie.
   - auth middleware validates signed JWT, configured issuer/audience, and cache-backed SSO session when a `sid` claim is present.
   - accessrbac middleware (`AccessSessionMidware`) enforces the app's own permission matrix (per-role endpoint-prefix table, longest match) when the route uses the shared accessrbac layer. Superadmin roles bypass the matrix; disabled or must-change-password users are rejected with 403.
   - standalone appliance apps (`mymatasan`, `myiotsan`) use app-local Basic Auth (or, for `myiotsan`, a session cookie issued by an explicit login endpoint) instead of MyIDSan JWT/accessrbac, then a shared `NewRequireRolePermission` middleware authorizes every request against the signed-in user's role, built on the shared accessrbac role/permission data model (not the shared session middleware, which requires JWT claims these apps do not have) — see `domain/shared/apis/local_auth.go.md` and `domain/shared/apis/authorization.go.md`.
5. Handler decodes payload, calls service, and writes response.

Shared JSON response helpers include `durationMs`, measured from request middleware start time to response serialization.

## Health and Readiness Flow

- `GET /health`: immediate alive response.
- `GET /ready`: performs DB and cache pings with timeout (`2s`), reports up/down.
- `GET /api/ready`: same handler, mirrored under `/api` so it is reachable over a reverse control tunnel (e.g. myseliasan's parent→node channel, which only dispatches against the `/api` subrouter) — lets a control plane surface a managed node's readiness in its own UI.
- An app module implementing `apphost.ReadinessReporter` contributes extra **advisory** fields merged into the `/ready`/`/api/ready` payload — they never flip `ok`/HTTP status, which stays gated on db + cache only. `mymatasan` reports `machine`/`cameras` host and camera health; `myseliasan` reports its fleet-listener health: `controlChannel` (up/down), `connectedNodes` (count), `mediaRelay` (up/down) — see `docs/modules/apps/myseliasan/app/app.go.md`.
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

Bootstrap seeding ensures the default `system` group and `superadmin` login exist. The shared accessrbac core (`EnsureBuiltins`) seeds `superadmin` and `viewer` roles on startup for apps that enable `AccessRbac`. The `superadmin` login's `UserRoleId` is repointed to the accessrbac superadmin role at startup so the bootstrap account bypasses the permission matrix.
Endpoint rows with `accessTier` metadata are seeded for rate-limit classification. Per-endpoint RBAC rows are no longer seeded; authorization is the accessrbac matrix managed via `/api/access-rbac`.

`myidsan` uses this same bootstrap flow to seed the identity-provider management surface, app registry, SSO fallback endpoints, and selected relying-app policies. It is the cross-app sign-on authority; authorization decisions now stay local to each app's accessrbac middleware. `mymatasan` is standalone: it seeds only local endpoint metadata for rate-limit classification and app bootstrap, mounts public version plus app-local ONVIF and vision routes, and protects those app-local routes with Basic Auth from local users, authorized by its own role permission matrix (`admin`/`operator`/`viewer`, seeded from `apps/mymatasan/services/rbac.go`'s `Policy()` — see `docs/MYMATASAN_TIER2_PLAN.md` Phase R). `myseliasan` registers the shared accessrbac entities and seeds its own stock superadmin user on startup via `EnsureStockSuperadmin`.

## Browser SSO Callback Flow

1. A browser opens a relying app such as `myseliasan`.
2. Without a local relying-app session, the app redirects to MyIDSan `/api/auth/authorize` with `client_id`, `audience`, exact `redirect_uri`, and state.
3. MyIDSan validates the client config and redirect URI allow-list. If the browser has no MyIDSan session, it serves `/api/auth/login` — the form now shows Google/GitHub buttons alongside the local login form when those providers are configured. The `continue` query parameter carries the authorization URL through any social login round-trip so the user lands back at step 4 automatically.
4. MyIDSan redirects the browser back to the relying-app callback with a short-lived one-time code and state.
5. The relying app validates state, then performs a backend HTTPS `POST` to MyIDSan `/api/auth/token` with its client secret.
6. For HTTPS token exchange, the relying app uses the OS trust store plus optional `sso.caCertPath`/`SSO_CA_CERT_PATH`. This trusts private CA bundles without disabling hostname, expiry, or chain validation.
7. After a valid token response, the relying app issues its own HttpOnly session cookie and redirects the browser to its app root.

## Node Camera WebRTC Relay Flow (myseliasan → browser)

This flow is for a browser live-viewing a camera attached to an adopted `mymatasan` node, where the browser has no direct path to the node.

1. The node dials the parent's media channel (`wss://parentHost:mediaPort/media`) over fleet mTLS and maintains a persistent connection (`MediaChannelManager.Run`).
2. The browser calls `GET /api/node-stream/config` to obtain the ICE server list (STUN/TURN) configured for the parent's WebRTC leg.
3. The browser creates a local WebRTC peer, generates an SDP offer, and POSTs it to `POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`.
4. The API handler verifies the caller has at least viewer access to the node (against the operator's live session, on the instance the browser reached). **On a clustered deployment**, if this instance is not holding the node's media channel, it forwards the decoded offer to the instance that is (`POST /api/internal/cluster/media-offer`, authenticated by a token derived from `jwt.secret`) and returns that instance's answer verbatim to the browser — skip to step 8, since the negotiation in steps 4-7 below then happens on the OWNING instance instead, and the video flows browser-to-owning-instance directly rather than through this one. Otherwise it confirms the node's media channel is connected here and builds a per-request `stream.Manager` backed by `MediaRelayHub.Connector(nodeID)`.
5. `CreateWebRTCAnswerWithOptions` calls `relayConnector.Subscribe(source)`, which allocates a `streamID`, sends `FrameStart` down the media channel to the node, and waits for `FrameMeta` (codec info, up to 15 s timeout).
6. The node receives `FrameStart`, subscribes the camera's RTSP stream, sends `FrameMeta`, replays the GOP backlog (`FrameBacklog`), then pumps live RTP as `FrameVideoRTP` / `FrameAudioRTP` frames.
7. The API handler builds H264 video and optional audio tracks, answers the SDP offer, and starts pumping the relayed RTP packets into the browser's WebRTC peer. The SDP answer is returned to the browser (directly, or relayed back through the forwarding instance from step 4).
8. The browser's WebRTC peer connects (using the configured ICE/TURN if needed) — to the OWNING instance's advertised address when forwarded, so the media itself never traverses a second instance or the load balancer — and renders live video.
9. When the browser disconnects, `Subscription.Close` triggers `relayConnector`'s `stopStream`, which sends `FrameStop` to the node and stops the RTSP subscription.

**Data path summary:** camera → RTSP → mymatasan (RTP) → media channel (binary WebSocket, fleet mTLS) → myseliasan (MediaRelayHub) → WebRTC (pion) → browser. See `docs/modules/apps/myseliasan/services/media_peer.go.md` and `docs/HOWTO.md`'s "Live camera video across instances (myseliasan, Phase 4)" for the cross-instance forwarding hop; standalone or single-instance deployments are unaffected (the forwarding branch in step 4 never triggers).

## Recording Playback over the Control Tunnel Flow (myseliasan → mymatasan)

This flow is for a browser playing/seeking a recorded clip that lives on an adopted node,
through myseliasan's embedded node camera pages. The control channel that carries tunneled
commands caps each message at 16 MiB (`infra/control.maxFrameBytes`) and can't itself seek a
clip end-to-end, so playback is chunked instead of proxied as a single request/response.

1. The embedded Recordings tab (`nodecam/recording.js`, reused unmodified from `mymatasan`)
   detects it is running inside myseliasan's commander proxy and points the `<video>` `src`
   at `GET /api/nodes/{id}/recording-stream/{segId}` instead of the node's own
   `.../download` URL.
2. `apps/myseliasan/apis/recording_stream.go` authorizes the caller exactly like the command
   proxy (live role via `INodeAccessService.Resolve`), parses the browser's `Range` header,
   and caps the requested span to `recordingStreamChunk` (8 MiB) — comfortably under the
   16 MiB control-channel frame cap.
3. It builds a tunneled `control.Request` for the node's
   `GET /api/recording/segments/{segId}/download` with the capped `Range` header (forwarding
   `?transcode=h264` when the browser can't decode HEVC) and sends it via
   `ControlSender.SendRequest`.
4. On the node, `apps/mymatasan/apis/recording.go`'s `downloadSegment` sees the `Range` header
   and serves it via `http.ServeContent`, which needs a seekable source: a plaintext,
   non-transcoded segment is opened directly; an encrypted and/or HEVC-transcoded segment is
   first materialized to a plaintext (optionally H.264) temp copy (`segmentPlayFile`, cached
   under `os.TempDir()/mymatasan-playcache`, reused on later requests, swept after 1h) so it
   can be range-served too.
5. `apps/mymatasan/apis/control_dispatch.go` forwards **all** response headers (not just
   `Content-Type`) back through the tunnel, so `Content-Range`/`Accept-Ranges`/
   `Content-Length` survive the round trip.
6. `recording_stream.go` requires the node to have answered `206 Partial Content`; anything
   else (an older node without Range support, or a segment that couldn't be made seekable) is
   surfaced to the browser as a "not streamable over the link" error rather than a full-clip
   fallback. On success it forwards the relevant headers and writes `206` with the chunk body.
7. The browser's `<video>` element repeats step 2–6 for each subsequent byte range it needs
   (playback continuation or a seek), so an arbitrarily large clip is played/sought without
   any single control-channel message exceeding its cap.

**Data path summary:** browser `<video>` Range request → myseliasan (`recording_stream.go`,
capped to 8 MiB) → control channel (`control.Request`, fleet mTLS) → mymatasan
(`downloadSegment`, decrypt/transcode/materialize as needed) → control channel
(`control.Response`, all headers) → myseliasan (`206 Partial Content`) → browser.

## Embedded IoT Node Management Flow (myseliasan → myiotsan)

Mirrors the recording-playback flow above, but for the general case of managing an adopted
`myiotsan` "Sensor hub" node's devices/rules/alerts/commands from inside myseliasan's UI
(`components/nodeiot/`, routed by `node_manager.js` on `kind === 'iot'`) rather than one
specialized range-proxied endpoint.

1. The embedded page's `apiBase()` is overridden (the same shim `nodecam/` uses) to point every
   call at `GET/POST/PUT/DELETE /api/nodes/{id}/proxy/<node-path>` instead of a same-origin URL.
2. `apps/myseliasan/apis/node_proxy.go` authorizes the caller against the per-node access grant
   (viewer/operator/admin), builds a tunneled `control.Request` for `<node-path>`, and sends it
   over the node's control channel.
3. On the node, `apps/myiotsan/apis/control_dispatch.go` (shared dispatcher, `domain/shared/apis`)
   routes the request into the node's own `/api` subrouter exactly as if it had arrived locally,
   and its response is tunneled back unchanged.
4. **Issuing a command or acknowledging an alert this way is attributed by name, not just id.**
   The control-plane caller has no local account on the node, so the node-side `actorId(r)`
   resolves to `0`; the synthetic principal the tunnel presents instead carries the caller's name
   as `cp:<who>`, read by `apis.actorName(r)` and stamped onto
   `DeviceCommand.RequestedByName` / `AlertEvent.AckedByName` (`apps/myiotsan/apis/devices.go`,
   `services/{commands,rules}.go`). Without this, the node's own audit trail — the first place an
   investigator looks — would say a relay was switched or an alert acknowledged by "System".

The browser never opens a connection to the node itself at any step — everything above travels
browser → myseliasan → control channel → node, the same as every other embedded-node flow in this
document, which is what lets an adopted node sit behind NAT with no inbound firewall rule.

## Cross-Domain Fleet Rule Correlation Flow (myseliasan)

This flow is why the suite has a fourth app. A `mymatasan` camera node and a `myiotsan` sensor
node cannot see each other's events; `myseliasan`, which already receives both over the fleet
control channel, can correlate across them.

1. A node (camera or sensor) raises a notification locally and forwards it up its control
   channel (`fleetnode.NewControlEventSink` → `ControlChannelManager.ForwardEvent`, see
   `docs/modules/domain/shared/fleetnode/doc.go.md`).
2. `myseliasan`'s `ControlServer` receives the event frame and invokes `onNodeEvent` (`app.go`),
   which does two things with it: `ingestNodeEvent` publishes it into the unified notification
   feed via `republishNodeNotification` (unchanged from before P6, now deduped on the node's
   stable engine id — see 2a below), and `observeForCorrelation`
   (`apps/myseliasan/app/correlate_bridge.go`) flattens it into a `services.NodeEvent` and hands
   it to the correlator — fed the **node's own event**, never the notification the first step
   just republished, so a fleet rule's own alert can never satisfy another fleet rule's clause.
2a. **Replay on reconnect (the live path above only carries events published while the channel
    is up):** a notification a node raises during a disconnect is otherwise dropped with no
    backfill. `ControlServer.SetOnConnect` (`app.go`) fires whenever a node's control connection
    is (re)accepted, pulling that node's `GET /api/notifications?since=<now-72h>` over the tunnel
    (`domain/notification.Service.ListSince`, oldest-first) and feeding each missed row back
    through the same `republishNodeNotification` as step 2. Both paths dedup against
    `apps/myseliasan/services.RelayDedup` (a `relayed_notif` ledger keyed
    `"<nodeId>|<originId>"`), so an event delivered live is never re-published by a later replay
    and vice versa; the `originId` is the node's engine id, round-tripped through the persisted
    row's `metadata.__oid` (`domain/notification.OriginIDKey`) so it survives the pull.
3. `Correlator.Observe` resolves the event's node kind from the **adopted node's own record**
   (`ManagedNode.Kind`, via a `registry.List` closure), matches it against every enabled
   `FleetRule`'s clauses, and records which `"required"` clauses it satisfies.
4. If every `"required"` clause for a rule has now been seen within `WindowSeconds`, the rule
   **arms** — it does not fire yet.
5. A 1-second ticker (`app.go`) calls `Correlator.Sweep` on every armed rule. Once a rule's
   `GraceSeconds` has elapsed, `Sweep` checks whether any `"absent"` clause matched an event
   during the window — if one did (e.g. a badge swipe arrived, even a few seconds late), the
   rule is silently disarmed and no alert is ever raised.
6. If the grace period elapses with the absent clauses still unmatched, the rule **fires**:
   `LastTriggeredAt` is persisted (so `CooldownSeconds` survives a restart), and a
   `notification.Notification` is published into the same unified feed step 2 writes to — the
   correlator's conclusion lands in the same place the raw node events did, distinguishable by
   `Source: "fleet-rule"`. When an enricher is wired (`Correlator.SetEnricher`,
   `apps/myseliasan/services/correlate_enrich.go`), the notification body gains a second,
   deterministic line of recurrence context ("also fired N times in the last 7 days") before
   publishing — a bounded, DB-reads-only lookup under a hard timeout, never an LLM call, since
   this step is still inside the alert path.

**Data path summary:** node event → control channel → `myseliasan` `onNodeEvent` → (a)
notification feed (deduped via `RelayDedup`), (b) `Correlator.Observe` → arm → `Sweep` (grace
elapsed, absent clauses still unmatched) → fire → notification feed. On reconnect, a second path
backfills anything missed: node (re)connects → `ControlServer.SetOnConnect` → pull
`GET /api/notifications?since=` over the tunnel → `RelayDedup`-gated `republishNodeNotification` →
notification feed.

## Fleet Configuration Policy Reconcile Flow (myseliasan → node)

This is the comparison side of fleet configuration policy + drift detection (flagship
hardening plan W2-1). Unlike the correlation flow above (event-driven, arms on a matching
event), this flow is timer-driven: nothing a node does triggers it, only the passage of time.

1. `leaderTicker` fires every 15 minutes (plus once ~90s after boot) and, only on the
   deployment's leader instance, calls `FleetPolicyReconciler.ReconcileAll`
   (`apps/myseliasan/services/fleet_policy_reconciler.go`).
2. For every adopted node (`PolicyNodeLister.List`, a narrowed view of `INodeRegistry`),
   `services.ResolveEffectivePolicy` merges every enabled `FleetPolicy` that matches the
   node's kind and scope (fleet → site → node, most specific wins per field, higher id
   breaks a same-scope tie) into one `EffectivePolicy` — a pure, database-free function
   (`apps/myseliasan/services/fleet_policy.go`).
3. If the node has no applicable policy at all, it is reported `unmanaged` and nothing is
   read from it.
4. Otherwise, for each governed catalog section (`apps/myseliasan/services/policy_catalog.go`
   — continuity, health, tamper, machineHealth, notificationRetention), the reconciler reads
   the section's current values from the node over the **same control-channel tunnel**
   `apis/node_proxy.go` uses for an operator's own node screens (`ControlSender.SendRequest`,
   `Role: "admin"`, `Actor: "fleet-policy"` — the node authorizes this exactly as it would an
   operator-driven request, asserting no special capability).
5. Each governed field is compared (`policyValuesEqual`, numeric-exact, no epsilon) against
   the winning policy's desired value. A field the node's response does not contain at all is
   `missing` (different remediation than drift: upgrade the node, not enforce a value its
   decoder would reject); a section that could not be read at all (offline, wrong role, 404)
   marks the whole node `unknown` — **never `compliant`**, since the node most likely to have
   actually drifted is the one that could not be reached.
6. If every read section's fields matched, the node is `compliant`. If any field disagreed,
   the node is `drifted` — UNLESS the field's winning policy has `Enforce` on, in which case
   step 7 runs before the final verdict.
7. **Enforcement (opt-in per policy, default off):** the reconciler re-reads the section's
   current object, overlays every enforcing drifted field onto it (`policySetPath`), and PUTs
   the WHOLE merged object back — never just the governed fields, since the node's settings
   endpoints reject unknown fields and a partial PUT would zero out every ungoverned field in
   the section. It then reads the section back a second time to VERIFY the value actually
   stuck (a `200` is not proof; every node settings service normalizes/clamps what it is
   given) before marking the field `applied`. A successfully-verified field is folded back
   into the `compliant` count for that node; an unverified or failed write leaves the node
   `drifted` and records why.
8. Every enforced write is recorded to the audit trail (`ActionPolicyEnforce`,
   `apps/myseliasan/services/audit.go`) regardless of outcome — the one settings change on a
   node with no operator behind it at the moment it happens.
9. The whole pass's result is stored in memory (`FleetPolicyReconciler.Last()`) and served by
   `GET /api/fleet-policies/compliance` without a fresh sweep; `POST
   /api/fleet-policies/compliance/refresh` (superadmin-only) or `POST
   /api/fleet-policies/compliance/{nodeId}` triggers one on demand instead of waiting for the
   next tick.

**Data path summary:** timer tick (leader-gated) → `ReconcileAll` → per node →
`ResolveEffectivePolicy` (pure) → per governed section → `GET` over control tunnel → compare
→ (compliant | drifted | unknown | unmanaged) → if drifted AND enforcing: merge + `PUT` +
re-`GET` verify → audit (`policy.enforce`) → stored compliance report → served to the SPA.

## Federated Cross-Node Search Flow (myseliasan → nodes)

This flow answers "where was this seen" across the whole fleet in one request — federated
cross-node search, flagship hardening W2-4 (finding F-10). Unlike every other node-tunneled
flow in this document, it fans a SINGLE request out to MANY nodes at once rather than
proxying one browser call to one node.

1. The browser calls `GET /api/nodes/search` (or `/search/labels`) once, with the search
   terms in the query string — never a per-node proxied call, unlike the browser-side fan-out
   this replaced.
2. `apps/myseliasan/apis/fleet_search_api.go` resolves the caller's **live** role and calls
   `FleetSearchService.Search`, which resolves the set of nodes that role can reach (per-node
   access grants, the same ones the node proxy uses) and that are a kind capable of holding
   sightings (a camera node; an IoT hub or door controller is skipped and counted, not
   silently dropped).
3. For each target node, concurrently (bounded to 8 at once, each under its own 15s
   deadline), `apps/myseliasan/services/fleet_search.go` sends ONE tunneled `control.Request`
   per requested source over the SAME control channel/tunnel the node proxy and the fleet
   policy reconciler use: `GET /api/observations/search` (object sightings) and/or
   `GET /api/vision/alerts/identities` (recognized plates/faces).
4. On the node, each endpoint answers through `apps/mymatasan/services/sighting_search.go`,
   which reads through the SAME `ObservationService` the node's own Objects page uses (so a
   sighting found by the fleet search resolves to the same footage segment the node's own UI
   would open for it), joins in camera names, and declares `capped`/`oldest` when it returned
   only a prefix of its matches.
5. Each node's outcome — success (with its hits), or a transport/HTTP failure classified into
   `offline | timeout | denied | unsupported | error` — becomes one `NodeCoverage` entry;
   `myseliasan` never treats a failed or unreachable node as "saw nothing".
6. Once every fan-out goroutine returns (or times out), the results are merged newest-first
   (deterministic tie-break: node id, then row id), clamped to the requested limit
   (`truncated: true` if cut), and returned alongside the `coverage` block: which nodes were
   searched, how many answered, whether the result set is `complete`, and — if any node
   capped — `completeThrough`, the timestamp back to which the merged result is guaranteed
   complete.
7. `fleet_search_api.go` audits the search as `fleet.search` with the query terms and an
   outcome of `"partial"` whenever `coverage.complete` is false, so the audit trail can never
   be misread as proof the whole fleet was actually asked.
8. The frontend (`views/components/objects.js`'s `FleetObjectSearch`) renders a coverage
   banner on every completed search — a reassuring line when every reachable node answered in
   full, and a per-node breakdown (status + reason) otherwise — so an empty or partial result
   set is never displayed as "nothing was seen" when part of the fleet was never actually
   reached.

**Data path summary:** browser → `GET /api/nodes/search` → `FleetSearchService.Search` →
(per accessible, sighting-capable node, bounded parallel fan-out) control channel →
`GET /api/observations/search` + `GET /api/vision/alerts/identities` → node's
`SightingSearch` (reuses `ObservationService`'s footage linkage) → merged, capped, and
coverage-annotated `FleetSearchResult` → audit (`fleet.search`, outcome `success`/`partial`)
→ browser coverage banner. See `docs/modules/apps/myseliasan/services/fleet_search.go.md`,
`docs/modules/apps/mymatasan/services/sighting_search.go.md`.

## Two-Way Audio (Talk-Back) Flow (browser mic → mymatasan → camera)

This flow lets an operator speak through a camera's own speaker from the live-view tile. Direction is the reverse of live view — audio flows browser → server → camera. Two transports are resolved server-side, in order: the standard ONVIF RTSP audio backchannel, then the TP-Link Tapo/VIGI proprietary port-8800 protocol for consumer cameras with no RTSP backchannel.

1. While a live-view tile is open, the frontend calls `GET /api/cameras/{id}/talk`; the handler resolves (or returns the cached, ≤10 min old) `TalkCapability` by first probing the camera's RTSP endpoint for an ONVIF audio backchannel (`talk.HasBackchannel`), then — only if that fails — probing port 8800 (`talk.Probe8800`) for a genuine TP-Link "Streamd" fingerprint. The mic button only renders when `supported` is true. When the resolved transport is TP-Link and `needsPassword` is true, the camera's Access tab shows a speaker-password field; the operator saves it via `POST /api/cameras/{id}/talk/password` before talk-back will connect.
2. Pressing the mic button captures the browser microphone (`getUserMedia`, 8 kHz mono, echo cancellation on) and creates a local WebRTC peer with a `sendonly` PCMA audio track; the browser generates an SDP offer and POSTs it to `POST /api/cameras/{id}/talk/offer`.
3. The API handler calls `ICameraService.OpenTalkSession`, which re-checks capability and, if supported, dials the resolved transport — `talk.DialONVIF` over the camera's RTSP backchannel using its stored ONVIF credentials, or `talk.DialTapo` over port 8800 using the stored `TalkPassword` (Tapo cloud password or VIGI admin password) — opening a live `talk.Session` ready to accept G.711 frames.
4. `talk.AnswerBrowserTalk` builds a PCMA-only WebRTC peer, adds a `recvonly` audio transceiver, sets the browser's offer as the remote description, and answers; the SDP answer is returned to the browser, which sets it as its own remote description to complete the handshake.
5. As the browser's peer connection receives audio, `pumpTrack` reads each RTP packet off the browser track and forwards its payload to `session.WritePCMA`. Over ONVIF this converts A-law → µ-law first if the camera's backchannel needs it and writes into the RTSP backchannel session; over TP-Link it packetizes the A-law frame into an MPEG-TS PES payload (`infra/talk/mpegts.go`) and writes it as a multipart `audio/mp2t` part on the port-8800 connection.
6. Toggling the mic off, closing the tile, or the peer connection failing/disconnecting closes both the WebRTC peer and the underlying `talk.Session` (`stopTalk` client-side; `closeAll` server-side).

**Data path summary:** browser mic → WebRTC (PCMA, pion) → mymatasan (`infra/talk`) → RTSP ONVIF audio backchannel **or** TP-Link port-8800 multipart/MPEG-TS session → camera speaker.

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
3. The frontend saves each rule through `POST /api/vision/rules`, including camera ID, detection type, normalized zone polygon points (one polygon, or a list of polygons for multi-zone — evaluated as a union), optional `ruleConfig` for line definitions, threshold, minimum frame count, cooldown, alert sound setting, enabled state, and optional rule-level `schedulePolicy`.
4. `schedulePolicy` is evaluated per rule. Empty policy means always active; weekly windows and RFC3339 date ranges can either allow detection only inside matches or deny detection during matches.
5. The MyMataSan vision monitor runs as an app worker when `vision.enabled` is true, loads enabled rules, filters out rules whose schedule is inactive, groups active rules by camera, and captures a JPEG frame from the saved RTSP URI or snapshot URI.
6. The configured reusable `infra/vision` detector runs in `motion`, `external`, `hybrid`, or `persistent` mode. Motion mode compares consecutive frames inside each rule's zone(s). External object mode maps model candidates to rule detection types and zone(s). Hybrid mode can use external object detection for semantic rules while routing configured rule types such as `intrusion` to motion. Persistent mode keeps a worker such as `yolo_worker.py` alive, sends each JPEG frame as newline-delimited JSON, and receives normalized object candidates without reloading the model per frame.
7. A detection is raised only when type/class matching, threshold, polygon or line-crossing geometry, minimum frame count, sequence state, and cooldown requirements are satisfied. Cooldown state is seeded from each rule's persisted `LastTriggeredAt` the first time the process sees it (`infra/vision/cooldown.go`), so a restart does not reset every rule's cooldown to zero and re-trigger a busy scene immediately.
8. Detection results are persisted as `alert_event` rows through `POST /api/vision/alerts` service logic. Diagnostic alert rows are throttled and written when capture or detection fails, or when frames are sampled without crossing the rule threshold. The vision monitor also writes back each fired rule's trigger time via `IVisionService.MarkRuleTriggered` so the next restart's cooldown seed is accurate.
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
