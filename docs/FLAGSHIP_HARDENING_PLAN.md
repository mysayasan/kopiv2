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
| W1-2 | Shared audit package + mymatasan audit log | F-02, F-22 | `feat/shared-audit` | ✅ shipped (benched, no UI) |
| W1-3 | Recording continuity monitor + coverage report | F-03 | `feat/mymatasan-continuity` | ✅ shipped, benched 2026-08-21 (15/15 on shipped defaults) |
| W1-4 | Evidence export with integrity manifest | F-04 | `feat/mymatasan-evidence-export` | ✅ shipped (benched) |
| W1-5 | Tamper / video-loss detection | F-05 | `feat/mymatasan-tamper` + `fix/mymatasan-tamper-moved` | ✅ shipped (benched; the moved verdict was found broken by that bench and fixed in #176) |
| W1-6 | Nightly `-race` CI job | F-21 | `ci/race-nightly` | ✅ shipped |
| **Phase 2 — Operate at fleet scale** |
| W2-1 | Fleet configuration policy + drift detection | F-06 | `feat/myseliasan-fleet-policy` | ✅ shipped, **screen benched 2026-08-25 (27/27 en, 29/29 ar) — the first time this screen had ever been driven in a browser; it found the fleet still reporting every node COMPLIANT after the last policy was deleted, on the page and from the API.** |
| W2-2 | Node state history + SLA reporting | F-08 | `feat/myseliasan-node-sla` | ✅ shipped, benched 2026-08-21 (33/33 on a real two-node fleet) |
| W2-3 | Critical-clip archive to control plane | F-09 | `feat/fleet-clip-archive` | ✅ shipped, benched 2026-08-21 (22/22 — footage verified surviving a destroyed node) |
| W2-4 | Federated cross-node search | F-10 | `feat/fleet-federated-search` | ✅ shipped, benched 2026-08-22 (36/36 on a real two-node fleet) |
| W2-5 | Staged version rollout | F-07 | `feat/fleet-staged-rollout` | ✅ shipped, benched 2026-08-22 (22/22 on a real two-node fleet) |
| W2-6 | Instrument dropped control-channel events | F-11 | `feat/control-channel-drop-metrics` | ✅ shipped, benched 2026-08-23 (19/19 on a real two-node fleet) |
| W2-7 | Email notification channel | F-20a | `feat/email-notification-channel` | ✅ shipped, benched 2026-08-23 (34/34 on a real two-node fleet + 3 screen passes) |
| **Phase 3 — Win the bake-off** |
| W3-1 | Timeline playback | F-12 | `feat/timeline-playback` | ✅ shipped, benched 2026-08-23 (29/29 on real recorded footage + 25/25 en and 26/26 ar screen passes; the screen pass found 4 defects the API bench could not) |
| W3-2 | Appearance search across cameras/nodes | F-16 | `feat/appearance-search` | ✅ shipped, benched 2026-08-23 (11/11 on the real model, 34/34 on a real two-node fleet, 17/17 en and 18/18 ar screen passes) |
| W3-3a | Case files (bookmarks, annotation, assignment, closure, bundle export, footage hold) | F-17 | `feat/case-files` | ✅ shipped, benched 2026-08-24 (48/48 on real recorded footage + 31/31 en and 32/32 ar screen passes; the bench found 4 defects, one of them in W1-4's shipped export) |
| W3-3b | Video wall (named layouts, sequence cycling, alarm auto-pop, second monitor) | F-18 | `feat/video-wall` | ✅ shipped, benched 2026-08-24 (25/25 API + 21/21 en and 21/21 ar screen passes; found a shipped defect that made MOST cameras undeletable) |
| W3-4 | Loitering / left-behind / directional rules | F-15 | `feat/dwell-rules` | ✅ shipped, benched 2026-08-24 (23/23 API + 14/14 en and 15/15 ar screen passes; evaluators unit-tested and mutation-checked — the harness has no people to film) |
| W3-5a | PTZ presets, home, guard tours + alarm recall | F-13 | `feat/ptz-presets` | ✅ shipped, benched 2026-08-24 (60/60 against a real ONVIF device + 26/26 en and 26/26 ar screen passes; found a patrol that moved the camera after being stopped, a tour that outlived its deleted camera, and a control that landed outside its tile in Arabic) |
| W3-5b | ONVIF events (PullPoint), digital inputs & relay outputs | F-14 | `feat/onvif-events` | ✅ shipped, benched 2026-08-24 (34/34 against a real ONVIF device + 16/16 en and 16/16 ar screen passes; found an alert-log write that silently never happened, and a control that could not be clicked at all) |
| W3-6 | Privacy zones: camera masks + export redaction | F-19 | `feat/privacy-masking` | ✅ shipped, benched 2026-08-24 (51/51 against a real ONVIF device, including a camera that LIES about what it stored, + 22/22 en and 25/25 ar screen passes; found a redact flag the API silently dropped and a status sentence hard-coded in English) |
| W3-6b | Face redaction on export | F-19 | `feat/face-redaction` | ✅ shipped, benched 2026-08-25 (29/29 against a REAL detector on real recorded footage — the face region measured at YAVG 16.0 against 141.7 unredacted — plus 18/18 en and 20/20 ar screen passes; found OpenCV's warnings colliding with the worker's report, and a dialog that had been rendering with no panel since W1-4) |
| W3-7 | N+1 node failover | F-23 | `feat/node-failover` | ✅ shipped, benched 2026-08-25 (55/55 on a real two-node fleet with real footage + 27/27 en and 30/30 ar screen passes; found four defects — a per-camera takeover result the control plane silently dropped, a takeover that called a retrying ffmpeg process "recording", a sweep that drilled every new plan on its first tick and turned the badge green by itself, and a clean fail-back rendered as an alarm that only LOOKING at the screenshot caught) |
| W3-8 | Tenant isolation | F-24 | — | ✅ CLOSED 2026-08-21 — single-org per install, will not build |
| W3-9 | Mobile PWA + web push | F-20b | `feat/mobile-pwa` | ✅ shipped, benched 2026-08-25 (38/38 against a REAL push service and a REAL node outage — the encrypted body read off the wire, the VAPID assertion checked, and the per-device severity floor proved against the control plane's own notification log — plus 24/24 en and 27/27 ar screen passes; found an install with no internet route being told the push service had REFUSED it, and, in shipped code, a fleet that raised an alarm when a site went dark and never once sounded the all-clear) |

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

### BENCH 2026-08-19 — the coverage read model ✅, the ALERT still owed

Benched against real recorded segments on a real node with two RTSP cameras. The coverage
endpoint was exercised on genuine footage, including a genuine gap: one camera reported
21.19% coverage over the window (763 covered seconds, 14 segments) while the camera whose
source had died reported 2.81% (101 seconds, 2 segments). The maths, the segment
accounting and the endpoint all work on real data.

It also produced, by accident, the exact condition this feature exists for: a camera whose
source stopped while the node went on reporting `state: streaming, ffmpegRunning: true` and
wrote no video for fourteen minutes, leaving a `.ts` open that never rolled. Reachability
said healthy. Only coverage knew. That is F-03 stated as a demonstration rather than a
claim.

**Still owed: the ALERT.** It cannot be compressed. The monitor scores whole CLOSED hours
and needs `FailureThreshold` (default 2) consecutive bad ones, so the shortest honest run is
just over two hours — an overnight or long-running fleet, not a bench script. The harness to
do it is now built and written down; what it needs is time.

### BENCH 2026-08-21 — the ALERT ✅ (15/15, on shipped defaults)

The "cannot be compressed" reading above was wrong, and the reason is worth keeping: the
monitor scores **the previous closed hour on every sweep**, so both hours it is about to
score are already in the past and can be seeded *now*. That turns "wait two hours" into
"wait for one hour boundary", without touching `MinCoveragePercent` (95) or
`FailureThreshold` (2) — compressing those would have benched different software from the
one that ships.

Docker, one container, four cameras: subject at ~33% coverage for two consecutive hours, a
healthy control at 100%, a recording-disabled control, and an offline camera at 33%.

- **The alert fired 31 seconds after the hour boundary**, on the sweep that scored the
  second bad hour — and was **silent after the first**. That silence is the half that is
  easy to get wrong and impossible to distinguish afterwards: a monitor that alerted on the
  first bad hour would look identical at the end of the run.
- Correct camera, `critical`, window `1787277600`, `coveragePercent: 33.33`, fired **once**
  rather than once per sweep, no spurious recovery notice.
- The healthy control never alerted (this is what decides whether the feature survives a
  real site rather than being muted), and the recording-disabled control was never scored —
  which is also what keeps detect-only AI streams out.
- Both gauges exported: `mymatasan_recording_gap_cameras`,
  `mymatasan_recording_coverage_percent{camera="3"} 33.33`.
- **Offline attribution holds.** On identical 33% coverage, the offline camera reported
  `reason: "camera-offline"` with a body ending "— the camera was offline", while the
  reachable one reported `unexplained`. One incident reads as one story.

**Not benched, mutation-tested instead:** the disk-guard pause suppression needs the guard
to actually trip. Deleting the `transition == "gap" && paused` check makes
`TestContinuityDoesNotAlertWhileTheDiskGuardHasPausedRecording` fail with a useful message,
which is the evidence available without filling a disk.

**Known limitation, accepted:** `alerting`/`lastScoredHour` live in memory, so a restart
re-raises an alert for a gap that is still ongoing. On shipped defaults that costs one
duplicate notification an hour after a restart. Restating an ongoing gap after a restart is
defensible; persisting the state is not worth a table.


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

### BENCH 2026-08-19 — covered ✅, frozen ✅, recovery ✅, **moved was BROKEN**

Benched against live frames: a real mymatasan node pulling a real RTSP stream, with the
scene swapped underneath it. Tamper timings were compressed through the settings (2s
samples, 3-sample streak, 10s frozen) — legitimate here because tamper is SAMPLE-driven,
unlike continuity, which scores whole closed hours and cannot be sped up.

What passed, on real video:

- **COVERED** fired on a bright, edge-free scene (uniform mid-grey, mean luma ~0.69 — well
  above the 0.12 low-light gate, so it was a genuine test and not one the low-light
  suppression would have swallowed).
- **RECOVERY** fired: "Camera view restored" when the scene came back. This is the crux the
  design cares about — alerting samples are excluded from the baseline, so a covered lens
  does not quietly become the camera's new normal and self-clear.
- **FROZEN** fired independently on a SHARP still frame, and did NOT co-fire "blocked" —
  so the two verdicts really are separable rather than one anomaly detector with two names.
  (On a uniform grey scene both fire, which is correct: a bag over the lens is also a
  picture that has stopped changing.)

**MOVED never fired, and cannot.** It is not the bench: it is unreachable by construction.

```go
if prev != nil && vision.HistogramDistance(prev, fp) >= cfg.MovedDistance {
        verdicts[TamperMoved] = true          // prev = the PREVIOUS SAMPLE
}
...
if verdicts[kind] { st.streak[kind]++ } else { st.streak[kind] = 0 }   // needs 3 in a row
```

The signal is the distance between CONSECUTIVE samples — which is transient by nature —
while `settle` demands `FailureThreshold` (default **3**) consecutive samples carrying the
verdict. A camera that is physically turned changes its view ONCE: sample N differs wildly
from N-1, then N+1 matches N because the new scene is stable. The streak resets to 0 on the
very next sample and can never reach 3.

Covered and frozen do not have this problem because both are measured against something
PERSISTENT — covered against the camera's rolling baseline, frozen against elapsed seconds.
Moved is the only one comparing two adjacent samples, and it is the only one that also
demands persistence.

Confirmed live: a scene swap that inverts the entire brightness distribution (`negate`,
which preserves every edge so it cannot be mistaken for "covered") produced no moved alert
after 100+ seconds. And **no test anywhere drives TamperMoved to an alert** — the only
reference to `MovedDistance` in the whole test suite is the settings-normalisation test, so
the defect was invisible to a green suite.

### FIXED 2026-08-20 — all three verdicts now fire (#176)

**The fix** (`fix/mymatasan-tamper-moved`) measures MOVED against a rolling REFERENCE
histogram — the per-bucket median of the last 30 samples, renormalized — so a re-aimed
camera stays different from its remembered normal for as long as it stays moved. Alerting
samples are excluded from that reference, exactly as they already were for covered, so a
camera left facing a wall cannot adopt the wall as its normal and self-clear. Two guards
came with it, both mirroring rules covered already had: suppressed in low light, and
suppressed while the lens is covered (one physical event must not raise two alarms, and you
cannot tell where a camera points when you cannot see out of it).

**Live proof after the fix** — real node, real camera, scene swapped underneath:

```
08:13:03  critical  Camera view changed      <- the re-aimed camera; impossible before
08:15:30  info      Camera view restored     <- and only when it was actually put back
```

It stayed outstanding for 2m27s while the camera kept pointing elsewhere — more than two
full reference windows — which is the exclusion working on live video rather than in a fake.
The bench scene was measured before it was trusted: 7.7x the baseline edge energy (so it
could not pass by tripping COVERED) at histogram distance 0.93 against the 0.55 threshold.

**Eleven new tests, none of which existed. Seven mutations were tried and all seven failed
loudly**, including restoring the original consecutive-sample logic, which fails all three
moved tests with "got 0" — so the suite would now catch the shipped defect.

One case came from reasoning rather than a failing test, and is the reason to read the fix
rather than just trust it: frames taken while the lens is COVERED are kept out of the
reference too. A lens covered for an hour would otherwise fill the window with featureless
grey, and uncovering it would look like movement — clearing one alarm would instantly raise
another, blaming an operator for moving a camera they had just fixed.

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

**W2-1 · Fleet configuration policy + drift** (F-06). **Shipped — benched twice.**
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

**Benched again against a REAL TWO-NODE FLEET (2026-08-19)** — containerised control
plane, two adopted mymatasan nodes holding certificates issued by the real fleet CA,
both dialing the real mTLS control channel on :39533. This is the half the HTTP bench
deliberately skipped, and everything it asserts is about the transport and the fleet
rather than the logic:

- a settings read reaches the node **over the control channel**, and an enforcing policy's
  **PUT body survives the tunnel** — the asserted `admin` role resolves through
  `normalizeControlRole` to the node's superadmin, and the node's OWN audit trail
  attributes the change to **`cp:fleet-policy`**, not to a local admin who was never there
- **precedence across a real fleet**: a fleet policy, a site policy on the site node-b sits
  in, and a node policy on node-a. Each contested field is won by the most specific policy,
  and the report NAMES the winner — node-a's coverage from "Lobby exception" (node scope),
  node-b's from "Airport regulator" (site scope), and the field neither override mentions
  falls back to "Estate standard" (fleet scope) on both
- **enforce is per field, not per node**: node-a's enforcing node-scoped policy corrected
  its coverage while the two report-only fields won by the fleet policy were left alone —
  so node-a reads `drifted` with `driftCount` exactly 2 and `appliedCount` 0 on the
  following pass. A node is drifted if ANY governed field disagrees, which is right
- the enforced write left every ungoverned field in the section byte-identical
- report-only wrote **nothing** to node-b
- **idempotent**: a second pass over the corrected field produced no new `policy.enforce`
  audit entry
- `docker stop node-b` → the next sweep reported it **`unknown`** with `driftCount` 0 and
  the reason "node is not connected", while node-a continued to be judged normally
- unauthenticated read and write both refused; a policy naming a section its node kind does
  not have, and a policy with no settings, are both refused **in a readable sentence**

Only the SCREEN was unexercised — the API beneath it was. **Loaded in a real browser
2026-08-22** while benching W2-4 (`tools/fleetbench/uicheck.js`, same fleet): it renders, the
compliance tiles and the "unknown is not compliant" note read correctly against a two-node
fleet with no policies, and the console is clean. Still not exercised in Arabic. Worth a look in Arabic when
somebody is next in the UI; the page uses logical CSS properties throughout.

**W2-2 · Node state history + SLA reporting** (F-08). **Shipped — benched against a real
two-node fleet (2026-08-21), 33/33.** `NodeStateEvent` (append-only transitions of
`ManagedNode.Status`, one row per CHANGE, keyed on `NodeId`) written by the four paths that
change a node's status, plus `FleetMonitorGap` (spans when the control plane itself was not
watching, DETECTED from a sweep watermark rather than declared, so a crash and a `kill -9`
are covered identically). `services/node_availability.go` turns both into availability per
node / per site / per calendar month, served by `GET /api/nodes/availability` and rendered
into the fleet health PDF as a new Availability section — which replaces that report's
"historical uptime is not yet tracked" footnote. Both tables ride in the `.selbackup` fleet
section. See `docs/modules/apps/myseliasan/services/node_availability.go.md`.

**Three decisions, each because the easy alternative flatters the vendor.** Time nothing was
watching is subtracted from the denominator and reported separately, never counted as uptime.
A node with no measured time is "no data", not 100%. Availability is FLOORED, never rounded:
98.999% reads 98.99, because an SLA written at 99% is either met or it is not. Rollups
aggregate node-seconds rather than averaging per-node percentages, so a node adopted yesterday
cannot weigh as heavily on a site's month as one that ran all of it.

**What the bench found — a real defect in the shipped-to-that-point behaviour.** A node was
stopped for 94 seconds and the report recorded a **10-second** outage. The transition was
dated to the sweep that DECLARED the node lost, which by construction runs a full grace window
(≥90s) after contact was actually lost, so every outage was short by that window, always in
the vendor's favour. Fixed by dating a `lost` transition to `ManagedNode.LastSeenAt` — safe
because that field is stamped by the control plane's own clock on every path that sets it,
never by the node, so there is no remote skew to import. The code comment justifying the
original choice had asserted the opposite and was simply wrong on the facts. Re-benched: 99s
recorded against 100s of real outage.

**The mutation pass also earned its keep**: 11 mutations, and the one that initially SURVIVED
showed that the test named "floors to two decimals" did not distinguish flooring from
rounding — the "never a perfect score" clamp was covering for it. The test now uses 98.999%,
where the clamp cannot help.

**Deliberately not built:** a screen. The PDF report is the deliverable the finding named, and
the JSON endpoint is there for one later. Same scope call as W1-2.

**W2-3 · Critical-clip archive** (F-09). **Shipped — benched against a real fleet with
real footage (2026-08-21), 22/22.** A per-rule `ArchiveClip` flag on the node
(`entities.DetectionRule`, default OFF) rides upstream on each alert as the notification
Data key `archiveClip`; the control plane queues an `ArchivedClip` job, pulls the event
clip over the existing tunnel in bounded byte ranges, hashes it as it arrives, and stores
it encrypted at rest with its snapshot. `GET /api/clips*` reads it back, Range-capable and
audited. See `docs/modules/apps/myseliasan/services/clip_archive.go.md`.

**PULL, NOT PUSH — a deliberate departure from the sketch above.** A push needs the node
to hold a durable queue, retry against a control plane that may be down for a week, and
manage the disk that queue consumes — on the appliance whose disk is already the scarce
resource and already pauses RECORDING when it fills. A pull needs none of it: the control
plane already learns of every alert live, and again through the 72h reconnect replay, so
it always knows what it owes. The queue lives where the database and the operator are.
**Retry is not a mechanism, it is a row that is still pending.** The transport already
existed too — ranged segment fetch over the tunnel, built for playback.

**The hook is on `republishNodeNotification`, not on the live event path**, because that
one function is also what the reconnect replay funnels through. Hooking the live path
alone would have archived the easy half and silently skipped every clip raised while the
link was down — the exact case the feature exists for. The bench proves it: the control
plane was stopped, an alert raised on the node, and the clip archived after reconnect.

**What the bench found.** An alert raised without an image made the archive store the
node's JSON refusal as the "snapshot" — a file that would show as a broken image months
later and cast doubt on everything else in the archive. Snapshots are now signature-checked
(`looksLikeImage`), and the API's "unavailable" message distinguishes a missing snapshot
from missing footage, because "the clip is not available (state: stored)" reads like a
contradiction and sends the reader hunting a bug that is not there.

**The headline assertion is the product claim itself**: the bench destroys node-a, wipes
its data directory, and then plays the clip back from the fleet — byte-identical to the
digest taken as it arrived, with the node and camera still named on the record because
those are snapshotted at archive time rather than resolved on read.

**Deliberately not built:** a screen, and no place in `.selbackup`. The bundle is a
CONFIGURATION backup; rows without their gigabytes of media would restore into a list of
incidents whose footage is missing, which is worse than listing nothing.

**W2-4 · Federated cross-node search** (F-10). **Shipped — benched against a real two-node
fleet (2026-08-22), 36/36.** `FleetSearchService` on the control plane scatter-gathers over
every node the caller's role can reach (bounded parallelism, a per-node deadline), merges
newest-first, and serves `GET /api/nodes/search` + `/search/labels`. Each node answers with
`SightingSearch`: object presence intervals from the metadata recorder, AND the plates and
faces recorded on its alert events, joined to its own camera names. Site scope, plate/person
text, object class, time window and confidence are all query terms. The fleet Object Search
screen now calls it instead of fanning out from the browser.

**THE QUERY TRAVELS, NOT THE INDEX — and the reason generalises.** The plan offered a choice:
federate the query, or replicate a compact index upward. The observation index is the
highest-volume derived data in the product, so replicating it grows the control-plane
database with the whole estate's detection volume and needs a queue on every appliance, its
own retention, and a backfill for everything written while the link was down. The node
already holds the index AND the footage the answer must link to; the control plane already
has an authenticated, authorized, audited transport to it. **Same shape as W2-3's pull-not-push:
before adding state to the device, ask what the other end already has.**

**THE COST OF FEDERATING IS THE FEATURE.** An offline node contributes nothing — so every
result carries a coverage block naming each node, its outcome (`ok | offline | timeout |
denied | unsupported | error`) and a reason, plus how far back the answer is actually
complete. Replication would have hidden the same gap behind stale data, which is worse: it
answers confidently and wrongly. **An investigator must be able to tell "the fleet never saw
it" from "the recorders that would have seen it were not asked", and only the search itself
can tell them.** The browser-side fan-out this replaces could not: its per-node `.catch`
returned an empty list, so an unreachable recorder simply produced fewer rows, and its label
fan-out was silently capped at 25 nodes.

**Two grants, not one, and that decided where the routes live.** Objects are served from
`/api/observations/search` and identities from `/api/vision/alerts/identities`, because
identities live on alert events — so a role with the Objects page but not the AI log cannot
learn who was recognized by asking through the control plane. The control-plane routes sit
under `/api/nodes` for the opposite reason: the prefix grant every fleet role already holds
covers them, so shipping the feature did not quietly take it away from every non-superadmin.
A per-source refusal is reported as `denied` rather than rolled up as a healthy node.

**What benching found.** The live UI check — not the API bench — caught it: sightings from
cameras that record nothing were labelled "Recording…" forever, because "newer than this
camera's newest footage" is true by construction on a camera that keeps none. Fixed by
consulting the recording configs to label, never to filter. **A promise the system cannot
keep is worse than an honest blank.** The mutation pass then found the test gap that fix
opened: nothing covered the case where the recording config cannot be READ, where the honest
answer is the softer one. 39 mutations, all killed.

**Deliberately not built:** paging. There is no offset parameter, because a global offset
cannot be honoured across independent sources — each node pages its own result set — and the
usual workaround (over-fetch then slice) drops real rows the moment any node caps. A wide
search is narrowed by its time window, and the coverage block says when it is too wide.

**W2-5 · Staged rollout** (F-07). **Shipped — benched against a real two-node fleet (2026-08-22), 22/22.** `FleetRolloutService` plans a fleet upgrade
into RINGS (a canary first), drives them over the existing control channel on a leader-gated
tick, and requires each ring to pass a health gate before the next one starts. Served by
`GET/POST /api/fleet-rollouts*`; audited at every transition.

**THE NODE NOW KNOWS WHAT IT IS RUNNING, AND SO DOES THE FLEET.** `ManagedNode.Version` is
captured from the control-channel hello — which already carried it and which
`control_server.go` previously logged and threw away. It costs nothing on the wire and it is
the only durable answer to the first question any upgrade asks. It is REPORTED, never
assigned: a field the control plane wrote from its own intent could only ever agree with
itself.

**"PROVE ITSELF" IS THREE CONDITIONS, NOT ONE.** A ring passes only when every node has come
back on the control channel, has REPORTED the target version, and has held that for a settle
window. Each catches a different failure: a node that never returns, a node that returns on
the OLD version because the swap silently failed, and a node that boots, looks healthy and
dies a minute later. Trusting the update command's 200 — which only means the node agreed to
try — is the version of this feature that reports success while the fleet burns.

**HALT, NOT ROLLBACK — a deliberate departure from the sketch above.** Rolling a binary back
is easy; rolling a DATABASE back is not, because migrations here are forward-only
(`infra/db/bootstrap.Migration` has no down step). An automatic rollback would hand a node
that has already migrated its schema a binary that has never seen it — turning a bad release
into a corrupted appliance, unattended, across a whole ring. So a failed ring stops the
rollout with a reason, leaves the rest of the fleet on the version it was already running,
and waits for a human. The node refuses downgrades for the same reason, before anything is
overwritten. **A fleet is recovered by rolling FORWARD to a fixed build**, which this same
machinery does.

**The version is PINNED, which also closes the fleet-bricking trap properly.**
`selectAssets` used to ignore its `tag` argument (`_ = tag`) and always read
`releases/latest`; it now looks up `releases/tags/v<version>`. That is required — a canary is
meaningless if the node resolves "latest" for itself — and it means the updater no longer
depends on the three sibling apps continuing to publish with `--latest=false`. The
`mymatasan_` asset-prefix guard stays.

**What the bench found.** The first run halted on its canary with "self-update is not
available for this install type": the rollout was planning across nodes that can NEVER
replace their own binary (a container image, a package-managed install), and only found out
by failing on one. Capability is now probed at PLAN time — such nodes are recorded
`unsupported` with the reason and the actual remedy, excluded from the rings but still
listed, so a plan that covers nine of twelve appliances says so instead of reporting complete
success over a fleet it never touched. A plan where nothing is updatable is refused. An
UNREACHABLE node is deliberately treated as capable: excluding it would quietly shrink the
rollout to whichever appliances were awake when an operator happened to press plan.

**W2-6 · Instrument dropped events** (F-11). **Shipped — benched against a real two-node
fleet (2026-08-23), 19/19.**

**THERE WERE THREE SILENT LOSSES, NOT ONE.** The finding named the first; the other two came
out of reading the path end to end.

1. `ForwardEvent` returned with no record when the channel was down — AND discarded the
   write error when it was up but failing. Both now counted, by kind and by reason
   (`disconnected` / `write_failed`), with successful forwards counted alongside, because a
   drop count with no total is a number nobody can size.
2. **Nothing watched the replay window's clock.** Missed events are recovered on reconnect
   from the last 72h of the node's own notifications — a sound design, and a promise with an
   expiry date. The fleet screens said "lost" in the same tone at hour 2 and hour 90.
   `ReplayHorizonMonitor` warns at two-thirds of the window and escalates when it lapses,
   naming the moment recovery stopped being possible rather than leaving the reader to do
   arithmetic. Served by `GET /api/nodes/replay-horizon`, which sweeps on demand.
3. `replayNodeNotifications` hit its 50-page (25k event) ceiling and `break`ed **silently**,
   so a replay that recovered a prefix reported itself exactly like one that recovered
   everything — and the remainder never comes back, because the next reconnect starts from
   the same window rather than where this one stopped. It says so now.

**THE PART WORTH REUSING: THE NODE ADMITS ITS OWN LOSSES.** The drop count rides up on the
next hello (`control.Frame.Dropped`), counted since the last successful hello rather than
since boot, so the control plane learns exactly what went missing during the gap it is about
to replay. **That turns "the replay covers it" from an assumption into something checkable.**
Whenever one side silently compensates for another, ask whether the compensating side can be
told what it is compensating for.

**A HEALTHY NODE MUST EXPORT ZERO, NOT NOTHING.** The bench's last failure looked like a bad
assertion and was not: a Prometheus counter with no samples is absent from the scrape, so a
node that has never dropped an event and a node with no instrumentation at all looked
identical — the exact confusion this item exists to end, reproduced one level up. Both
counters are now published at zero for the kinds a node forwards.

**Testability note:** the manager's live connection is now held as a one-method
`frameWriter` rather than `*control.Conn`. Both failure paths — which are the entire point
of the file — could not otherwise be exercised without standing up a websocket, and two
mutations survived until they could be.

**W2-7 · Email notification channel** (F-20a). **Shipped — benched 2026-08-23 against a real
two-node fleet and a real SMTP server, 34/34, plus screen passes on both apps.** Phase 2 is
complete.

`infra/notification/mail_channel.go` beside the existing six channels, delivering through
`infra/mailer` — which grew multi-recipient, custom headers and MIME attachments to carry it
(`message.go`). `email` is now a destination type in `domain/notification`'s single dispatch
switch, so mymatasan gets it in the per-destination model and any future app gets it free.

**THE FINDING NAMED A CHANNEL; READING THE PATH FOUND A MISSING LEG.** `apps/myseliasan`
built a notification `Service` and **never called `Configure`** — the control plane had no
outbound delivery of any kind. Every node-offline, every relayed alert, every monitoring gap
was persisted, logged, streamed to any open browser, and stopped there. An operator watching
fifty sites had to be looking at the screen to learn one had gone dark, which is precisely
the moment nobody is looking at a screen. It also made two config blocks lies:
`notification.webhook` and `notification.telegram` had been in the shared model all along and
were never consumed by that app. **When an item says "add a channel", check the other end is
actually wired to any.**

**THE RELAY IS ONE PER INSTALL, NOT ONE PER DESTINATION** — the design call worth reusing.
Recipients are routing (changed often, by whoever decides who gets told); an SMTP relay is
infrastructure (changed once, by whoever runs the mail server, and audited). Putting
credentials on every destination row would multiply the secret to rotate and would let a
notification screen quietly open egress on an install whose config says mail is off. It also
gives an air-gapped deployment one place to look. Same instinct as W2-3/W2-4/W2-5: ask which
end already knows enough.

**PARTIAL DELIVERY IS A SUCCESS, AND THE ERROR CONTRACT IS THE WHOLE FEATURE.** When a relay
accepts some recipients and rejects others, the message IS delivered and must NOT be retried.
Failing the send would let one stale address in a distribution list silence the alert for
everybody else — and because the worker retries, it would do so on every alert, forever.
Only "nobody received it" is a failure, and even then a by-name rejection is permanent, since
retrying the same list cannot change the answer. Three of the mutation-checked assertions
exist for exactly this table.

**A GUARD THAT CANNOT DELIVER MUST BE REFUSED AT SAVE TIME.** A username with STARTTLS off
produces a relay that silently never sends, because the sender will not put a credential on a
cleartext link. Both apps reject it when it is saved rather than when an incident needs it.
The same reasoning added `POST /api/settings/notification/test` to myseliasan — the control
plane already had a "test this connection" button for Redis and none for the alerting path.

**WHAT THE MUTATION PASS FOUND, AND IT IS THE RECURRING ONE.** 34 mutations, 34 caught — but
only after one survivor exposed a test named `RefusesCleartextCredentials` that never reached
the guard it named: with `UseStartTls: true` the "STARTTLS not offered" check returns first.
The rewritten test points the sender at a relay that OFFERS `AUTH` and asserts the password
**never reached the wire**. *A test named after a guard is not evidence it runs.*

**WHAT THE SCREEN PASS FOUND.** The snapshot-delivery hint listed "webhook/MQTT base64,
Telegram photo" and not email, so an operator on an email destination could not tell whether
the control applied to them — the same class as W2-4's "Recording…" label. Fixed in four
languages. The mymatasan screen check also TYPES: the recipient list is an array edited as
text, and a controlled textarea derived from `to.join()` cannot be typed into at all, because
parsing eats the newline the instant Enter is pressed. It renders, the API accepts, and the
feature is unusable.

**Not claimed:** a snapshot attachment travelling end to end. The harness runs no cameras, so
no notification carrying an image was raised. The MIME assembly is unit-tested with a
decode-and-compare round trip and mutation-checked; it has not been benched live.

---

# Phase 3 — Win the bake-off

Target ~8–12 weeks, ordered by how often each decides a competitive evaluation.

**W3-1 · Timeline playback** (F-12). **Shipped — benched against real recorded footage
with a real hole in it (29/29), plus screen passes in English (23/23) and Arabic (24/24).**

Scrub bar shaded by coverage, seek across segment boundaries, multi-camera sync, speed
control, detection events plotted as jump targets. `GET /api/recording/timeline` returns
each camera's playable segment index plus the merged spans the bar shades;
`GET /api/recording/timeline/seek` resolves one wall-clock moment to a (segment, offset)
per camera. A new Timeline screen sits beside Live View in the nav.

**THE UNIT IS A MOMENT, NOT A FILE — and that is the whole design.** Everything on screen
is a rendering of one number, `cursor`, in unix seconds. Multi-camera sync then falls out
rather than being built: the tiles do not synchronise with each other, they each answer the
same question about the same instant. A camera with no footage at that instant says so,
which is exactly what an investigator is looking at a wall to find out.

**THE SEEK RESOLVER ALREADY EXISTED.** `pickCovering` in `services/observation.go` —
which resolves an object sighting to its footage, including preferring continuous footage
over a short event clip that overlaps it — is the same rule playback needs. It was reused,
not reimplemented, and its paging loop was lifted to a package function both call. A second
copy would have meant the Objects screen and the timeline disagreeing about which file
holds a moment, which is one fact described two ways. **Before writing a resolver, check
whether some other screen already resolves the same thing.**

**IT IS A SERVER CALL ON PURPOSE, not arithmetic over the index the browser already holds.**
Two reasons, and the second is the one that decided it: there is no JS test runner in this
repo, so a client-side copy of the rule would be the one piece of load-bearing logic with no
test at all; and an appliance under disk pressure evicts its oldest footage continuously, so
an index fetched a minute ago can name a segment that no longer exists. Asking at seek time
turns that into an honest *"that footage is gone, the next is at 04:12"*.

**THE BAR AND THE COVERAGE REPORT ARE ONE FACT.** `coveredSpans` was factored out of
`recording_coverage.go` so the shading and the percentages come from the same merge. A bar
that shades an hour the coverage report calls empty is a bar that promises evidence, and the
bench asserts the two agree to 0.01% on real segments.

**IT REFUSES RATHER THAN TRUNCATING.** `GetSegments` orders newest-first, so a capped read
silently drops the OLDEST segments and the LEFT of the bar renders empty — and empty on a
scrub bar does not read as "we did not fetch this", it reads as "there is no footage here".
Over the segment cap the request is refused with the fix named ("ask for a shorter window").
Same reasoning as W2-7's refuse-at-save-time.

**THE ROUTES LIVE UNDER `/api/recording` and the detection marks do NOT.** The Recordings
page grant is `canRead("/api/recording")`, so every role that can already browse footage
gets the timeline on upgrade; a new prefix would have shipped the flagship feature invisible
to everyone but a superadmin (the W2-4 lesson). The marks come from `/api/vision/alerts`,
a different page grant — an operator granted Recordings but not Alerts must not learn what
the AI recognised by scrubbing a bar. That split needed no new endpoint at all: the alerts
API already takes field filters, so the browser asks it directly and renders no marks on 403.

**WHAT THE BENCH PROVED THAT NO UNIT TEST COULD.** Every seek in the product is
`at - startedAt`, which is only a real offset if the file holds as many seconds as its row
claims to span. The bench downloads a segment and asks ffprobe: 60.00s of media against a
60s recorded span. It holds because `infra/recording.segmentEndedAt` stores StartedAt plus
the PROBED duration rather than the moment the recorder closed the file — so a segment
truncated by an ffmpeg crash reports the length it actually contains. Had that not held,
every scrub would have been silently off by however long the stream stalled, and the player
would have looked like it was working.

**THE SCREEN PASS FOUND FOUR DEFECTS. THE API BENCH PASSED 29/29 THROUGH ALL OF THEM.**
Three are set out below and the fourth, which only appeared in Arabic, follows them. Every
one is invisible from the API side, and two of them make the feature wrong rather than
merely ugly. This is now the strongest evidence in the programme for the rule that an
item touching a screen owes a browser pass.

**1 · Every scrub landed later than the click.** The bar hit-tested the pointer against the
surrounding panel, while the shaded spans, the detection marks and the cursor are all
positioned inside the TRACK — which is inset by the fixed-width camera label at the start of
each row. The result was a constant offset of about 5.6% of the visible window: three
minutes on an hour, eighty on a day. Nothing about it looks wrong — a segment loads, the
readout shows a time, the frames play — so it survives every check except one that compares
where the cursor came to rest with where the operator clicked. That comparison is now an
assertion. **When a click is mapped to a value, measure against the element the marks are
drawn in, and prove the two agree.**

**2 · The window control could not move the window.** A keep-the-cursor-visible effect ran
unconditionally and in both directions, so setting "ending at" to last night snapped
straight back to wherever the playhead sat — the control was inert for the one thing it is
for. It now follows only while PLAYING and only forwards, and moving the window by hand
takes the cursor with it. A second, narrower version of the same fight remained afterwards:
a stale `timeupdate` from the clip still decoding kept reporting the old position, which
dragged the window back after a manual jump. Fixed by ignoring any position reported by an
element that is not playing the segment the cursor is being computed from.

**3 · Two controls edited in one tick discarded each other.** The window's end and its width
were separate pieces of state, so changing the zoom and then the end time — an entirely
ordinary sequence — applied one and silently dropped the other, each handler having read the
other's pre-change value out of its closure. The window is now a single value mirrored into
a synchronously-updated ref, so the second edit composes with the first.

**AND THE ONE IN ARABIC.** The API bench passed 29/29. Driving the
screen, the tile that had to skip a hole rendered **"تم التخطي 0 دقيقة"** — *skipped 0
minutes* — because the note rounded the gap to whole minutes and the hole was 43 seconds.
That note exists for exactly one purpose: to let an operator tell "this camera missed the
incident" from "the player put me in the wrong place". Rounding it to zero states that
nothing was skipped, on the one tile whose entire job is to say that something was. Fixed
with a magnitude-following formatter (seconds / minutes / hours) that can never render zero.
**A quantity whose whole meaning is "this is not nothing" must not be rounded.** It surfaced
in Arabic and not in English purely because that run's click happened to land nearer the end
of the hole — which is an argument for running the screen pass in more than one language on
its own merits, quite apart from checking the direction the layout mirrors.

**THE BENCH'S OWN BUG, seen for the fourth time in this programme.** The first version of
the zero-gap check was `notes.every(...)` — trivially true on an empty array — and it passed
on a run that never reached the hole. The gap click was also aimed at a fixed fraction of a
window whose right edge is *now*, so it drifted out from under the click as the bench ran.
Both fixed: the window is pinned before the click, and the assertion requires a note to
exist. **A check that cannot tell "the thing did not happen" from "I failed to make it
happen" is not a check.**

**Deliberately not built:** hover thumbnails on the scrub bar. `/segments/{id}/frame` would
serve them, but each hover spawns ffmpeg on the appliance, and a scrub bar generates hovers
continuously. Not worth the load for this item; revisit with a pre-extracted sprite sheet.

**W3-2 · Appearance search** (F-16). **Shipped — benched three ways: 11/11 against the real
model on real pixels, 34/34 against a real two-node fleet, and screen passes of 17/17 in
English and 18/18 in Arabic.**

An operator picks a recorded sighting and asks where else it went. `GET
/api/observations/appearance` ranks that node's sightings; `GET /api/nodes/search/appearance`
asks the whole fleet. A "Find similar" action sits on every object-search row.

**NO THIRD-PARTY MODEL, AND THE DECIDING CONSTRAINT TURNED OUT TO BE LICENSING.** The
descriptor is 560-d: a 512-d ImageNet embedding from the resnet18 the ANOMALY feature already
installs (renamed `_crop_backbone`, since it now serves two stages) plus a 48-d two-band
hue/lightness histogram.

The first instinct was air-gap — no download. That reasoning was **wrong and worth
correcting**: `yolo11n.pt` is 5.6 MB, tracked in git and shipped by goreleaser, so the
constraint forbids runtime egress, not bundling weights. The real blocker is licensing.
Re-identification code is usually permissive, but the published checkpoints are trained on
Market-1501, DukeMTMC-reID or MSMT17 — all research-only, and **DukeMTMC was withdrawn by its
own authors over consent concerns.** Weights derived from non-consensual surveillance footage
are not shippable in a product that is sold, and that is a sharper objection than any
technical one. Histograms over pixels carry no such freight.

**THE COLOUR HALF IS THE HALF THAT WORKS.** The embedding alone separated a red figure from a
blue one by 0.033; colour alone separates them by 0.115, and the combined descriptor by
0.074. An ImageNet backbone is trained for CLASS invariance — it answers "person" whatever
they are wearing — which is precisely the opposite of what appearance search needs, and it is
also the opposite of what an operator means, since nobody searches for a torso texture.
Weight held at 1.0 rather than favouring colour: the bench scene is flat-coloured rectangles
under even light, the best case colour will ever see and the worst case shape will ever see.

Every row stamps its model, and vectors from different models are never compared — a swap
degrades to "no older matches" rather than to confident nonsense. **If you ever want more:
CLIP image embeddings are MIT-licensed and far more attribute-sensitive than an ImageNet
classifier; the cost is ~150 MB and heavier inference on an appliance already running
detection.**

**THE BENCH KILLED THE CALIBRATION, AND THE FIX IS THE MOST REUSABLE THING HERE.** Measured
on the real model: two crops of the SAME subject score **0.9825**; a red figure against a
blue one scores **0.9498**. ImageNet features are dominated by structure — a person-shaped
thing on a background — and discard most of what an operator means by "the man in the red
jacket". So the shipped 0.45 similarity floor filtered nothing, the "strong ≥ 0.75" band
marked every row strong, and the screen would have printed "95% match" between two unrelated
people. Not a weak feature: a wrong answer delivered confidently, on a screen someone acts on.

The ORDER was always right. Only the absolute number was meaningless. So a hit is now scored
by **how far it stands out from the other candidates compared** — a robust z-score,
deviations above the MEDIAN scaled by the median absolute deviation, the same shape the
dashboard's baseline analytics already use. That is self-calibrating per site, per camera and
per class, with no threshold for anybody to tune. **Ask of any similarity score: what is its
RANGE on real data? A metric that only ever returns 0.94–0.99 is an ordering, not a verdict,
and presenting it as a percentage is the bug.**

Median and MAD rather than mean and standard deviation, because the thing being searched for
is an outlier: if somebody walked past six cameras, six near-duplicates are in the candidate
set, a mean climbs towards them and a standard deviation widens, and the very matches the
search exists to surface are the ones it flattens. Below twelve comparisons it reports
`calibrated: false` and says so on screen rather than dressing three results up as a finding.

**FEDERATION IS TWO HOPS, and the second one is easy to miss.** The operator names a sighting
on one node; that id means nothing anywhere else. So the control plane fetches the DESCRIPTOR
from the node holding it, then fans the descriptor out. A version that passed the id around
would return results only from the node that recorded it — and would look like it worked. The
first hop is authorized identically to the search, or it is a read the caller does not have
dressed up as a query. Merge order is by standout, not raw similarity: each node calibrates
against its own crowd, so the two are not comparable quantities across nodes.

**WHAT THE FLEET BENCH FOUND: "Purge now" was not purging.** It destroyed a camera's footage
and snapshots and left the object index intact — a searchable record of every person and
vehicle the camera saw, pointing at footage that no longer exists. Appearance search made
that materially worse by hanging a descriptor off each row. `POST /api/recording/purge-camera`
now cascades to the metadata and reports the count. **A destroy action must be checked against
everything DERIVED from what it destroys, not just the primary artefact.**

**Two seams became shared rather than copied**, both of which would have been bugs as copies:
the at-rest vector codec (now one implementation with the face gallery) and `pickCovering`,
so a sighting found by ranking opens the same footage the Objects grid would open for it.

**Not built, and stated because the gap is real:** no end-to-end proof that a camera watching
a real person produces a stored descriptor. The harness films synthetic test patterns, so the
detector finds no person to describe. Part one of the bench covers the embedding on real
pixels, the unit tests cover the recorder's hand-off, and the join between them is not
exercised. Also not built: searching by an uploaded photograph, which is a different feature
with a different risk profile — it turns a review tool into a watchlist.

**W3-3 · Cases + video wall** (F-17, F-18) was SPLIT into two PR-sized items. They share a
row on the original register and nothing else: one is an investigation container, the other
is a wall of live tiles.

**W3-3a · Case files** (F-17) — SHIPPED and benched on `feat/case-files`. Bookmark a
moment or a sighting into a case from the Timeline or Objects, annotate it, assign it, close
it with a stated outcome, and export the whole thing as one bundle. This is the natural home
for W1-4's export bundle and W1-2's audit trail, and the three did want to be one feature: the
case bundle reuses `plan`/`materialize`/`concat` unchanged, and ships the case's own audit
entries as `chain-of-custody.csv`.

**The design decision that makes it a product rather than a demo: an OPEN case holds its
footage.** Retention, the per-camera "Purge now" and the disk-pressure sweeper all refuse to
delete footage a case's evidence points at. Without it, on a box with seven-day retention, a
case opened today is a list of broken links next week — silently, because nothing about the
case changes. Footage leaves this appliance by FOUR paths and the fourth is the trap: the
recorder runs its own hourly sweep over the live directory, deleting by filename age with no
view of the database, so a hold enforced only in the service layer is undone within the hour
and leaves rows pointing at nothing. It is wired through a predicate on `RecorderConfig`.
Secure wipe, factory reset and deleting a camera still destroy footage regardless — a hold
that could block those would turn a case into a way to make footage undeletable. The hold
FAILS CLOSED: a hold that cannot be read means everything is held.

Also fixed in passing, found by reading the path rather than by a test: **the operator role
could start an evidence export and could not download it.** `/api/evidence` was granted POST
only, and a bundle is built asynchronously — so the rung the role model describes as "may
export footage" conferred the right to begin an export and nothing else. W1-4's bench never
caught it because it ran as an administrator.

**WHAT THE BENCH FOUND — four, and the first is in code that shipped in Phase 1.**

1. **The bundle's media was longer than the bundle said.** An eighteen-second bookmark
   exported as sixty seconds of video. An export is whole stored segments joined without
   re-encoding, and the manifest described only the REQUEST (`requestedRange`,
   `coveredSeconds`) — nothing in the bundle said what the file actually contains. A
   recipient counting wall-clock times from the first frame, which is the only thing a
   recipient can do, was out by the difference. **This is in the shipped single-clip export
   too (W1-4), whose bench never ffprobed the media.** The manifest now carries
   `output.startsAt` / `endsAt` / `mediaSeconds` / `requestedOffsetSeconds` and `VERIFY.txt`
   says it in words. The footage is deliberately NOT cut to fit: a stream-copy cut lands on
   a keyframe rather than the requested instant and can break the leading GOP, and handing
   over less than was recorded is a worse answer than handing over more and describing it
   exactly. **ffprobe the media — a manifest can claim anything, and this one was claiming
   it about evidence.**
2. **The chain of custody ended one event before the one that matters.** The custody list is
   read from the audit trail while the bundle is assembled and this export's own row is
   written afterwards, so the bundle never contained "who took a copy of this out of the
   system". Appended explicitly now.
3. **A case bundle could be collected through the single-clip route.** `/api/evidence` and
   `/api/cases` are governed by different page grants and the evidence download never
   checked which kind of job it had, so a role with Recordings-use and no Cases grant could
   pull a whole investigation's footage. Both routes now refuse the other's jobs, and export
   ids gained a random suffix — `exp-<unix>-<counter>` was enumerable inside the six-hour
   retention window by anybody who could call the route at all.
4. **The screen said "Footage gone" about footage the same screen was holding** — found by
   the browser pass, not by the 48 API checks. An item's play link and its missing-footage
   label were resolved from the START INSTANT while the hold and the export work on the
   SPAN, so a bookmark whose opening seconds predated the recording was reported as having
   no footage at all, on a case that was correctly reporting four held clips of it. It now
   snaps FORWARD to the first footage inside the span, the same thing the timeline's seek
   does with a gap, and says when the recording actually starts. **Same shape as W2-4's
   "Recording…" label: a fact computed at one resolution and rendered as the answer to a
   different question.**

**W3-3b · Video wall** (F-18) — SHIPPED and benched on `feat/video-wall`. Named walls, cycling,
alarm-driven auto-pop, and a second-monitor window.

**THE WHOLE FEATURE IS ONE WORD: the arrangement was in a COOKIE.** Live Views already had a
grid, tiles, paging, fullscreen and drag-reorder — and none of it left the browser that built
it. It could not be handed to the next shift, could not be opened on another monitor without
being rebuilt by hand, and vanished when somebody cleared their browser. Moving it into the
database is most of the work; the three behaviours on top (cycle, pop, second monitor) are
properties of the WALL rather than of the browser showing it, which is why they are columns
rather than component state. **When a feature already exists but "does not count", check where
its state lives — that is often the entire gap.**

Cycling and popping interact, and the interaction is the design: **a pop SUSPENDS cycling**,
because a wall that rotates away two seconds after raising an alarm has done the opposite of
raising one. Paging by hand restarts the dwell for the same reason.

**WHAT THE BENCH FOUND — four, and the first is the worst thing found in this programme so
far.**

1. **DELETING A CAMERA FAILED WITH A 500 ON MOST CAMERAS.** The camera-delete cascade clears
   the camera's appearance descriptors; the appearance purge treated "there were none to
   clear" as an error (the generic repo reports a zero-row DELETE as a failure, and the
   guard for it matched the read sentinel but not the write one); the failure aborted the
   cascade by design. Appearance search is off by default, so ANY camera that never produced
   a descriptor — nearly all of them — could not be deleted at all. Shipped in W3-2, missed
   because no bench had ever deleted a camera. **A bench only covers the verbs it uses.**
2. **And the delete told nobody why.** 5xx bodies deliberately hide their detail from the
   caller, which is right, but this path did not log either — so the reason existed nowhere.
   The operator saw "internal server Error" and the support engineer saw nothing.
3. **"Save as new" stole the default wall.** Saving the wall you are looking at under a new
   name inherited its default flag, so a personal variant silently changed what every other
   operator's screen opens with.
4. **The landing route dropped the query string.** `/` redirected to `/app` and discarded
   `?wall=<id>`, so the second-monitor window arrived with no wall and rendered the ordinary
   workspace. It broke every deep link into the app, not just this one.

**The fleet-level wall spanning nodes is still unbuilt** and remains the differentiator no
appliance vendor can match; this is the single-appliance half.

**W3-4 · Loitering / left-behind / directional** (F-15) — SHIPPED and benched on
`feat/dwell-rules`. Three evaluators over the tracker line crossing already used, in
`infra/vision/dwell.go`; no second inference pass, no new pipeline.

**THE WHOLE DIFFICULTY IS THAT A TIME RULE CANNOT BE WRONG FOR ONE FRAME.** A frame rule is
corrected by the next frame and the min-frames streak absorbs the wobble. A time rule that is
wrong for one frame LOSES ITS CLOCK, and a thirty-second threshold then never fires on a real
camera where anybody ever walks behind anything. Every decision in the file follows from
that, and two are worth carrying elsewhere:

- **Not being seen is not the same as being seen somewhere else.** A missed detection is
  missing information and is forgiven; being observed OUTSIDE the zone is information and
  resets the dwell immediately. Conflating them either resets on every flicker or lets
  somebody who steps out and back accumulate a dwell they never had.
- **The forgiveness is counted in MISSED SAMPLES, not seconds.** What is tolerated is a
  dropped detection, and detections only happen when the detector runs — "not seen for eight
  seconds" means nothing on a camera sampled once in that window. The same confusion W2-2
  found in the availability numbers, pointing the other way.

**The alert says when the dwell BEGAN**, not only when it fired: the alert arrives at 14:05
and the interesting thirty seconds start at 14:04:30. **A rule that could never fire is
refused at save time** — no classes (these types have no entry in the static class map, so it
would match nothing silently), or a direction rule with no direction.

**The bench cannot drive the evaluators and says so.** The harness films synthetic test
patterns, so the detector finds no person, bag or vehicle to track; a drawn rectangle is not
a person to a COCO model. What is claimed live: creation, the four refusals and their
wording, survival across a restart, an alert of each type flowing through notification into
the alert log carrying `dwellStartedAt`, and the role model. The evaluators are covered by
unit tests that drive a clock across many samples, plus three mutations — the zone-exit
reset, the unattended check, and the bearing negation — each caught.

**What the tests found while being written:** tracks were being aged by wall clock AFTER
matching, so an expired track was matched back to life on the very pass that should have
retired it, and a seventeen-second absence counted as continuous dwell. Fixed by ageing on
missed samples. And the first direction test moved a person 0.4 of the frame in one sample —
further than the tracker's own matching radius — so it became a new track every step and
accumulated no travel at all: **a synthetic scene has to reproduce the real condition, not
just contain the right answer**, the same lesson as W1-5's 3px bands and W3-2's orthogonal
crowd.

**Found by the Arabic screen pass, unrelated:** a rule's schedule summary ("Always active",
"Daytime only") was hard-coded English and appeared untranslated beside every rule in every
non-English installation. Now localised.

