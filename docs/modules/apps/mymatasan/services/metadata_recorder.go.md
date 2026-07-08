# Module: apps/mymatasan/services/metadata_recorder.go

## Purpose

Implements `MetadataRecorder`, the write side of the object metadata recorder ("what objects each camera saw"): it implements `vision.ObservationSink` so the detector forwards every object candidate it already computed (no second video decode), and coalesces consecutive sightings of a label into presence-interval rows.

## Responsibilities

- `NewMetadataRecorder(repo, configs RecordingConfigLister, minConfidence)` builds the recorder; `configs` (satisfied by `IRecordingService`) supplies the per-camera `MetadataEnabled`/`MetadataGapSeconds` config.
- `Start(ctx)` launches two goroutines: a config-refresh/stale-interval-close ticker (`metadataCloseTickSeconds`, default 2s) and an async DB writer (buffered channel, `metadataWriteBuffer`). On `ctx` cancellation, open intervals are flushed synchronously so no presence is lost on shutdown.
- `Observe(cameraID, capturedAt, candidates)` — the `vision.ObservationSink` implementation. Gated by the per-camera enable toggle; filters candidates below `minConfidence`; dedupes a repeated `capturedAt` for the same camera (since both the rule `Detect` path and the metadata-only `ObserveOnly` path can call it for the same frame); folds each label's per-frame count/best-confidence/best-box into its open `openObservation`.
- `Observed(cameraID, capturedAt) bool` — reports whether this exact frame was already folded, so `VisionMonitor` knows whether a metadata-only `ObserveOnly` pass is still needed.
- `EnabledCameras() map[int64]bool` / `IsEnabled(cameraID) bool` — the per-camera enable state, read by `VisionMonitor` to sample metadata-enabled cameras even without alert rules.
- `closeStale()` — on each tick, writes and removes any open interval whose label has been absent for at least the camera's gap window (`MetadataGapSeconds`, default `defaultMetadataGapSeconds` = 5s).
- `flushAll()` — shutdown path: closes every open interval regardless of gap, writing synchronously.
- `write(e)` — persists one `entities.ObjectObservation` row via the generic repo; a write failure only logs (`log.Printf`), it never blocks the recorder.

## Notes

- `RecordingConfigLister` is a narrow interface (`ListConfigs`) so this package doesn't need the full `IRecordingService` surface; `IRecordingService` satisfies it.
- The per-camera config cache (`cfg map[int64]metaCamCfg`) is refreshed on the same 2s tick and swapped under its own `RWMutex`, decoupled from the per-frame observation state lock.
- A brief occlusion or a dropped detection frame does not split one presence into many rows — that's what the gap window absorbs; a shorter gap yields more, tighter intervals.
- `MaxConfidence`/`MaxCount`/`PeakBox` are the running maxima across the whole interval, not just the last frame, so a short peak (e.g. someone stepping fully into frame briefly) is not lost.
- Wired in `app.go`: the detector is set as the recorder's data source via `vision.ObservationCapable.SetObservationSink`, and the recorder itself is handed to `VisionMonitorSettings.Metadata`.
