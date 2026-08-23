# Module: apps/mymatasan/services/observation.go

## Purpose

Implements `ObservationService`, the read/maintenance side of the object metadata recorder: searches recorded presence intervals and resolves each to the footage segment covering it, plus retention-aligned purge.

## Responsibilities

- `NewObservationService(repo, recording IRecordingService)` builds the service; `recording` resolves footage links and drives purge retention.
- `SetAppearanceReaper(r AppearanceReaper)` — wires appearance-descriptor purging in, post-construction (the appearance service needs the at-rest cipher, built later in `app.go`'s wiring). See *Appearance purge* below.
- `GetObservations(ctx, limit, offset, cameraId, extraFilters, extraSorters)` — true server-side filtered/sorted/paged search restricted to cameras whose continuous NVR recording is actually on (via `camerasRecording`): metadata recording is independent of footage recording, so a detect-only camera (recording `enabled=false`) logs observations but keeps no segments, and those sightings would otherwise show up as permanently-stuck "Finalizing…" (`FootagePending`) rows that never resolve. When an explicit `cameraId` (`>0`) is given and it isn't recording, the search short-circuits to an empty result; otherwise the base filter becomes `CameraId IN <recording camera ids>` (or short-circuits empty if none are recording). This restriction is applied at the query level, not by dropping rows after the page is fetched, so paging and the total count stay correct. `extraFilters`/`extraSorters` come straight from the client `DataTable` (column filters + sort, e.g. a time daterange, object `Label` — including multi-select via the `In` compare operator, or `MaxCount`) so paging runs over the real filtered set. Defaults to `StartedAt DESC` when no sort is supplied. Each result is an `ObservationResult` — the raw `ObjectObservation` plus `SegmentId`/`SegmentCodec`/`SeekSeconds` for click-to-play, or `FootagePending: true` when the sighting falls inside the camera's current still-writing segment (no finalized file yet, but footage is coming). The seek target is the sighting's `PeakAt` (falling back to `StartedAt`) so playback lands on the object's clearest frame.
- `camerasRecording(ctx)` — reads all `RecordingConfig`s via `IRecordingService.ListConfigs` and returns the set of camera IDs with `Enabled=true`. Returns a non-nil (possibly empty) map when configs were read successfully, and `nil` only on a read error, so `GetObservations` can distinguish "nothing is recording" (empty map → show no results) from "unknown" (`nil` → don't restrict, fail open rather than hide real results).
- `resolveCoveringSegments(ctx, rows)` — batched replacement for the old per-row `coveringSegment` lookup: groups the page's rows by camera, fetches each camera's candidate segments once via `fetchCoveringCandidates` over the `[earliest..latest]` sighting span (instead of one query per row — the prior approach was an N+1 that scaled badly as the table grew), then matches each row in memory with `pickCovering`. Also returns, per camera, the newest end-time among its candidates (`newestEnd`) so the caller can tell a still-writing tail (`StartedAt >= newestEnd` → `FootagePending`) from an unrecorded gap (older than the newest footage, but nothing covers it → omitted from results).
- `fetchCoveringCandidates(ctx, cameraId, minAt, maxAt)` — thin `*ObservationService` wrapper that calls the package-level `coveringSegmentCandidates` against `s.recording`.
- `coveringSegmentCandidates(ctx, src segmentPager, cameraId, minAt, maxAt)` — the actual paging logic, lifted to package level so it has exactly one implementation shared with the recording timeline's seek (`services/recording_timeline.go.md`'s `SeekAt`): pages a camera's segments (100/page, capped at 50 pages) newest-first via `segmentPager.GetSegments` (satisfied by `IRecordingService.GetSegments`), stopping once it reaches a *continuous* (non-event-clip) segment starting at/before `minAt` — the nearest full-footage segment preceding the earliest sighting is enough to cover it — so the sweep stays bounded regardless of retention depth. Object search (resolving a sighting to its footage) and Timeline seek (resolving a scrubbed moment to its footage) must agree on which segment covers a moment, so there is one function, not two that could drift apart.
- `segmentPager` — the single-method interface (`GetSegments`) `coveringSegmentCandidates` depends on instead of `IRecordingService` directly, so `recordingService` itself (which cannot import its own service interface without a cycle) can call it from `seekCamera`.
- `pickCovering(candidates, at)` — from newest-first candidates, returns the segment spanning `at`, preferring a continuous segment (`AlertId == 0`, full footage/accurate seeking) over an event clip that also happens to span it.
- `Labels(ctx, cameraId)` — distinct object labels observed for a camera (or all cameras), scanning a bounded window of recent rows (2000) rather than a `DISTINCT` query, keeping it engine-agnostic. Backs the search UI's label filter list.
- `PurgeOldObservations(ctx)` — deletes presence intervals past retention: each camera's own `RecordingConfig.RetentionDays` wins when set, else `defaultObservationRetentionDays` (30). Batches deletes 500 rows at a time by ascending `EndedAt`. Returns the count deleted. Before each batch's rows are deleted, their ids are handed to `AppearanceReaper.DeleteForObservations` first (when wired) — see *Appearance purge* below.
- `PurgeAllForCamera(ctx, cameraId)` — deletes EVERY observation belonging to one camera regardless of age, batched 500 rows at a time. Part of the camera-delete cascade (`app.go`) and the recording API's "Purge now" action (`apis/recording.go.md`'s `purgeCameraNow`): once the camera's recording config is gone, retention (which is driven off that config) would never purge these rows again. Deletes every appearance descriptor for the camera FIRST, in one statement, via `AppearanceReaper.DeleteForCamera` (when wired).
- `ResolveFootageFor(ctx, points []FootagePoint) []FootageRef` — batches a set of `(cameraId, at)` moments to their covering segments, for appearance search's ranked shortlist: a hit an operator cannot open is a hit they cannot act on. Groups points by camera and runs ONE `coveringSegmentCandidates` fetch per camera (not one per hit — a shortlist can be up to 200 rows), then `pickCovering` per point, so it goes through the exact same resolver (including the continuous-over-event-clip preference) `GetObservations` uses — a sighting found by appearance ranking opens the same file the Objects grid would open for it. Returns a `FootageRef{SegmentId, Seek}` per point in the same order, zero-valued when nothing covers that moment.