**W3-5 WAS SPLIT.** The row carried two unrelated features — PTZ (F-13) and ONVIF events
with relay I/O (F-14) — on one register line, the same shape as W3-3. The PTZ half is
shipped; the events half is untouched and is now its own row.

**W3-5a · PTZ presets, home, guard tours and alarm recall** (F-13) — SHIPPED and benched on
`feat/ptz-presets`. `infra/onvif/ptz.go` adds presets, home, absolute/relative move and
status; `services/ptz.go` adds guard tours and a recall a detection rule can ask for.

**THE PLACES LIVE ON THE CAMERA.** ONVIF presets are stored by the device and recalled by a
token it issues. Mirroring them into a table would create a second answer to "where can this
camera point", and the two part company the first time somebody uses the camera's own web
page — which is how a large share of PTZ cameras in the field get set up. So presets are read
live, and a tour stores tokens it does not own. **An unreachable camera is a THIRD state,
distinct from "it has no presets":** reading one as the other would stop every patrol in the
building the moment the network hiccupped, and announce each one as broken.

**THE CROSS-FEATURE DEFECT THIS ITEM EXPOSED, found by reading the path rather than by a
bench: the tamper monitor and PTZ are the same physical event.** W1-5's `MOVED` verdict is
"this camera is no longer showing what it used to show", which is the literal, intended,
successful outcome of every preset recall, tour step and jog of the ring. Nothing connected
them. Turning on a guard tour would have made the appliance report tampering on the camera it
was patrolling, every few minutes, forever — and the operator's fix is to switch tamper
detection off, after which it protects nothing. `PTZJournal` is the one place that knows a
view changed because WE changed it: a commanded move makes the monitor **forget** that
camera's baselines (suppression only defers the alert — the old reference outlives the quiet
period), and a camera on patrol is judged on **neither** scene verdict, because a camera that
is supposed to keep changing what it looks at has no normal view to be measured against.
**That cost is stated in the code, in the docs and on the tour screen** rather than left to
be discovered from an alert that never comes. `FROZEN` still works throughout — it asks about
the STREAM, not the scene.

