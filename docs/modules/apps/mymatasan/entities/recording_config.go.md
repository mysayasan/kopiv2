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
| `CreatedAt`         | int64  | Unix seconds; row insertion time. |
| `UpdatedAt`         | int64  | Unix seconds; last update time. |

## Notes

- The `ukey:"camera"` tag generates a unique index on `camera_id`, enforcing one config per camera.
- The bootstrap schema creates and auto-migrates the `recording_config` table on startup; adding `LiveStreamUrl` adds the column automatically without a manual migration.
- Config rows are loaded at app startup and applied to the `recording.Manager` via `Configure`. Runtime changes via `PUT /api/recording/config` take effect immediately through the hot-reload path.
- `LiveStreamUrl` is the mechanism for split-stream setups where live view uses a different stream than recording. It is persisted in this table so the Recording UI preserves the selection across page reloads.
- `StreamURL` and `FallbackStreamUrl` control the recording stream and fallback; they are independent of `LiveStreamUrl`.
