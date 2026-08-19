# Flagship Hardening Plan — mymatasan + myseliasan

Source: the 2026-08-19 condition register (24 findings, IDs `F-01`…`F-24`).
Report artifact: https://claude.ai/code/artifact/6fffdcb0-2ed5-4973-995e-6f97230e5e19

This is the working plan. It is written to be **resumable**: the status board below is
the single source of truth for where the work stands. Update it in the same commit that
lands the work.

---

## Status board

| # | Work item | Finding | Branch | Status |
|---|-----------|---------|--------|--------|
| **Phase 1 — Trust the system** |
| W1-1 | myseliasan backup & restore (`.selbackup`) | F-01 | `feat/myseliasan-backup` | ✅ shipped (benched) |
| W1-2 | Shared audit package + mymatasan audit log | F-02, F-22 | `feat/shared-audit` | ● built, not benched |
| W1-3 | Recording continuity monitor + coverage report | F-03 | `feat/mymatasan-continuity` | ● built, not benched |
| W1-4 | Evidence export with integrity manifest | F-04 | `feat/mymatasan-evidence-export` | ● built, not benched |
| W1-5 | Tamper / video-loss detection | F-05 | `feat/mymatasan-tamper` | ● built, not benched |
| W1-6 | Nightly `-race` CI job | F-21 | `ci/race-nightly` | ✅ shipped |
| **Phase 2 — Operate at fleet scale** |
| W2-1 | Fleet configuration policy + drift detection | F-06 | `feat/myseliasan-fleet-policy` | ◑ half-benched |
| W2-2 | Node state history + SLA reporting | F-08 | — | ☐ not started |
| W2-3 | Critical-clip archive to control plane | F-09 | — | ☐ not started |
| W2-4 | Federated cross-node search | F-10 | — | ☐ not started |
| W2-5 | Staged version rollout | F-07 | — | ☐ not started |
| W2-6 | Instrument dropped control-channel events | F-11 | — | ☐ not started |
| W2-7 | Email notification channel | F-20a | — | ☐ not started |
| **Phase 3 — Win the bake-off** |
| W3-1 | Timeline playback | F-12 | — | ☐ not started |
| W3-2 | Appearance search across cameras/nodes | F-16 | — | ☐ not started |
| W3-3 | Cases + video wall | F-17, F-18 | — | ☐ not started |
| W3-4 | Loitering / left-behind / directional rules | F-15 | — | ☐ not started |
| W3-5 | PTZ presets + ONVIF events & relay I/O | F-13, F-14 | — | ☐ not started |
| W3-6 | Privacy masking + export redaction | F-19 | — | ☐ not started |
| W3-7 | N+1 node failover | F-23 | — | ☐ not started |
| W3-8 | Tenant isolation (decision required first) | F-24 | — | ☐ not decided |
| W3-9 | Mobile PWA + web push | F-20b | — | ☐ not started |

Status vocabulary: `☐ not started` → `◐ in progress` → `● built, not benched` →
`◑ half-benched` → `✅ shipped`.
**`● built, not benched` is never the end state.** Everything here is boot-and-exercise
before it counts as done.

---

## Working conventions

- **One branch per work item**, base `main`, one PR each. W1-1 and W1-2 touch disjoint
  files and can run in parallel; W1-4 depends on W1-2 (it writes an audit record).
