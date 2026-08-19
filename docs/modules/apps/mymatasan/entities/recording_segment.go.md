# Module: apps/mymatasan/entities/recording_segment.go

## Purpose

Declares the `RecordingSegment` entity that persists metadata for one recorded video clip.

## Fields

| Field       | Type   | Notes |
|-------------|--------|-------|
| `Id`        | int64  | Auto-increment primary key. |
| `CameraId`  | int64  | FK reference to the camera that was recorded. Carries `idx:"cam_time"`. |
| `AlertId`   | int64  | Optional; the alert event that triggered this clip. |
| `FilePath`  | string | Absolute or relative path to the MP4 file on the server filesystem. |
| `StartedAt` | int64  | Unix seconds; beginning of the recorded window (trigger time minus pre-roll). Carries `idx:"cam_time"`. |
| `EndedAt`   | int64  | Unix seconds; end of the recorded window (trigger time plus post-roll). |
| `FileSize`  | int64  | File size in bytes after encoding. |
| `Codec`     | string | On-disk video codec (`h264`/`hevc`); empty for legacy rows. The playback path reads it to decide whether the browser needs an on-the-fly transcode. |
| `Sha256`    | string | Hex SHA-256 of this segment's PLAINTEXT mp4, taken at finalize before at-rest encryption (`infra/recording.HashPlaintextFile`, `docs/modules/infra/recording/hash.go.md`). **Empty means unhashed, not unchanged** — rows written before this column existed have none, and neither does a segment adopted after a crash (by then the file on disk is already encrypted). An evidence export must report that difference rather than paper over it: a digest taken at export time proves only that the file has not changed since the export, a materially weaker claim than "not altered since it was recorded". |
| `CreatedAt` | int64  | Unix seconds; row insertion time. |

## Notes

- The bootstrap schema creates the `recording_segment` table automatically on first startup; the additive `codec` and `sha256` columns are reconciled onto existing installs by the bootstrap drift check.
- `CameraId`+`StartedAt` form a `(camera_id, started_at)` secondary index (`idx:"cam_time"`, `infra/db/bootstrap/schema.go`'s non-unique `idx:"group"` tag), keeping the Object Search footage-linkage sweep (`services.ObservationService.fetchCoveringCandidates`) and other camera+time-range segment queries off a full-table scan as the table grows.
- `FilePath` is used by the download endpoint to open and stream the file; the delete endpoint removes both the row and the file.
- `AlertId` is zero when the clip was not triggered by a detector alert (reserved for future manual recording).