**THREE CLAIMS ON ONE CAMERA, IN A FIXED ORDER: a person, then an alarm, then the patrol.**
Somebody at the ring is tracking something the appliance cannot see, so an alarm arriving
while they hold the camera does not move it — it is reported, and they decide. An alarm beats
the patrol because a patrol looks at nothing in particular. Getting this wrong is not
cosmetic: a patrol that steps during an alarm rotates the camera away three seconds after
pointing it there, and the recording shows the corridor next door.

**THE BENCH FOUND THREE, and needed a device to find any of them.** The harness films
mediamtx RTSP sources with no ONVIF service at all, so `tools/fleetbench/onvifsim.py` is a
small ONVIF PTZ device that keeps the state a real dome keeps and **records what it was asked
to do** — which is what lets the bench assert the appliance SENT `GotoPreset` for the right
stops, in order, at the right times, rather than that an API returned 200.

1. **A patrol that had been TOLD TO STOP moved the camera once more.** A tick reads the tour
   rows, asks the camera what presets it has — an ONVIF round trip — and only then commands
   the move; a stop landing inside that gap is written to a row the tick has already read.
   A camera that swings away one beat after the screen said the patrol stopped is worse than
   one that never stopped, because the screen and the dome disagree about who has it — and
   that is exactly when somebody is reaching for the ring. Closed with a generation counter
   re-checked immediately before the command, which also covers an operator taking the ring
   mid-tick. **ASK OF ANY LOOP THAT COMMANDS HARDWARE: what can change between the decision
   and the command?**
