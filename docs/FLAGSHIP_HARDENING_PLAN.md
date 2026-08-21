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
| W2-1 | Fleet configuration policy + drift detection | F-06 | `feat/myseliasan-fleet-policy` | ✅ shipped |
| W2-2 | Node state history + SLA reporting | F-08 | `feat/myseliasan-node-sla` | ✅ shipped, benched 2026-08-21 (33/33 on a real two-node fleet) |
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
| W3-8 | Tenant isolation | F-24 | — | ✅ CLOSED 2026-08-21 — single-org per install, will not build |
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

Only the SCREEN is unexercised — the API beneath it is. Worth a look in Arabic when
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
