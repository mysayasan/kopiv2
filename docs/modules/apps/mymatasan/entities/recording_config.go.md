# Module: apps/mymatasan/entities/recording_config.go

## Purpose

Declares the `RecordingConfig` entity that stores per-camera recording settings in SQLite.

## Fields

| Field               | Type   | Notes |
|---------------------|--------|-------|
| `Id`                | int64  | Auto-increment primary key. |
| `CameraId`          | int64  | Unique per-camera key; one config row per camera. |
| `Enabled`           | bool   | Whether recording is active for this camera. |
| `PreRollSec`        | int    | Seconds of footage to include before the alert trigger time. |
| `PostRollSec`       | int    | Seconds of footage to capture after the alert trigger time. |
| `StoragePath`       | string | Base directory on the server where clip files are written. |
| `RetentionDays`     | int    | Clips older than this are deleted by the purge operation. Zero disables retention enforcement. |
| `SegmentMinutes`    | int    | Duration of each rolling `.ts` segment in minutes (RTSP mode). |
| `LiveStreamUrl`     | string | RTSP URI used for the browser live-view stream. When set, the UI shows this as the selected live stream and `applyLiveStream` pushes it to the camera entity. |
| `StreamURL`         | string | Optional RTSP URI override for the recording stream. When set, this takes precedence over the ONVIF-discovered URI. Useful for pointing the recorder at a sub-stream while live view uses the main stream. |
| `FallbackStreamUrl` | string | Optional fallback RTSP URI tried after 2 consecutive quick connection failures of the primary stream. Supports cameras that allow only one concurrent RTSP connection. |
| `MetadataEnabled`   | bool   | Turns on the object metadata recorder for this camera — a searchable text log of what objects the camera saw, independent of NVR recording (see `services.MetadataRecorder`). |
| `MetadataGapSeconds`| int    | How long a label may go unseen before its open presence interval is closed and written; `0` uses the recorder's default (~5s). |
| `AppearanceEnabled` | bool   | Adds an appearance descriptor to each person/vehicle sighting this camera records, so those sightings can later be ranked by "find more like this" (W3-2, see `services.AppearanceService`). **Requires** `MetadataEnabled` — the descriptor hangs off the observation row metadata recording creates. Unlike `MetadataEnabled` it is a real per-camera choice rather than a consequence of recording: it costs a neural-network forward pass per person/vehicle in every sampled frame. Off by default; added additively (auto-migrated column), so every existing camera keeps its current behaviour. |
| `CreatedAt`         | int64  | Unix seconds; row insertion time. |
| `UpdatedAt`         | int64  | Unix seconds; last update time. |

## Notes

- The `ukey:"camera"` tag generates a unique index on `camera_id`, enforcing one config per camera.
- The bootstrap schema creates and auto-migrates the `recording_config` table on startup; adding `LiveStreamUrl` adds the column automatically without a manual migration. `MetadataEnabled`/`MetadataGapSeconds` are similarly added via `ALTER TABLE`; a dedicated `app.go` seeder backfills both to their disabled defaults on existing rows (a bare `ALTER TABLE` leaves them `NULL`, which the bool/int scanner can't read).
- Config rows are loaded at app startup and applied to the `recording.Manager` via `Configure`. Runtime changes via `PUT /api/recording/config` take effect immediately through the hot-reload path.
- `LiveStreamUrl` is the mechanism for split-stream setups where live view uses a different stream than recording. It is persisted in this table so the Recording UI preserves the selection across page reloads.
- `StreamURL` and `FallbackStreamUrl` control the recording stream and fallback; they are independent of `LiveStreamUrl`.
- `MetadataEnabled`/`MetadataGapSeconds` are read by `services.MetadataRecorder.refreshConfig` (via `RecordingConfigLister`) and by `VisionMonitor` (via `MetadataRecorder.EnabledCameras`/`IsEnabled`) to decide which cameras to sample for metadata even when they have no AI alert rules.
- `AppearanceEnabled` is also read by `MetadataRecorder.refreshConfig`, which ANDs it with `MetadataEnabled` when building its own per-camera cache (`metaCamCfg.appearance`) rather than trusting the column alone, so a row saved appearance-on/metadata-off can never make the sampler embed crops with nowhere to write them — see `services/metadata_recorder.go.md`. `services.recordingService.SaveConfig` enforces the same pairing on write (`req.AppearanceEnabled && req.Enabled`) — see `services/recording.go.md`.