2. **A deleted camera left its patrols behind**, commanding a device that was no longer
   configured, every dwell, forever, under an id nothing can render. The same shape W3-2
   shipped with its appearance descriptors — and found the same way, by a bench finally using
   the verb. **A BENCH ONLY COVERS THE VERBS IT USES:** the first run passed 44/44 without
   ever deleting a preset, a tour, or a camera, or stopping a patrol.
3. **THE ARABIC SCREEN PASS: the button that opens the panel rendered perfectly and could not
   be pressed.** It sat at `inset-inline-start: -34px` inside `.ptz-ring-overlay`, which is
   anchored with a PHYSICAL `right`. In RTL the logical inset flipped sides and pushed 25 of
   its 26 pixels off the tile onto the page background, where a click hits the workspace and
   nothing happens. **MIXING A PHYSICAL ANCHOR WITH A LOGICAL OFFSET is the bug**; the fix is
   flow layout, which has no offset to flip. The English pass could not have found it.
   The check that turned it from a mystery into a diagnosis is `document.elementFromPoint` at
   the control's own centre — **when a click does nothing, ask what is actually there.**

**Also from reading the path:** an alert raised through `POST /api/vision/alerts` — which the
rule editor's own Test button uses, and which already goes out of its way to give a
hand-raised alert parity with the monitor — would have notified and not moved the camera.
"What happens when this rule fires" would have had two answers depending on which code raised
it. One `ApplyRulePTZRecall`, called from both. Diagnostics are deliberately not a caller: a
dome that swings to the gate because the detector failed to capture a frame is driven by the
health of the software rather than by what it saw.