## Appearance purge (W3-2)

`AppearanceReaper` is the slice of appearance storage the observation retention paths need:

```go
type AppearanceReaper interface {
    DeleteForObservations(ctx context.Context, observationIds []int64) (int, error)
    DeleteForCamera(ctx context.Context, cameraId int64) (int, error)
}
```

Satisfied by `*services.AppearanceService` (`services/appearance_search.go.md`). A `nil`
reaper (the state whenever `SetAppearanceReaper` was never called — any install without
appearance search) disables the leg entirely; both purge paths behave exactly as before. Where
it IS wired, descriptors are deleted **before** the observation rows that own them, in both
`PurgeOldObservations` and `PurgeAllForCamera` — once an observation row is gone, nothing
points at its descriptor and no later sweep can find it, so retention would otherwise quietly
stop applying to the appearance index while still applying to everything else. A descriptor
that outlives the sighting it describes is a searchable record of somebody the retention
policy says has been forgotten, so this ordering is load-bearing, not cosmetic.

## Notes

- `ObservationResult` embeds `*entities.ObjectObservation` by pointer, so JSON output flattens the interval fields alongside the added footage-link fields.
- Footage linkage is by time overlap, not a stored foreign key — an observation always resolves against the *current* segment table, so it stays correct even if segments are re-indexed.
- `FootagePending` distinguishes "recorded, not yet playable" (still-open segment; will resolve on its own once the segment closes and is saved) from "not recorded at all" (older gap; permanently omitted rather than shown as a dead link).
- Wired in `app.go` alongside `MetadataRecorder` (write side) and exposed at `GET /api/observations` (+ `/labels`) via `apis/observation.go`; `PurgeOldObservations` runs on the same 6-hourly loop as `IRecordingService.PurgeOldSegments`. `appearanceService` is wired into it via `SetAppearanceReaper` in the same block that builds `AppearanceService` — see `services/appearance_search.go.md`.
