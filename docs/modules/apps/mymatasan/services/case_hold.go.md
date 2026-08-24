# Module: apps/mymatasan/services/case_hold.go

## Purpose

The **footage hold**: while a case file is OPEN, the footage its evidence points at is not
deleted (W3-3).

This is the property that makes a case worth opening. Sites run seven, fourteen or thirty
days of retention; an investigation routinely outlives all three, and the moment it does,
every clip the case names quietly becomes a broken link. Nothing about the case changes, the
rows are all still there, and the operator discovers it when they open the export and find
nothing in it.

## The four paths footage leaves by

A hold honoured by three of them is not a hold:

| Path | What it is |
|------|------------|
| `services/recording.go` `PurgeOldSegments` | the retention sweep (DB-driven) |
| `services/recording.go` `PurgeCameraFootage` | the per-camera "Purge now" |
| `services/recording.go` `PurgeOldestSegments` | the disk-pressure sweeper |
| `infra/recording/rtsp.go` `purgeOldFiles` | the recorder's OWN hourly FILE sweep |

The last one is the trap: it walks the live directory and deletes by filename age with no
knowledge of the database at all, so a hold enforced only in the service layer is undone
within the hour, leaving segment rows pointing at files that no longer exist. It is wired
through `RecorderConfig.RetentionHold`, a predicate `RecorderConfigBuilder.SetFootageHold`
installs — so `infra/recording` never learns what a case is, only that some footage is spoken
for.

## What the hold deliberately does NOT block

**Secure wipe**, **factory reset** and **deleting a camera** destroy footage regardless. Those
are the documented "destroy everything" operations, they are superadmin-only and audited, and
a hold that could block them would turn a case into a way to make footage undeletable. Camera
delete is why `PurgeAllForCamera` (unconditional, used by the delete cascade) and
`PurgeCameraFootage` (hold-honouring, used by the operator's button) are two methods rather
than one with a flag: the difference is a policy decision and it should be visible at the call
site. Footage lost that way shows on the case as `FootageMissing`.

## It fails closed

If the hold cannot be read — a database error, or more open cases than one read considers
(`openCaseScanLimit`, 500) — `HeldSpans.unreadable` is set and EVERYTHING is reported held.
A transient error must not be indistinguishable from "no case wants this": one of those two
readings destroys evidence, and only on the day it matters.

## Paging past held rows

Each purge reads oldest-first and held rows stay at the FRONT of that window forever, so the
loops advance a `heldSoFar` offset. Without it the query returns the same batch every
iteration and every segment behind the held one outlives its retention — while the purge
reports success.

## Shapes

- `FootageHold` — one open case's claim on a span of one camera. `Covers` treats touching
  endpoints as no overlap (a segment ending exactly when the evidence starts contains none of
  it) and a zero-length probe as an instant, which is what the file sweeper can ask about.
- `HeldSpans` — one camera's holds, resolved once and asked many times (`Blocked`,
  `BlockedSegment`), so a purge over hundreds of segments makes one query.
- `FootageGuard` — what every deletion path holds. Built EMPTY at boot and completed with
  `SetCases` once the case service exists, because the cycle is real (the case service reads
  recordings; every purge asks the case service). A setter rather than an optional type
  assertion: a mis-wiring then shows as a guard that holds nothing, loudly, instead of an
  interface that silently stopped matching — that trick has already dropped a metric in this
  codebase once.
- `FootageGuard.Predicate` — the hold in the shape the file sweeper needs, memoized per camera
  for `predicateCacheTTL` (30s) so one sweep costs one query.
- `CaseHoldSummary` — what the case screen shows: clips and bytes held, how many are already
  **BeyondRetention** (they exist ONLY because the case is open, and go when it closes), and
  how many pieces of evidence have no footage left at all.
- `PurgeFootageResult` — `Deleted`, `Kept`, `Reason`. `Kept > 0` is the purge WORKING; the
  reason names the case, because a purge that silently leaves footage behind is one nobody
  trusts again.

## Related

- `apps/mymatasan/services/case_file.go.md`
- `apps/mymatasan/services/recording.go.md`
- `apps/mymatasan/services/recorder_config.go.md`