**Not claimed:** the tamper interlock is unit-tested and mutation-checked, not live-benched —
the simulator serves ONVIF and no video, so there is no siphon frame for the monitor to read.
No ONVIF preset TOURS (`tptz:PresetTour`): device support is thin and inconsistent, and a
tour driven from the appliance works on any camera that has presets at all.

**W3-5b · ONVIF events, digital inputs & relay outputs** (F-14) — SHIPPED and benched on
`feat/onvif-events`. `infra/onvif/events.go` + `relay.go`; `services/camera_events.go` +
`relay.go`.

**TWO DIRECTIONS THE PRODUCT HAD NEVER GONE.** Everything it detected, it detected itself —
pull a frame, run a model. A camera has its own senses, and the ones no video analysis can
substitute for are the DIGITAL INPUTS on its terminal block: a door contact, a PIR, a beam, a
panic button. And a relay output is the only thing this appliance does that acts on the
world. The capability pill for events had been rendered on the camera page since long before
this, and nothing consumed it.

**THE EVENT TRANSPORT'S FAILURE MODE IS SILENCE, and that is the whole design.** PullPoint is
a subscription with a LEASE; a camera drops it without a word if it is not renewed, and a
door contact that has stopped reporting looks exactly like a door nobody opened. So a
subscription that cannot be kept alive is an ALERTABLE FAULT, not a quiet retry — the same
principle as W1-3 and W1-5. Renewal runs at two thirds of the lease **the device reported**,
measured on the DEVICE's clock, because a camera whose clock is wrong is common enough that
this app has a date/time screen.

