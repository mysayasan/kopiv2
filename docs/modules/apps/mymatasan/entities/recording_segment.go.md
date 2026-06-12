# Module: apps/mymatasan/entities/recording_segment.go

## Purpose

Declares the `RecordingSegment` entity that persists metadata for one recorded video clip.

## Fields

| Field       | Type   | Notes |
|-------------|--------|-------|
| `Id`        | int64  | Auto-increment primary key. |
| `CameraId`  | int64  | FK reference to the camera that was recorded. |
| `AlertId`   | int64  | Optional; the alert event that triggered this clip. |
| `FilePath`  | string | Absolute or relative path to the MP4 file on the server filesystem. |
| `StartedAt` | int64  | Unix seconds; beginning of the recorded window (trigger time minus pre-roll). |
| `EndedAt`   | int64  | Unix seconds; end of the recorded window (trigger time plus post-roll). |
| `FileSize`  | int64  | File size in bytes after encoding. |
| `CreatedAt` | int64  | Unix seconds; row insertion time. |

## Notes

- The bootstrap schema creates the `recording_segment` table automatically on first startup.
- `FilePath` is used by the download endpoint to open and stream the file; the delete endpoint removes both the row and the file.
- `AlertId` is zero when the clip was not triggered by a detector alert (reserved for future manual recording).
