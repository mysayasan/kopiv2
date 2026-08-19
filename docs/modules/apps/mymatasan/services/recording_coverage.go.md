# Module: apps/mymatasan/services/recording_coverage.go

## Purpose

How much of a window actually has footage on disk. The read model behind both the coverage strip on the Recordings page and the continuity monitor (`services/recording_continuity.go.md`) — deliberately one implementation, so the screen an operator reads and the alert that wakes them can never disagree.

## Responsibilities

- `Coverage(ctx, cameraId, from, to, bucket)` on `IRecordingService` → `CoverageReport{Buckets[], OverallPercent}`, bucketed `hour` or `day`.
- `coveredSeconds(segs, from, to)` — the maths: clip each segment to the window, merge overlaps, sum.
- `mergeIntervals` — sorts and coalesces spans.
- Served by `GET /api/recording/coverage?cameraId&from&to&bucket` (see `apis/recording.go`), which caps a request at `coverageMaxBuckets` and says so rather than truncating silently — a shortened coverage report reads as "the footage is missing" when it only means "you asked for too much".

## The four cases that make the number honest

Each one is a way a naive implementation reports the wrong thing, and each has a test in `recording_coverage_test.go`:

- **Overlaps are merged, not summed.** Overlap is normal rather than exceptional: a recorder restart re-opens a segment overlapping the previous one, and an event clip can be cut from the same footage as a continuous segment. Summing would report *more* than 100% for an hour that has a hole in it — worse than reporting nothing, because it reads as proof the footage is there.
- **A segment straddling the window start is counted, clipped.** `GetSegments` filters on `StartedAt`, so the query widens backwards by `coverageLookbackSlack` (one hour, comfortably more than any configured segment length). Without it the segment covering the first minutes of every hour is missed and every hour looks slightly short — precisely at the boundary where recorders roll over.
- **Segments running past the window end are clipped**, so a long clip cannot credit an hour it does not cover.
- **An unfinished segment (`EndedAt == 0`) is treated as running to the window end.** Zero-length would under-report the current hour and raise a false gap; open-ended would credit a crashed recorder's abandoned row with footage that does not exist. Clipping credits only time that has actually elapsed.

## Notes

- `OverallPercent` is computed across the whole range, not as the mean of the buckets: averaging percentages would weight a part-elapsed final bucket the same as a full one.
- Touching spans (`end == next start`) merge. Without that every segment boundary would read as a one-second hole and no camera would ever reach 100%.
- `coverageQueryLimit` bounds one query at 20000 segments; at the shortest sensible segment length that still spans several days for one camera, and the API caps the requested range independently.
- The frontend strip (`views/components/recording.js`, `CoverageStrip`) renders days in four bands rather than a gradient — "is this day usable?" is the question, and a continuous shade makes 40% and 60% look alike. A day with nothing reads as an outline rather than a filled swatch, so a run of them is visible as a hole.
