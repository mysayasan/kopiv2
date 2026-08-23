# Module: apps/mymatasan/services/recording_timeline.go

## Purpose

The read model behind continuous, wall-clock playback: the Timeline screen's scrub bar
(`Timeline`) and its seek (`SeekAt`). Both are built on the same merged footage spans as
`services/recording_coverage.go.md`'s `coveredSpans`, so the shading, the coverage percentages,
and the player agree about the same hour — a bar showing footage the player cannot reach reads to
an operator as lost evidence.

## Responsibilities

- `Timeline(ctx, cameraIds, from, to)` on `IRecordingService` → `TimelineReport{From, To,
  Cameras[]}`. Per requested camera, calls `cameraTimeline`.
- `cameraTimeline(ctx, cameraId, from, to)` — reads segments with the same backwards lookback slack
  `Coverage` uses (`coverageLookbackSlack`, one hour) so a segment straddling the window start isn't
  missed, drops rows the lookback pulled in that never actually reach the window, orders the
  playable segment list oldest-first (the bar reads left to right; `GetSegments` returns
  newest-first for the grid), and derives `Spans`/`CoveredSeconds`/`Percent` from `coveredSpans`
  (`recording_coverage.go`) — the same merge `Coverage` sums.
- `ErrTimelineTooManySegments` / `timelineMaxSegments` (12000) — `Timeline` refuses a camera whose
  segment count in the window exceeds this rather than silently returning the newest N: `GetSegments`
  orders newest-first, so a truncated read would drop the *oldest* segments and render the left half
  of the bar empty — which reads as "no footage" rather than "the request needs narrowing". The error
  names the fix (a shorter window).
- `SeekAt(ctx, cameraIds, at)` → `[]TimelineSeek`. Per camera, calls `seekCamera`.
- `seekCamera(ctx, cameraId, at)` — pages back only as far as the nearest preceding *continuous*
  segment via the package-level `coveringSegmentCandidates` (`services/observation.go.md`, shared
  with object search) and picks a covering segment with `pickCovering` (continuous footage preferred
  over an overlapping event clip — the same preference object search applies). When nothing covers
  `at`, falls through to `nextSegmentFrom`, which returns the earliest segment starting at/after
  `at` — reachable only because nothing covers `at`, so one row (`ORDER BY StartedAt ASC LIMIT 1`)
  is enough. Sets `Found=false` (not the nearest footage in either direction) when the camera has no
  footage at or after `at` at all — an honest "not recording, and never came back" rather than a
  silent mis-seek.
- `SeekAt` is a server call rather than arithmetic the browser does over an index it already holds,
  for two reasons: it is the one place the "which file covers this moment" rule lives (so there is
  no second copy in JavaScript to drift out of step with `pickCovering`), and on an appliance under
  disk pressure the oldest footage is being evicted continuously, so a cached browser index can name
  a segment that no longer exists by the time the operator scrubs to it.

## Key Types

- `TimelineSegment` — one playable file (`Id`, `StartedAt`, `EndedAt`, `Codec`, `AlertId`).
  `StartedAt`/`EndedAt` are wall clock, and media time inside the file runs 1:1 from `StartedAt`
  because `infra/recording.segmentEndedAt` stores the file's *probed* duration rather than the
  moment the recorder closed it — so `at - StartedAt` is a real offset, not an estimate that drifts
  when a stream stalls.
- `TimelineSpan` — one merged `[From, To)` run of footage after overlaps are merged and the result
  is clipped to the window.
- `CameraTimeline` — one camera's `Segments[]`, `Spans[]`, and `CoveredSeconds`/`Percent` (the same
  numbers `/api/recording/coverage` reports for the same window).
- `TimelineReport` — the whole scrub bar for one window: `From`, `To`, `Cameras[]`.
- `TimelineSeek` — one camera's seek result: `At` (requested) vs `ResolvedAt` (served, differs only
  when snapped forward over a gap), `Found`, `SegmentId`/`OffsetSeconds`/`SegmentEndsAt`/`Codec`,
  and `Snapped`/`GapSeconds` — the screen states "you asked for 02:14 and are watching 02:47" rather
  than silently repositioning, since that sentence is the difference between a camera that missed
  half an hour and a player that mis-seeked.

## Notes

- Served by `GET /api/recording/timeline` and `GET /api/recording/timeline/seek` — see
  `apis/recording.go.md` for request/response shape, the 8-camera and 31-day request caps, and why
  detection marks are deliberately not part of this response.
- `coveringSegmentCandidates` (moved to package level in `services/observation.go.md`) and
  `segmentPager` are what let `seekCamera` reuse object search's covering-segment sweep without
  `recordingService` depending on its own `IRecordingService` (an import cycle).