- **Before every commit/push**: run the `docs-sync` agent (always) and `i18n-sync`
  (whenever the frontend was touched — en/ms/zh/**ar**, the fourth is Arabic).
- **Version entry**: each PR adds `changes/pending/<ts>-<slug>/change.json` with
  `type`, `scope`, `summary`, `compatibility`. `scope` must be the **app name**
  (`mymatasan` / `myseliasan`) — `scope:"core"` produces no tag and therefore no release.
- **Schema changes**: additive columns need no migration (the auto-migrator adds them).
  Write a `bootstrap.Migration` only for structural changes. Never edit the ID or SQL of
  a released migration.
- **DB footguns** (both bite silently): `GetByForeign` returns ONE row — use `Get` +
  `Equal`. `GetById` ERRORS on a missing row — wrap with an is-not-found check.
  `GetByUnique` with no matching ukey returns the FIRST row.
- **Never** rewrite a repo file with `Get-Content | Set-Content` (adds a BOM, mangles
  non-ASCII) and never append source with `cat >>`. Use the Edit tool.

---

# Phase 1 — Trust the system

Target ~4–6 weeks. Everything in this phase answers a question a customer asks on a bad
day. Ship W1-1 first — it is the only finding describing state that cannot be recovered
at all.

## W1-1 · myseliasan backup & restore (`.selbackup`) — F-01

**Why first.** myseliasan holds the fleet CA private key, the node registry, RBAC, sites
and floorplans, and the audit log. `app.go:1893` correctly refuses to boot rather than
silently resetting fleet trust when the fleet-secret key is missing — which makes key
loss loud, total, and currently unanswerable. One failed disk = re-adopt every node by
hand.

**Template.** `apps/myidsan/services/backup.go` (973 lines) is the closest fit and
already solves the hard part (sealed columns). `apps/mymatasan/services/backup.go` (627)
is the second reference. This is a port, not a design problem.

### File format

Identical envelope to `.idbackup`:

```
magic "SELB" (4 bytes) || formatVersion (1 byte) || atrest.EncryptWithPassphrase(json(backupFile))
```

`backupApp = "myseliasan"`, `backupSchemaVersion = 1`, `backupFormatVersion = 1`.
Manifest (`App`, `AppVersion`, `SchemaVersion`, `CreatedAt`, `Sections`, `Counts`) is
surfaced by `Preview` before anything is applied.

### Sections

Order matters — it is also the apply order, and later sections depend on ids remapped by
earlier ones.

| Section | Tables / data | Notes |
|---|---|---|
| `access` | control_user + shared accessrbac role/permission tables | Password hashes ride along, as in myidsan |
| `fleetca` | `control_setting` rows `pairing.caCert`, `pairing.caKey`, `pairing.parentCert`, `pairing.parentKey`, `pairing.revoked` | **The section that matters.** Private keys are sealed — see below |
| `fleet` | `managed_node` + `node_access_grant` | Replace mode drops the fleet — gate behind explicit confirm |
| `sites` | `site` + `node_placement` + floor-plan images from `apps/myseliasan/floorplans/` as base64 | Mirror myidsan's `backupAvatar` pattern for the on-disk images |
| `rules` | `fleet_rule` | |
| `settings` | remaining `control_setting` rows (fleet key, deployment mode, agent schedule) | Excludes the `pairing.*` keys owned by `fleetca` |
| `audit` | `audit_log` | **Merge mode only.** The trail is append-only; never offer replace |

**Deliberately excluded** — state the reason in the code, not just here:
`relayed_notif` (a short-lived dedup ledger, not a record), `agent_digest` (regenerable),
basemap PMTiles and `llm/models/` (large and re-downloadable).

### The crux: sealed columns

The fleet CA private key is stored **in the database** as a `control_setting` row, sealed
with the at-rest cipher (`apps/myseliasan/services/secret_store.go`). A backup restored
onto a machine with a different at-rest key must still work.

Follow the `.idbackup` rule exactly:

- **Export**: unseal with the source cipher — `decodeSecret(s.cipher, row.Value)` — so the
  payload carries plaintext PEM inside the passphrase-encrypted envelope.
- **Restore**: re-seal with the *destination* cipher — `encodeSecret(s.cipher, plain)` —
  before writing the row.

`decodeSecret` already returns legacy plaintext untouched when a row was never wrapped,
so a pre-encryption backup restores correctly.

### The second crux: the CA is cached in memory

`fleetCA` holds `ca *fleetca.CA` under a mutex and only reloads via `ensure()`. Restoring
the `fleetca` section into a running control plane leaves the process serving mTLS from
the *old* CA. Do not try to hot-swap it.

**Restoring `fleetca` forces a restart** — reuse `apphost.Restarter`, the same path
settings-materialize already takes. Say so in the UI before the operator confirms.

### Steps

1. `apps/myseliasan/services/backup.go` — port from myidsan; sections above; `IBackupService`
   with `AvailableSections` / `Export` / `Preview` / `Restore`.
2. `apps/myseliasan/services/backup_test.go` — round-trip per section; a
   **different-at-rest-key restore** test (the whole point); replace-vs-merge; a rejected
   non-`SELB` upload; a wrong passphrase.
3. `apps/myseliasan/apis/backup.go` — mirror `apps/mymatasan/apis/backup.go`:
   `GET /api/backup/sections`, `POST /api/backup/export`, `POST /api/backup/preview`,
   `POST /api/backup/restore`. Superadmin only (`h.requireSuper`, as in `apis/system.go`).
4. `apps/myseliasan/app/app.go` — wire the service and API. It needs the settings repo,
   the at-rest cipher, and the accessrbac repos.
5. Frontend: a Backup & Restore panel in `views/components/settings.js`, matching
   mymatasan's. Restart warning on the `fleetca` section. i18n across en/ms/zh/ar.
6. Audit: record `backup.export` / `backup.restore` with section counts in `Metadata`
   (lands naturally once W1-2 is in; if W1-1 ships first, add the hook in W1-2).

### Done when

Live-benched, not just green:

1. Adopt at least two nodes onto a throwaway control plane; confirm mTLS heartbeat.
2. Export a full `.selbackup`.
3. Destroy the install — drop the DB **and** delete the at-rest key file.
4. Fresh install, new at-rest key, restore the backup, restart.
5. **Both nodes reconnect over mTLS with no re-adoption and no claim code.**

Step 5 is the acceptance test. Nothing else proves the CA survived.

---

## W1-2 · Shared audit package + mymatasan audit log — F-02, F-22

**Why.** mymatasan — the app holding the actual video — is the only app in the suite with
no audit log. `DELETE /api/recordings/segments/{id}`
(`apps/mymatasan/apis/recording.go:124`) deletes footage recording no actor, no reason.
Viewing and downloading are equally unrecorded. That is a GDPR Art. 30 line item and a
hard requirement in government CCTV tenders.

Audit is already implemented **twice** (myidsan, myseliasan) with no shared package.
Adding a third copy is how three implementations drift. Extract first, then adopt.

### Extract `domain/shared/audit`

myidsan's version is a strict superset — take it as the shared shape:

- **Entity**: myidsan's `AuditLog`, including `UserAgent` and `idx:"actor"` on
  `ActorEmail`. For myseliasan this is an additive column; the auto-migrator adds it.
- **Service**: `Record(ctx, AuditEntry)` (best-effort, never propagates a write failure —
  auditing must not be able to fail the audited action), `List(ctx, limit, offset,
  AuditFilter)`, `PurgeOlderThan(ctx, days, archiveDir)`.
- **Retention**: port `apps/myidsan/services/audit_retention.go` whole. It writes a
  JSON-lines archive before deleting, deletes nothing if the archive did not flush, and
  records the purge naming the cutoff and archive file. Keep all three properties.

Adopt in the two existing apps using **type aliases**, the pattern
`apps/mymatasan/services/fleetnode.go` already uses for `domain/shared/fleetnode` — call
sites stay unchanged. Wrap myseliasan's 3-argument `List(limit, offset, action,
targetType, targetId)` into an `AuditFilter` so its API handler needs no edit.

Action constants stay **per app** — the verbs are app-specific.

### Adopt in mymatasan

- `apps/mymatasan/services/audit.go` — aliases + mymatasan's action constants.
- `apps/mymatasan/apis/audit.go` — list + CSV export, mirroring `apps/myidsan/apis/audit.go`.
- `apps/mymatasan/app/wire_services.go` + `wire_routes.go` — build and mount. Admin-only
  in the page matrix (`services/pages.go`).
- Table is new in this app, so the auto-migrator creates it. No migration needed.

### Actions to record

Evidence handling is the point — do not stop at configuration changes:

| Action | Where |
|---|---|
| `recording.view`, `recording.download` | `apis/recording.go` — `downloadSegment`, `segmentFrame` |
| `recording.delete`, `recording.purge` | `deleteSegment`, `purgeExpired`, `purgeCameraNow` |
| `recording.export` | W1-4, with the manifest hash in `Metadata` |
| `recording.config_change` | `saveConfig` — include before/after retention |
| `camera.create`, `camera.update`, `camera.delete` | `apis/camera.go` |
| `camera.credential_change` | `apis/camera.go` — **never** put the credential in `Metadata` |
| `rbac.set_role`, `user.create`, `user.disable` | `apis/local_auth_api.go`, `apis/settings.go` |
| `settings.change` | `apis/settings.go` |
| `backup.export`, `backup.restore` | `apis/backup.go` |
| `system.reset`, `system.update` | `apis/system.go` |
| `teach.skill_activate` | `apis/teach.go` — it changes what the system detects |

`ClientIp` must come from `middlewares.ClientIP` (trusted-proxy resolution) so it cannot
be forged.

### Done when

- The three apps share one implementation; myidsan and myseliasan behaviour is unchanged
  (their existing tests pass untouched).
- Deleting a recording as an admin produces an audit row naming the actor, IP, camera and
  time range; viewing one produces a `recording.view` row.
- Retention purge archives before deleting, verified by deleting the archive dir's write
  permission and confirming nothing is removed.

---

## W1-3 · Recording continuity monitor + coverage report — F-03

**Why.** `CameraHealthMonitor` probes TCP reachability with an RTSP DESCRIBE deep-check.
It is well built and answers the wrong question. A wedged ffmpeg, a full disk, a
quarantine loop, or a silently changed stream URL all leave the camera "online" while
nothing is written.

**The data already exists.** `RecordingSegment` carries `CameraId`, `StartedAt`, `EndedAt`
under a `cam_time` composite index; `RecordingConfig.Enabled` says which cameras are
supposed to be recording. The sweep is an indexed range query.

### Steps

1. `apps/mymatasan/services/recording_continuity.go` — on an interval, for each camera
   with `RecordingConfig.Enabled`, take the last **closed** hour, sum segment coverage
   over `[hourStart, hourEnd]` clipped to the window, and compare against 3600s minus a
   configurable tolerance. Debounced and edge-triggered, mirroring `CameraHealthMonitor`:
   N consecutive bad hours before alerting, M good before clearing.
2. Raise `recording.gap` into the notification service so it flows to the unified feed and
   every configured destination like any other event.
3. `apps/mymatasan/services/recording_continuity_test.go` — full hour, partial hour,
   zero coverage, overlapping segments (must not double-count), a segment straddling the
   hour boundary.
4. `GET /api/recordings/coverage?cameraId=&from=&to=&bucket=hour|day` → coverage
   percentage per bucket. Backs both the UI strip and the report.
5. `apps/mymatasan/services/health_settings.go` — a continuity section (enabled,
   interval, tolerance %, failure/recovery thresholds), read live each sweep like the
   other monitors.
6. `wire_monitors.go` — start it under `safego`, alongside `w.cameraHealth.Start(ctx)`.
7. Frontend: a coverage strip on the Recording page — a month of days, shaded by
   coverage, click to drill into hours. i18n ×4.

### Cruxes

- **Exclude detect-only streams.** Cameras started via `EnsureDetectionStream` write no
  segments by design. `Manager.detectStreams` already distinguishes them — do not alarm
  on them.
- **Attribute the gap.** If `CameraHealthMonitor` had the camera offline for the window,
  the alert must say *"no footage — camera was offline 14:00–15:00"*, not raise a second
  independent alarm. One incident, one story.
- **Only score closed hours.** Scoring an in-progress hour reports a false gap every
  sweep — the same trap `AnalyticsMonitor` already documents.

### Done when

A camera is recording normally; kill its ffmpeg child without stopping the recorder, and
within two sweeps a `recording.gap` alert names the camera and the window. The coverage
strip shows the hole. Restore it and the alert clears.

---

## W1-4 · Evidence export with integrity manifest — F-04

**Why.** Playback is per stored segment. There is no way to ask for 14:05–14:40 on camera
3 and get one file, no hash, no export record, and no bundled metadata. An operator
stitching segments in an external tool destroys any claim the footage is unmodified —
and this is the moment the entire product is bought for.

**Depends on W1-2** (the export must write an audit record).

### Segment hashing

Add `Sha256 string` to `entities.RecordingSegment`. Additive — the auto-migrator adds the
column; empty means "legacy row, recorded before hashing" and the manifest must say so
rather than pretending.

Compute it in `infra/recording/rtsp.go` at finalize, in both `remuxSegment` and
`adoptSegment`, over the **plaintext MP4 before encryption** and before the atomic
rename. Hashing plaintext (not ciphertext) makes the hash stable across at-rest key
changes and across a backup/restore, and it is what a third party can independently
verify against the exported file.

This upgrades the guarantee from "the export is internally consistent" to "the footage
was not altered between recording and export".

### Export job

`apps/mymatasan/services/evidence_export.go`:

1. Resolve segments overlapping `[from, to]` for the requested camera(s) via the
   `cam_time` index.
2. Materialize each to plaintext through the existing `segmentPlayFile` path in
   `apis/recording.go` — it already decrypts and optionally transcodes HEVC→H.264 into a
   short-lived temp cache. Reuse it; do not write a second decrypt path.
3. Concatenate with ffmpeg (stream copy where codecs match; re-encode only when they do
   not, and record which in the manifest).
4. Emit `manifest.json` and bundle it with the media and a plain-text `VERIFY.txt`
   explaining how to check the hashes with standard tools.

### Manifest contents

```
app, appVersion, exportedAt (UTC), exporterUserId, exporterEmail, reason
camera: id, name, location, timezone
requestedRange: from, to
coveredRange:   from, to
gaps: [ {from, to, reason} ]          <-- MANDATORY
sources: [ {segmentId, path, startedAt, endedAt, sha256, hashOrigin: "recorded"|"computed-at-export"} ]
output: {filename, sha256, codec, transcoded: bool}
```

**The `gaps` array is the crux.** An export that silently skips missing footage looks
continuous and is actively misleading — worse than refusing to export. Missing minutes
must be visible in the manifest and stated in the UI before the operator confirms.
`hashOrigin` is equally load-bearing: a legacy segment hashed at export time proves only
that the file has not changed since the export, and must not be presented as more.

### API + UI

- `POST /api/evidence/exports` (create, async — a long range is not a request-scoped job),
  `GET /api/evidence/exports/{id}` (status), `GET /api/evidence/exports/{id}/download`.
- Audit `recording.export` with the output hash and the requested range in `Metadata`.
- Frontend: export dialog from the Recording page — range picker, camera picker, a
  required reason field, a gap warning before confirm. i18n ×4.
- Page-matrix grant: exporting is an operator capability; **deleting stays admin-only**
  (the existing RBAC reasoning — an operator present at an incident must not be able to
  destroy the footage of it — still holds).

### Done when

Export a range that deliberately spans a gap. The bundle verifies: recomputing SHA-256 on
the output matches the manifest, each source hash matches what was recorded at finalize,
and the gap is listed with its reason. An audit row names the exporter and the range.

---

## W1-5 · Tamper / video-loss detection — F-05

**Why.** A covered lens, a camera turned to a wall, a defocused ring, or a frozen stream
all leave the camera online and the recorder writing files. The system reports green —
including when someone disabled the camera immediately before an incident.

**No ML needed.** `Manager.LatestFrame(cameraId)` already returns a decoded JPEG and its
capture timestamp from the siphon tee the detector uses.

### Steps

1. `infra/vision/tamper.go` — pure, testable functions over two decoded frames:
   - `FrozenScore` — mean absolute difference; byte-identical successive JPEGs are the
     strong signal.
   - `BlurScore` — variance of Laplacian / edge energy; a sustained collapse means a
     covered or defocused lens.
   - `SceneShiftScore` — histogram distance; a large persistent shift means the camera
     moved.
   Fixture-based tests with checked-in sample frames (clear, blurred, covered, shifted).
2. `apps/mymatasan/services/camera_tamper_monitor.go` — mirrors `CameraHealthMonitor`
   exactly: per-camera state, N-consecutive debounce, edge-triggered, settings read live
   each sweep. Raises `camera.tamper` with a subtype of `frozen` | `covered` | `moved`.
3. Settings section + frontend surface, i18n ×4.
4. Add `tamper` to `detectionModes` in `views/lib/helpers.js` so it can also be authored
   as a per-camera rule — this is also the first step of W3-4.

### Crux: the night problem

A static corridor at 03:00 is not a frozen stream, and IR illumination collapses colour
and edge energy legitimately. Both defaults must be conservative:

- Judge `frozen` on byte-identical frames, or diff below a floor **sustained across a
  window longer than any plausible still scene** (start at 60s, make it configurable).
- Suppress `covered` when the frame is in low-light/IR — or learn a per-camera,
  per-hour-of-day baseline, which is the same shape `AnalyticsMonitor` already uses for
  activity.

Tune against real footage using the existing calibration harness rather than guessing
thresholds. A tamper alert that cries wolf nightly gets muted, and then it protects
nothing.

### Done when

On a live camera: cover the lens → `covered` within the debounce window. Freeze the
stream (pause the source) → `frozen`. Pan the camera → `moved`. Then leave a dark static
scene running overnight with no alert.

---

## W1-6 · Nightly `-race` CI job — F-21

Half a day. Do it in week one so everything else in Phase 1 lands on top of it.

`.github/workflows/go-check.yml` currently runs `go test ./... -count=1` with no `-race`,
no linter, and no frontend job. This tree is unusually concurrent — per-camera recorder
goroutines, four monitor loops, the control channel, media relay fan-out, the
notification hub. That is exactly where a data race hides for months and then corrupts a
recording under load.

- Add a `schedule:` (nightly) + `workflow_dispatch:` job running
  `go test ./... -race -count=1`, timeout ~40 min. Nightly rather than per-PR because
  `-race` is slow; per-PR can follow once the runtime is known.
- Fix what it finds before starting W1-1 proper. Findings here outrank new features.

Follow-ups, not blocking: a `.golangci.yml`, a frontend lint/test job, and promoting
govulncheck from advisory to blocking once someone owns triage — the workflow file
already argues for that last one.

---

# Phase 2 — Operate at fleet scale

Target ~6–8 weeks. This is what turns myseliasan from a dashboard over nodes into the
reason someone buys the fleet instead of the appliance. Re-plan each item in detail when
reached; seams noted now so the shape is not re-derived.

**W2-1 · Fleet configuration policy + drift** (F-06). **Half-benched.**
`FleetPolicy`/`FleetPolicyItem` (fleet → site → node scope, per-node-kind, field-level
precedence) plus a catalog-whitelisted set of governable settings (continuity, health,
tamper, machine health, notification retention — deliberately excluding hardware/runtime
settings and notification routing/credentials) and a reconciler that reads each node's
settings over the existing control channel, compares only governed fields, and — only
when the winning policy has `Enforce` on (default off, report-only) — merges the desired
values onto the node's current section and PUTs, then re-reads to verify. Compliance
states are compliant/drifted/unknown/unmanaged, with unknown explicitly not counted as
compliant. Leader-gated 15-minute sweep plus one pass 90s after boot. `GET/POST
/api/fleet-policies`, `DELETE /api/fleet-policies/{id}`, `GET
/api/fleet-policies/catalog`, `GET/POST /api/fleet-policies/compliance[/refresh]`; reads
follow the permission matrix, writes are superadmin-only. New audit actions
`policy.enforce`/`policy.save`/`policy.delete`. See
`docs/modules/apps/myseliasan/services/fleet_policy_reconciler.go.md`. Building this
surfaced and fixed an unrelated backup gap: `FleetRuleClause` rows were never exported by
`.selbackup`, so a restored correlation rule had zero clauses and could never fire (see
`docs/modules/apps/myseliasan/services/backup.go.md`).

**Benched against a real appliance (2026-08-19) — the half that could hide a real bug.**
A real mymatasan node was booted and the reconciler run against its real settings
handlers, driven by `fleet_policy_live_test.go` (gated on `RUN_NODE_IT=1`; it is a
permanent, re-runnable bench, not a throwaway script). What passed:

- every catalog section is readable from a real node, and **all 21 declared fields exist
  and have the declared type** — a catalog naming a path or field the appliance does not
  serve would have produced nodes that were permanently, unfixably "drifted"
- **all 21 fields can actually be set**; none is normalized away by the node, which would
  have meant a policy that could never go green
- a report-only policy saw the difference and **issued no write at all**
- an enforcing policy corrected the value and left every ungoverned field in the same
  section byte-identical — the merge crux, and the most expensive thing this design could
  get wrong, since the node decodes each section with `DisallowUnknownFields`
- retention wrote on its own path with the node's webhook/Telegram credentials **absent
  from the request body**
- a second pass over an already-correct node wrote nothing
- an unreachable node reported `unknown`, never `compliant`

**What the bench found:** the node rate-limits the control plane like any other caller,
and a tunneled request carries no JWT, so every tunneled call shares one bucket per path.
A real sweep is ≤15 requests per node per 15 minutes and is nowhere near the limit — the
429 came from the exhaustive field test firing ~150 requests in ten seconds — but "node
returned 429" is not something an operator should have to decode, so it now has its own
message. Two other gaps were fixed while benching: a failed unattended write was not being
audited (the more interesting of the two records — a policy that has been failing to
configure an appliance for a month is otherwise invisible), and the ticker and the "check
now" button could start concurrent sweeps against the same fleet.

**Still owed before this is `✅ shipped`**, and none of it is about the logic above:
transport (the same run over the mTLS control channel with an adopted node rather than
HTTP), the API and RBAC gating, the UI, and multi-node precedence in a real fleet
(fleet + site + node policies over two nodes at different sites).

**W2-2 · Node state history + SLA reporting** (F-08). `reports.go:205` states the gap in
its own footnote. Add a node-state history table fed by the existing liveness
transitions, then availability per node / per site / per month into the existing pure-Go
PDF report module. `apps/mymatasan/apis/notification.go:157` is the per-camera equivalent
to model it on.

**W2-3 · Critical-clip archive** (F-09). Per-rule flag that pushes the event clip and
snapshot to the control plane over the existing mTLS control channel, with a retry queue
for the offline case. Narrow deliberately — not "upload everything". Makes the fleet the
system of record for the events that matter, and survives a stolen or burned appliance.

**W2-4 · Federated cross-node search** (F-10). `services/metadata_recorder.go` on the node
already produces the observation index. Either federate the query over the control
channel or replicate a compact index upward. Query terms: plate, face, object class, time
window, site.

**W2-5 · Staged rollout** (F-07). Canary rings, health gate between rings, halt on
failure, rollback. `apps/mymatasan/services/update.go` is the node-side primitive it
drives. Remember the fleet-bricking trap: own tag namespace, never GitHub "latest".

**W2-6 · Instrument dropped events** (F-11).
`domain/shared/fleetnode/control_channel.go:51-59` returns silently when the channel is
down. The 72h replay covers this well, but nothing is counted. Add a drop metric and
alert as a node's disconnect approaches `notifReplayWindow`.

**W2-7 · Email notification channel** (F-20a). `infra/mailer` exists and is used only by
myidsan password reset. A `mail_channel.go` alongside the existing six channels in
`infra/notification/`. Small, and it unblocks procurement conversations that Telegram
cannot.

---

# Phase 3 — Win the bake-off

Target ~8–12 weeks, ordered by how often each decides a competitive evaluation.

**W3-1 · Timeline playback** (F-12). Scrub bar with coverage shading (reuses W1-3's
coverage endpoint), seek across segment boundaries, multi-camera sync, speed control,
detection events plotted as jump targets. Shares its backend with W1-4.

**W3-2 · Appearance search** (F-16). The strongest differentiator available, and the
infrastructure is largely built: `services/face_embedder.go`, `face_gallery.go` and
`entities/face_embedding.go` are the storage-and-match seam. Extend from faces to person
and vehicle appearance embeddings, then federate through the control plane (needs W2-4).

**W3-3 · Cases + video wall** (F-17, F-18). Bookmarks, annotation, multi-clip incident
packaging, assignment, closure — the natural home for W1-4's export bundle and W1-2's
audit trail; the three want to be one feature. Video wall: named layouts, sequence
cycling, multi-monitor, alarm-driven auto-pop. The fleet-level wall spanning nodes is
something no appliance vendor can match.

**W3-4 · Loitering / left-behind / directional** (F-15). New rule evaluators over the
existing tracker, not new pipelines. Loitering and direction are track-duration
questions over data already produced.

**W3-5 · PTZ presets + ONVIF events** (F-13, F-14). `infra/onvif/client.go` stops at
`PTZMove`/`PTZStop` — add presets, absolute/relative move, tours, home, recall-on-detection.
Separately, no event subscription exists at all: adding PullPoint unlocks camera-side
analytics and tamper alarms, digital **inputs** (door contacts, PIRs, panic buttons), and
relay **outputs** (sirens, strobes, gates). Relay output is the difference between
recording an intrusion and responding to one — and the natural mypintusan seam.

**W3-6 · Privacy masking + redaction** (F-19). Static pre-record masks, face blur on
export, redaction workflow. **Promote into Phase 2 if any EU deployment comes into view**
— static masking is a deployment precondition there, not a feature.

**W3-7 · N+1 node failover** (F-23). Driven from the control plane, which already knows
which nodes are alive and which cameras they own. `apis/deployment.go` is right that
mymatasan cannot cluster; failover is a myseliasan feature.

**W3-8 · Tenant isolation** (F-24). **Decide before building anything else in Phase 3:**
is integrator/MSP resale a target? If yes, retrofitting a tenant dimension after the
schema is in the field is expensive, and it should move much earlier. If no, close this
item.

**W3-9 · Mobile PWA + web push** (F-20b). The higher-effort half of the notification gap.

---

## Resume checklist

When picking this up cold:

1. Read the status board above.
2. `git branch -a` and `gh pr list` — a work item may be in flight.
3. For anything marked `● built, not benched`, the remaining work is the live bench in
   that item's **Done when**, not more code.
4. The mymatasan verify recipe (throwaway app with env `HOME`/`DATA`, fresh sqlite,
   seeded admin, CDP screenshots) is the standard harness for UI verification.
   `tools/k6 -App {mymatasan|myseliasan}` is the load harness.