**INITIALIZED IS NOT AN EVENT.** On subscribing, a camera announces the CURRENT state of
every property it publishes — so a building with four closed door contacts announces four
closed door contacts the instant we connect. Treated as alerts, every restart, every renewal
failure and every network blip raises a burst of alarms for doors nobody touched, at exactly
the moments an operator is least able to tell a real one from noise. **ASK OF ANY SUBSCRIBE:
what does it tell you before anything has happened?**

**THE RELAY IS A CHOKEPOINT, AND ONE RULE OUTRANKS THE REST: TURNING SOMETHING OFF IS NEVER
REFUSED.** The rate limit, the mode lookup and every other check apply only to switching ON;
the OFF branch sits above all of them, before anything that can fail. A limiter that can
block an OFF is a siren nobody can silence, and it would refuse exactly when the siren is
sounding — which is when the limiter is most likely to have been tripped. The screen honours
the same rule: the off control is never disabled and never dimmed, not even during the
request that started the siren.

**THE RESPONSIBILITY FOR SWITCHING OFF GOES TO THE DEVICE WHENEVER THE DEVICE WILL TAKE IT.**
A monostable output releases itself; a bistable one stays put forever. So a pulse first asks
the camera to BECOME a timed output, and only if the camera refuses does this appliance hold
it with a timer in memory — and then says so on screen, because that is the one state where a
crash leaves something in the building energised. An unknown mode counts as bistable: the
reading where WE are responsible for the off. **PUSH THE OBLIGATION ONTO THE THING THAT
SURVIVES YOUR PROCESS.**

**WHAT THE BENCH FOUND:**

1. **An alert-log write that silently never happened.** The listener also wrote a row into
   the AI alert log "so a door contact is searchable next to the detections". `alert_event`
   requires a RULE ID and a digital input has none, so `ValidateAlertEvent` refused every
   write: the notification arrived, the log stayed empty, and the only symptom was a line in
   a log nobody reads. **HALF OF WHAT THE FUNCTION CLAIMED TO DO DID NOT HAPPEN.** The feed
   is the right home anyway — it is where tamper and both health monitors already publish,
   and it is filterable by camera. **A VALIDATION GUARD YOU DID NOT READ IS A FEATURE YOU DID
   NOT SHIP.**
2. **THE SCREEN PASS: the outputs button could not be clicked at all.** Absolutely positioned
   at the tile's top corner, it landed underneath the maximize and remove buttons that
   already live there — perfectly rendered, invisible to a click, `elementFromPoint` returning
   their `svg`. Moved into the tile header with the other tile controls, where it also has no
   logical inset to flip in Arabic. **Second sighting in two items of a control that renders
   and cannot be pressed; the corners of a tile are occupied.**
3. **A deadlock in the simulator itself** (`note()` taking a non-reentrant lock the caller
   already held) that presented as the whole device going silent mid-bench — which reads
   exactly like a product failure. Recorded in the checklist so the next person does not hunt
   it in the product.

