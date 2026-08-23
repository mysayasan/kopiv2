# Module: apps/mymatasan/services/metadata_recorder.go

## Purpose

Implements `MetadataRecorder`, the write side of the object metadata recorder ("what objects each camera saw"): it implements `vision.ObservationSink` so the detector forwards every object candidate it already computed (no second video decode), and coalesces consecutive sightings of a label into presence-interval rows.

## Responsibilities

- `NewMetadataRecorder(repo, configs RecordingConfigLister, minConfidence)` builds the recorder; `configs` (satisfied by `IRecordingService`) supplies the per-camera `MetadataEnabled`/`MetadataGapSeconds` config.
- `Start(ctx)` launches two goroutines, each under `infra/safego.Supervise` (names `mymatasan.metadata.recorder`/`mymatasan.metadata.writer`) rather than a bare `go`: a config-refresh/stale-interval-close ticker (`metadataCloseTickSeconds`, default 2s) and an async DB writer (buffered channel, `metadataWriteBuffer`). The writer must restart on panic — if it dies the observation queue backs up and every sighting is lost with no signal. On `ctx` cancellation, open intervals are flushed synchronously so no presence is lost on shutdown.
- `Observe(cameraID, capturedAt, candidates)` — the `vision.ObservationSink` implementation. Gated by the per-camera enable toggle; filters candidates below `minConfidence`; dedupes a repeated `capturedAt` for the same camera (since both the rule `Detect` path and the metadata-only `ObserveOnly` path can call it for the same frame); folds each label's per-frame count/best-confidence/best-box into its open `openObservation`.
- `Observed(cameraID, capturedAt) bool` — reports whether this exact frame was already folded, so `VisionMonitor` knows whether a metadata-only `ObserveOnly` pass is still needed.
- `EnabledCameras() map[int64]bool` / `IsEnabled(cameraID) bool` — the per-camera enable state, read by `VisionMonitor` to sample metadata-enabled cameras even without alert rules.
- `closeStale()` — on each tick, writes and removes any open interval whose label has been absent for at least the camera's gap window (`MetadataGapSeconds`, default `defaultMetadataGapSeconds` = 5s).
- `flushAll()` — shutdown path: closes every open interval regardless of gap, writing synchronously.
- `write(p pendingObservation)` — persists `p.entity` (an `entities.ObjectObservation` row) via the generic repo; a write failure only logs (`log.Printf`), it never blocks the recorder. When the write succeeds and `p.appearance` is non-empty, it then calls `r.appearance.Store` (see *Appearance descriptors* below) with the new row's id — logged on failure, not propagated: losing the ability to rank one sighting by appearance is a much smaller harm than losing the record that the sighting happened, so the two outcomes must not share a fate.

## Appearance descriptors (W3-2)

`MetadataRecorder` optionally also carries each interval's peak-crop **appearance vector**
through to storage, so "find more like this" (`services/appearance_search.go.md`) has
something to rank later.

- `AppearanceStore` interface (`Store(ctx, AppearanceRecord) error`) — the one method the
  recorder needs from `AppearanceService`, narrowed so the recorder can be tested without a
  cipher, a repo, or a search path. Wired post-construction via `SetAppearanceStore` (needs
  the at-rest cipher, built later in the app's wiring than the recorder is); `nil` disables the
  whole leg — the state on any install that hasn't turned appearance search on, and the
  recorder must (and does) behave exactly as before in that case.
- `openObservation.peakAppearance`/`peakAppearanceModel` travel alongside `peakBox`/`peakAt`,
  for the same reason: a descriptor taken from a half-occluded closing frame ranks badly
  against every future query, and nothing downstream could tell that was why. `Observe`'s
  per-frame aggregation keeps `bestAppearance` paired with the *same* candidate as `bestBox`
  (not "the first vector seen this frame") so two people in one frame never cross-contaminate
  each other's descriptor. The kept descriptor is only overwritten when the current frame
  actually produced one — the appearance stage skips crops too small or too uncertain, so a
  frame can raise the peak confidence and carry no vector, and clearing the one already held
  would lose the interval's only description to a later, marginally-better-scored but more
  distant frame.
- `pendingObservation{entity, appearance, model}` replaced the write channel's bare
  `entities.ObjectObservation` element: the descriptor is keyed by the observation's id, which
  doesn't exist until the row is inserted, so the row and its (not-yet-persisted) vector travel
  together through `writeCh` rather than being paired afterwards by `(camera, label, time)` — a
  join on values that are not unique.
- `IsAppearanceEnabled(cameraID) bool` — the per-camera compute gate `VisionMonitor` reads to
  set `vision.Frame.WantAppearance`, in the same shape as the LPR/face gates. Returns `false`
  both when the camera's config has appearance off and when `r.appearance` is unwired (a build
  or install without it never pays for vectors that have nowhere to go).
- `refreshConfig` ANDs `AppearanceEnabled` with `MetadataEnabled` when building `metaCamCfg`
  rather than trusting the column alone — appearance is meaningless without the observation row
  it attaches to, so a config saved as appearance-on/metadata-off must not make the worker embed
  crops on every frame only to throw every vector away. (`services.SaveRecordingConfig` already
  enforces this pairing on write — see `services/recording.go.md` — this is defense in depth.)

## Notes

- `RecordingConfigLister` is a narrow interface (`ListConfigs`) so this package doesn't need the full `IRecordingService` surface; `IRecordingService` satisfies it.
- The per-camera config cache (`cfg map[int64]metaCamCfg`) is refreshed on the same 2s tick and swapped under its own `RWMutex`, decoupled from the per-frame observation state lock.
- A brief occlusion or a dropped detection frame does not split one presence into many rows — that's what the gap window absorbs; a shorter gap yields more, tighter intervals.
- `MaxConfidence`/`MaxCount`/`PeakBox` are the running maxima across the whole interval, not just the last frame, so a short peak (e.g. someone stepping fully into frame briefly) is not lost. `PeakAt` (the `capturedAt` of the frame that set the current `peakBox`) is tracked alongside them and persisted on `entities.ObjectObservation`, so Object Search playback can seek to the exact clearest moment instead of the interval start.
- Wired in `app.go`: the detector is set as the recorder's data source via `vision.ObservationCapable.SetObservationSink`, and the recorder itself is handed to `VisionMonitorSettings.Metadata`. `appearanceService` is wired into it via `SetAppearanceStore` in the same block that builds `AppearanceService` — see `services/appearance_search.go.md`.