**Also decided:** a rule can only ever PULSE an output, never latch one, because the rule
that would leave a siren sounding is by construction the one firing at 4am with nobody
watching. Every actuation is audited, including the automatic ones and the failures — through
a recorder function rather than the request-scoped Auditor, because a rule sounding a siren
has no HTTP request behind it and would otherwise have gone unaudited. And `device.alert` is
now a category mymatasan destinations can subscribe to: without registering it,
`normalizeCategories` DROPS it, and a destination subscribed to door contacts alone would
have silently become one subscribed to everything.

**The grant is its own path**, not part of `/api/cameras/*/ptz`: a control-room operator who
may point a camera is not automatically somebody who may open a gate. Operators read the
outputs (a screen that cannot list them cannot offer a button); switching them is a third
rung an administrator grants deliberately.

**Not claimed:** no input→output automation (a door contact cannot yet fire a siren by
itself) — that is myiotsan's flow engine, and the seam to reach for is its Issue chokepoint.
An input cannot be bookmarked into a case file, because case evidence points at alert events.
No real relay hardware; the simulator implements the calls the product makes and the faults a
device returns.

**W3-6 · Privacy zones** (F-19) — SHIPPED and benched on `feat/privacy-masking`.
`infra/onvif/mask.go` + `services/privacy.go`, and redaction in `services/evidence_export.go`.

**THE EU PROMOTION RULE IS SPENT: asked and answered 2026-08-24 — NOT shipping to the EU.**
So this was built as an ordinary feature rather than a compliance control. Do not raise it
again unless the market changes.

**ONE DRAWN REGION, TWO MECHANISMS, AND THE WHOLE DESIGN IS NOT CONFUSING THEM.** The CAMERA
burning it in means the pixels never leave the sensor — not on disk, not in an export,
nothing on a stolen drive. The EXPORT redacting it means the pixels are recorded and do not
leave the building. **A mask that is not burned in by the camera is a courtesy, not a privacy
control**, and which one an operator has is a fact about their hardware. So the product
reports it per camera, in those words, rather than implying the stronger claim.

**MASKING IN OUR RECORDING PIPELINE WAS REJECTED AND THE REASON IS WRITTEN DOWN.** It would
give the strong guarantee on every camera and cost the product its architecture: recording is
`-c copy`, so masking mid-stream means decoding, filtering and re-encoding every camera
continuously — an order of magnitude on the capacity story for something most cameras do for
free. **Masking only in the VIEWER is the trap**, not an option: it looks identical to the
operator and protects nothing.

**READ IT BACK — a camera can accept a mask with HTTP 200 and store something else.** A
different coordinate space, a bounding rectangle, or nothing at all. `encoder.go` already
carried this scar for H.265 ("many cameras accept a Media1 set with HTTP 200 but silently
keep H.264"). Every write is read back and compared; anything that does not round-trip is
reported as UNCONFIRMED and treated as not masked. **A privacy mask believed to be applied
and not applied is worse than none, because somebody relies on it.**

**REDACTION AND THE EXPORT'S INTEGRITY PROMISE ARE IN DIRECT TENSION, and the answer is to
say so.** `evidence_export.go` states in its own comments that an export must not re-encode,
"because re-encoding changes every pixel and hands the other side an obvious argument that
the footage was processed" — and redacting IS re-encoding. So a redacted bundle DECLARES
ITSELF A DERIVATIVE: in its filename, in the manifest, and first in VERIFY.txt, while still
carrying the SOURCE digests so the derivation is traceable to footage that stays on the
recorder. Solid black rather than blur: a blur invites the argument that something could be
recovered, and on a low-detail region it sometimes can be.

**WHAT THE BENCH FOUND:**

1. **A redact flag the API silently dropped.** The handler builds the service request field
   by field rather than passing the decoded body, so `redact` existed on the screen, existed
   in the service, and never crossed the middle. The export came out unredacted with a
   manifest that correctly said so. **The bench caught it because it asserted the MANIFEST,
   not that the export succeeded** — the check that would have passed is the obvious one.
2. **THE ARABIC SCREEN PASS: the status sentence was hard-coded English.** The single most
   important line on the page — the one that distinguishes "never recorded" from "only the
   exports are protected" — printed in English in an Arabic UI. **Identical in shape to the
   defect W3-4 shipped with its rule-schedule summaries.** The server now returns a state
   plus the zone names and the SCREEN composes the sentence; the screen check asserts, in
   any non-English run, that the banner is not the API's English text.
3. **A camera with no ONVIF at all was reported as "could not be reached"**, which sends
   somebody to check a network for a fact about the camera. Now "this camera cannot mask
   anything itself".
4. **My own first pixel check passed on UNREDACTED footage.** It guessed "is it black" from
   a cropped PNG's file size, and the part of the test pattern it cropped is a flat colour
   band that compresses to almost nothing — so it was the one green tick on a run whose
   manifest correctly said nothing had been redacted. Now MEASURED with `signalstats`, two
   regions: inside the zone must be black AND outside it must not. **A single measurement
   passes just as happily on a video that is black everywhere.**

**Bench-scene lesson worth keeping: BLACK IS 16, NOT 0.** H.264 here is limited ("TV") range,
where luma runs 16–235. A "near zero" threshold fails on a perfectly black rectangle and
reads as a broken redaction.

**Not claimed:** no real camera: `onvifsim.py` implements the calls the product makes and can
be told to lie, but it is not any vendor's firmware. (Face redaction on export was W3-6b and
has since shipped — see below.)

**W3-7 · N+1 node failover** (F-23) — **SHIPPED and BENCHED, `feat/node-failover`.**
55/55 on a real two-node fleet with real recorded footage, plus 27/27 en and 30/30 ar screen
passes. **The benches found four real defects; see the bottom of this section.**

Driven from the control plane, which is the only party that talks to both appliances, knows
which are alive, and is still running when one of them is not. `apis/deployment.go` is right
that mymatasan cannot cluster — it is pinned to its own disks and its own capture hardware.
So this is not clustering; it is a **rehearsed handover** arranged in advance.

`infra/handoff` (new, stdlib-only) · `apps/mymatasan/{entities,services,apis}/standby*` ·
`apps/myseliasan/{entities,services,apis}/failover*` · the Failover screen in the SPA.

**THE FACT EVERYTHING FOLLOWS FROM: when failover matters, the failed appliance is
unreachable.** Nothing can be fetched from it — not its camera list, not its credentials, not
its recording settings. So the copy is taken EARLY and repeatedly, while it is healthy, and
the spare is asked to PROVE it can open those cameras before anything has gone wrong. **That
proof is the product.** A spare nobody has drilled is a line in a contract.

**READY IS NEVER INFERRED FROM A SUCCESSFUL COPY.** A staged set proves the two appliances
can talk to each other; it says nothing about whether the spare can reach the CAMERAS — a
different network path, different credentials, and the thing that actually fails. A plan that
has never been drilled reports `untested`, in the same place a proved one reports `ready`.
Same rule as the policy reconciler's `unknown`: absence of evidence gets its own colour.

**THE CONTROL PLANE CARRIES AN ENVELOPE IT CANNOT OPEN.** Moving a camera set means moving
camera credentials, and the obvious "export my cameras" endpoint would be a bulk credential
dump readable by anything that can call it. Instead the SPARE mints a one-exchange X25519
key, the PROTECTED appliance seals its set to that key with the spare's node id as associated
data, and myseliasan relays a blob it has no key for. A bundle captured in flight cannot be
staged onto a different node. **The exchange also asserts that the appliance which ANSWERED
is the spare the plan names** — without that check a mis-delivered tunnel would seal a site's
credentials to a machine nobody chose, silently, because every later step still succeeds.

**FAILOVER IS ADDITIVE, AND NOTHING IS EVER FENCED.** A control plane that cannot reach a
node cannot tell "dead" from "partitioned and recording perfectly". Stopping the returning
appliance means being willing to stop the only thing recording, on evidence that is
definitionally incomplete. So at worst both record the same camera until an operator fails
back — a duplicate stream and duplicate footage. **Nothing recording is the one unrecoverable
outcome, and no path here can produce it.** Fail-back is therefore manual too: a recorder that
returns for thirty seconds and dies again would otherwise thrash the whole building.

**A STAGED CAMERA IS NOT A CAMERA.** It does not appear in the camera list, is not probed, is
not recorded and is not on the wall; the camera row is created at ACTIVATION and only then. A
spare covering four recorders would otherwise show four sites' worth of cameras it is not
watching, in a control room, permanently. **And fail-back does NOT delete it**: the footage
recorded during the outage hangs off that row, and removing the camera would purge it.

**ACTIVATION READS THE RECORDER BACK.** Writing a recording config row starts nothing — the
settings screen has always had to hot-reload the recorder after a save — so a takeover that
only wrote rows would report success and record nothing until the next restart. Each camera's
outcome comes from `Manager.Statuses()`: `recording` only when ffmpeg is actually running,
otherwise the real reason.

**Not claimed:** it restores RECORDING, not the recordings — footage on the failed appliance
is still only there, and the only copies elsewhere are what the W2-3 clip archive already
pulled off it. **Capacity: CLOSED by W3-7b** (`feat/failover-capacity`, benched 2026-08-25, 11/11 against a
real appliance's own estimate) — the control plane now asks the spare what it can carry, counts
the spare's own cameras and every other plan committed to it, refuses to call an over-committed
plan READY, and still never blocks a takeover, because nothing recording is the one outcome
that cannot be undone. The original gap read: a spare is not stopped from taking on more
cameras than it can encode, and the drill does not measure load (the honest half — whether it
can reach and log into each camera — is measured).

**WHAT THE BENCHES FOUND — four, and every one of them was a screen or an API telling an
operator something other than the truth.**

1. **A PER-CAMERA TAKEOVER RESULT THE CONTROL PLANE SILENTLY DROPPED.** The appliance computes
   what happened to each camera while taking over and does NOT store it — it is a result, not
   a state — so the control plane rebuilding its view from the database afterwards returned an
   empty list. An operator who had just pressed the button in an emergency was told "active"
   and nothing about which of their forty cameras was actually being recorded. Every status
   code was 200 and the audit trail even had the outcomes in it. **The same shape as the
   redact flag W3-6 dropped: a field that existed at both ends and never crossed the middle.**
2. **A TAKEOVER THAT CALLED A RETRYING FFMPEG PROCESS "RECORDING".** `FFmpegRunning` is a
   weaker claim than it looks: a recorder pointed at a host the spare cannot resolve has a
   live process too, because it is retrying. So the takeover reported "recording" for a camera
   the DRILL ROW IMMEDIATELY ABOVE IT said could not be reached. A card that says both things
   at once is worse than one that says nothing. Now the whole set is started and then settled
   once, and `recording` requires `LiveFiles > 0` — footage on disk, not a live process. The
   third answer, "started, nothing written yet", is the honest one for the seconds in which a
   slow camera and a dead one look identical.
3. **A SWEEP THAT DRILLED EVERY NEW PLAN ON ITS FIRST TICK.** `now - LastDrillAt` on a plan
   that has never been drilled is fifty-five years, so the badge an operator had just watched
   say "never tested" turned green by itself half a minute later — beside a sentence telling
   them to press Test to find out whether the spare can reach the cameras. **The product and
   its own screen disagreed, and the distinction between COPIED and PROVED — the whole feature
   — became invisible in ordinary use.** "Never" is not "long ago"; they are different
   questions and one subtraction cannot answer both.


4. **A CLEAN FAIL-BACK SUMMARISED AS AN ALARM.** After handing the cameras back, every camera
   correctly reported "stopped" — and the card summarised that as "0 of 1 cameras are
   recording on the spare", in the amber it uses for a partial takeover. A successful,
   deliberate operation rendered as a warning. **Found by LOOKING at the Arabic screenshot on
   a run where all twenty-nine assertions passed** — which is the entire argument for looking
   at the picture the screen check writes and nobody reads.

**And two the bench found in ITSELF, both worth more than the checks they broke:** the camera
list answers with `{data:{result:[…]}}` rather than `{result:{items:[…]}}`, so "nothing
appeared on the spare" passed for the wrong reason while "the camera exists on the spare"
failed on a takeover that had worked; and the hold-down refusal was tested after a plan
already existed, so it hit the already-protected check and passed for a reason that had
nothing to do with the hold-down. `result_list` is the harness helper for the first, and the
second is a reminder to run a refusal in the state where only the thing under test can refuse.

**What the bench actually exercises:** two adopted nodes, a real mediamtx camera, real
recording on the protected node; the sealed bundle inspected for the camera password;
staging asserted to create NO cameras; a drill that comes back PARTIAL against a camera that
cannot be reached and READY only after it is removed; `docker stop` on the recorder and the
real 120s hold-down; the takeover; **a segment downloaded off the spare and decoded with
ffprobe — 60.00s of video, not a filename**; the recorder brought back and confirmed NOT
fenced (its own segment count grows again); the fail-back; and the footage surviving both the
fail-back and the deletion of the plan.

**W3-8 · Tenant isolation** (F-24). **CLOSED 2026-08-21 — decided: NO.** Each install
serves a single organisation; integrator/MSP resale off one shared control plane is not a
target. This was the one Phase 3 item that had to be decided before the others, because
retrofitting a tenant dimension onto a schema already in the field is expensive — deciding
against it is what makes the rest of Phase 3 safe to build in any order.

The standing consequence, which outlives this plan: **do not add a tenant/org column to
any new table.** An unused dimension is not free — it has to be filtered on in every query
forever, and the one place it gets forgotten is a cross-tenant leak in a product that never
had tenants. An integrator running several customers runs several installs; myseliasan
already federates across sites, which covers the multi-site case that motivated F-24.

**W3-6b · Face redaction on export** (F-19) — **SHIPPED and BENCHED, `feat/face-redaction`.**

`apps/mymatasan/ai/face_redact_worker.py` + `services/face_redactor.go`, riding the redacting
export pipeline W3-6 built and the YuNet model face recognition already installs.

**A PRIVACY ZONE IS A GUARANTEE AND A FACE PASS IS NOT, AND NOTHING IN THE PRODUCT LETS THE TWO
BE READ AS THE SAME KIND OF STATEMENT.** A zone was named by a human, does not move, and is
covered. A face pass covers the faces a DETECTOR FOUND — and detectors miss faces in profile,
at distance, partly occluded or motion-blurred. So the manifest gets its own `faces` block,
never more names in the `regions` list, and it carries a Limitation sentence saying faces may
remain and that a count of detections is not a count of people. The screen says it beside the
control AND beside the finished file.

**SOLID BLACK, NOT BLUR** — the same decision for the reason W3-6 already recorded: a blur
invites the argument that something could be recovered, and on a small region it sometimes can.
The register names the item "face blur"; the mechanism is a fill, deliberately.

**THE MISS THAT CAN BE PREVENTED IS THE FLICKER.** A detector finds a face at frames 100 and
102 and misses 101 — one full-resolution face in the middle of a clip nobody will scrub
through, which is exactly what makes naive face blurring worthless, because it looks like it
worked. Every detection is HELD three frames either side and WIDENED 28% beyond the detector's
box (which is tight around the features and leaves the jaw, hairline and ears outside it).
**That arithmetic is in Go, not in the worker**: it is the difference between covered and
visible, so it belongs where it can be unit-tested and mutation-checked.

**THREE REFUSALS, ALL THE SAME SHAPE.** An appliance that cannot obscure faces refuses at
REQUEST time rather than returning a bundle that did not; a scan with any unreadable frame
refuses rather than producing a partial redaction that looks complete; and a render that wrote
fewer frames than were scanned refuses rather than handing over a file that simply ends early.

**WHAT THE BENCH FOUND — one, plus one found by looking.** The pipeline came up clean, which is
unusual here and is explained by the two shakedowns that preceded it: the worker was run end to
end on the host and then inside the bench image before a line of it was wired in. The second of
those runs is what exposed **OpenCV writing its own warnings to stderr**, where the worker's
report is also written — treating that stream as pure JSON turned a completely successful
export into "the worker did not report what it wrote".

**And then, by LOOKING at the screenshot on a run where all twenty assertions passed in both
languages: THE EVIDENCE EXPORT DIALOG HAD NEVER HAD A PANEL.** It is the only element in the
app that uses `.modal`, and no `.modal` rule existed — every other dialog uses `.modal-card`.
The inputs looked fine because inputs paint themselves; every label, hint and warning rendered
transparently over the darkened page behind. It shipped that way with the export itself in
W1-4. Face redaction added three paragraphs of text — including the one sentence this feature's
honesty depends on — and a mild wrinkle became an unreadable overlap. **Second item running
where opening the PNG caught what the assertions could not.**

**HOW A FLEET WITH NOTHING TO FILM BENCHED A DETECTOR.** The harness films test patterns, so
W3-4 stated plainly that its evaluators were never driven end to end. Here that would have
gutted the item. Instead the camera films a DRAWN face — and YuNet detects it at 0.7-0.9
confidence, checked before the bench was written rather than assumed. That yields the thing a
synthetic scene normally cannot: a real detector making a real detection at a position we know,
which is what lets the output be MEASURED (face region YAVG 16.0 — black — against 141.7 in the
unredacted control, and a background at 106.5 so the reading means something).

**Not claimed:** no tracking (each frame is detected independently; a face lost for longer than
the hold is uncovered for the gap); no speed guarantee, so a face export is capped at 20 minutes
of footage; and the feature needs opencv plus the YuNet model the face-recognition setup
downloads — without them it refuses rather than degrading.

**W3-9 · Mobile PWA + web push** (F-20b) — **SHIPPED and BENCHED, `feat/mobile-pwa`.**

The other half of what F-20 named. W2-7 gave the control plane an outbound leg at all; email
still does not WAKE anybody, and the fleet's worst moments happen when nobody is at a screen.
myseliasan is now an installable app whose service worker shows a notification pushed from the
appliance, end-to-end encrypted to the device (RFC 8291), signed with the install's own VAPID
identity (RFC 8292), delivered by whichever push service the operator's browser uses.

**The problem this feature had to solve before writing a line of it.** A Web Push message is
delivered by POSTing to a URL a BROWSER VENDOR owns. There is no standard way to wake a closed
phone without one. **This control plane is normally deployed on an intranet with no internet
egress**, where every one of those POSTs fails at the TCP connect and no amount of
configuration changes it. Shipping a feature that quietly does nothing on most installs was
never an option, and neither was refusing to build it: the sites that DO have egress are
exactly the ones running fifty buildings off one operator's phone.

So the feature never claims to work — **it measures whether it works, per device, by actually
delivering**, and it separates four answers that a single "failed" would have merged:
delivered, the subscription is gone, the service refused, or the service was never reached.
Registering a device performs a real delivery immediately (W3-7's drill applied to a different
promise), so somebody turning this on learns in the same second rather than during an incident.
The screen states the no-egress case as what it is — the deployment working as designed, not a
fault to chase — and lists the hosts a firewall would have to allow.

**What leaves the building is stated on the screen, not buried in the manual.** Enabling push
means the appliance makes outbound HTTPS requests to a browser vendor, and that vendor learns a
message reached a device and when. The contents are encrypted end to end and it cannot read
them. Whether the appliance talks to Google or Apple at all is an ADMINISTRATOR's decision, so
the whole surface sits behind an accessrbac grant; within that grant a device is personal, and
only its owner (or a superadmin cleaning up after somebody who has left) may touch it.

**Two defects, one of them older than this work.**

- **An install with no route out was told the push service had REFUSED it.** The transport
  classifier treated any context error as "not the network's fault", which swallowed the single
  most important real-world case: a connect that times out because there is nowhere to connect
  to. The unit test that drove the design used an already-cancelled context and stayed green
  throughout. Now `Canceled` (the caller gave up, nothing was learned) and `DeadlineExceeded`
  (our own allowance ran out with no answer) are separated, with a test for each.
- **The fleet raised an alarm when a site went dark and never sounded the all-clear.** The
  liveness sweep emits `node-recovered` on the lost → online edge. It never saw that edge on a
  real fleet: a node comes back by DIALLING IN, `AcceptControlConn` marks it online between
  sweeps, and by the time the sweep looks the previous status is already online — so the
  recovery notification was, in practice, unreachable code. The registry's own unit test drives
  the sweep directly, so it had been green since the feature shipped. Found only because the
  push bench took a node down for real, brought it back, and counted what reached a device.

**Not claimed.** No offline cache, deliberately: a control plane serving yesterday's node list
would show a green estate that went dark an hour ago, and "the page will not load" is the
honest failure. No delivery guarantee — a push service may drop, delay or coalesce, and the
feed remains the record. A browser may rotate its subscription on its own, and the service
worker cannot re-register it (the API needs the CSRF token, which lives in a cookie a worker
cannot read); rotation heals from both ends instead — the SPA re-posts on every load and the
dead endpoint is deleted when the service answers 410 — so between a rotation and the next time
somebody opens the app, that device is not reachable. iOS delivers Web Push only to a page the
user has added to the Home Screen. And the bench does not decrypt the payload: a second
implementation of RFC 8291 written in the same sitting would only prove that two readings of
the spec agree with each other, so the encryption is checked against the RFC's own published
test vector, byte for byte, and the bench checks the wiring.

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
