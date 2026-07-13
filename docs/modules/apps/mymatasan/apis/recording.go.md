# Module: apps/mymatasan/apis/recording.go

## Purpose

Exposes HTTP endpoints for managing per-camera recording configs, downloading or deleting recorded video clips, querying recorder status, and managing per-camera RTSP stream selection.

## Routes

| Method   | Path                                       | Description |
|----------|--------------------------------------------|-------------|
| `GET`    | `/api/recording/segments`                  | List recorded clips with optional `cameraId`, `alertId`, `startedAfter`, `startedBefore` query filters and `limit`/`offset` paging. |
| `POST`   | `/api/recording/segments/purge`            | Purge segments already past each camera's `retentionDays` (the same safe sweep the disk-mitigation job runs automatically). |
| `POST`   | `/api/recording/purge-camera`              | The per-camera **"Purge now"** action: deletes ALL footage and AI-event snapshots for one camera regardless of expiry (see below). |
| `DELETE` | `/api/recording/segments/{id}`             | Delete a clip by ID (removes the DB row and the file on disk). |
| `GET`    | `/api/recording/segments/{id}/download`    | Stream the MP4 file to the browser with `Content-Type: video/mp4`. Accepts `?transcode=h264`: when the segment is stored as HEVC and the request asks for h264 (the player sets this only for browsers that can't decode HEVC), the decrypted stream is transcoded HEVC→H.264 on the fly (fragmented MP4, via the shared NVENC semaphore). Capable browsers and non-HEVC segments stream the stored bytes untouched. Honors `Range` (see below); a `Range` request is required for tunneled playback (`myseliasan`'s `/api/nodes/{id}/recording-stream/{segId}`) since it can only forward bounded chunks. |
| `GET`    | `/api/recording/segments/{id}/frame`       | Return a small JPEG frame of the segment at `?seek=<seconds>` (see below). |
| `GET`    | `/api/recording/config`                    | List all per-camera recording configs. |
| `GET`    | `/api/recording/config/{cameraId}`         | Fetch the recording config for one camera. |
| `PUT`    | `/api/recording/config`                    | Create or update the recording config for a camera (see below). |
| `GET`    | `/api/recording/status`                    | Return a `[]CameraStatus` snapshot for all configured recorders. |
| `GET`    | `/api/recording/storage/status`            | Report whether the configured at-rest storage codec can actually be produced on this host (see below). |
| `GET`    | `/api/recording/streams/{cameraId}`        | List all ONVIF media stream profiles for a camera using stored credentials. |
| `POST`   | `/api/recording/streams/{cameraId}/live`   | Update the camera's configured live-view RTSP URI from a selected profile or explicit URL. |

### PUT /api/recording/config — request body

```json
{
  "cameraId": 1,
  "enabled": true,
  "preRollSec": 30,
  "postRollSec": 10,
  "storagePath": "./recordings",
  "retentionDays": 7,
  "segmentMinutes": 15,
  "streamUrl": "",
  "fallbackStreamUrl": ""
}
```

- `streamUrl` — optional recording-stream RTSP override. When set it takes precedence over the ONVIF-discovered URI. Use this to point the recorder at a sub-stream while live view uses the main stream.
- `fallbackStreamUrl` — optional fallback RTSP URI, automatically activated after 2 consecutive quick connection failures of the primary stream.

### PUT /api/recording/config — response

```json
{
  "config": { ... },
  "recorderWarning": ""
}
```

`recorderWarning` is a non-empty string when the hot-reload attempt encountered an issue (e.g., no RTSP URI found), allowing the UI to surface it without treating the config save as an error.

### Hot-reload behaviour

`PUT /api/recording/config` persists the config **and** immediately calls `recording.Manager.Configure` to apply the change without a restart. The RTSP URI resolution order is:

1. `streamUrl` field in the request body (explicit override)
2. ONVIF `SnapshotSource` for the camera (stored credentials)
3. Error surfaced as `recorderWarning` if neither yields a URI and `enabled` is true

### GET /api/recording/storage/status

Reports whether the currently configured at-rest storage codec (`recording.storage.codec`) can actually be produced on this host, so the UI can warn the operator when the setting doesn't match the hardware.

```json
{
  "codec": "hevc",
  "reEncode": true,
  "nvencUsable": false,
  "fallbackToCopy": true,
  "compatible": false
}
```

- `reEncode` — `true` when the codec is `h264`/`hevc` (needs the GPU); `false` for `copy`/empty.
- `nvencUsable` — result of `recording.StorageCodecUsable` (probed and cached via `NVENCUsable`; always `true` when `reEncode` is `false`).
- `fallbackToCopy` — the effective `recording.storage.fallbackToCopy` value (`nil` config = `true`).
- `compatible` — `!reEncode || nvencUsable`; `false` means the configured codec needs a GPU this host doesn't have. The recorder still records — as stream-copy if `fallbackToCopy` is on, or by dropping segments if it's off — but the storage codec setting itself is wrong for the hardware.

### GET /api/recording/segments/{id}/frame

Extracts a single JPEG frame of the segment at `?seek=<seconds>` via `recording.ExtractFrameJPEG` (fast-seek ffmpeg grab; decrypted first when at-rest encryption is on). `?w=` sets the output width (default 480, clamped 160–1920 — a larger width is used for the maximized/full view). The extracted frame is cached on disk (`os.TempDir()/mymatasan-thumbs`, keyed by segment id + seek + width) since it depends only on those three inputs; an optional `?box=x,y,w,h` (normalized 0..1) plus `?label=` draws a detection box on top via `vision.AnnotateJPEG` **after** the cache read/write, so the same cached frame serves any box. Backs the Object Search result-row footage screenshot as well as the Recordings-tab and Notifications-event play thumbnails.

### GET /api/recording/segments/{id}/download — Range support

A `Range` header (browser `<video>` seeking, or the control-plane tunnel's chunked player)
is served via `http.ServeContent`, which requires a seekable `io.ReadSeeker`:

- **Plaintext, no transcode**: the on-disk file is already seekable — opened directly, no
  extra work.
- **Encrypted and/or `?transcode=h264`**: the decrypted (and, if requested, HEVC→H.264
  transcoded) stream is not seekable, so it is first materialized to a plaintext temp copy
  via `segmentPlayFile` before being served with `http.ServeContent`.
- Non-`Range` requests are unaffected: they still stream the (decrypted/transcoded) file
  directly with `io.Copy`, exactly as before.

`segmentPlayFile(r, filePath, segID, transcode)` writes the materialized copy to
`os.TempDir()/mymatasan-playcache/seg_<id>[_h264].mp4` (write-to-`.tmp`-then-rename so a
concurrent reader never sees a partial file); a later request for the same segment/variant
reuses the cached file instead of re-decrypting/re-transcoding. `cleanupPlayCache` is run
(best-effort) on each call and removes cached files whose mtime is older than one hour, so
the cache is self-pruning with no dedicated sweep job.

### POST /api/recording/purge-camera

Body or query: `cameraId`. The per-camera **"Purge now"** action (Recording tab): unlike the retention sweep, this deletes every recorded segment for the camera regardless of expiry via `recordingService.PurgeAllForCamera`, plus every alert event and its snapshot file for the camera via `visionService.PurgeAlertsForCamera` (injected as `IVisionService` on `recordingApi`). Footage removal is authoritative — its error fails the request — while snapshot removal is best-effort (logged, not fatal) so a snapshot hiccup can't leave the footage half-purged. Returns `{"segments": N, "snapshots": N}`. The UI gates this behind a 5-second cancellable countdown confirmation, mirroring the factory-reset wipe, and refreshes only the Recording tab's own segment list afterward (no full-page reload). Reachable from `myseliasan`'s embedded node camera Recording tab over the node proxy tunnel.

### GET /api/recording/streams/{cameraId}

Returns all ONVIF media profiles using the credentials already stored for the device. The response from `StreamOptions` includes profile token, name, encoding, resolution, and RTSP URI for each profile.

### POST /api/recording/streams/{cameraId}/live

Body: `{"rtspUrl": "rtsp://..."}`. Updates the camera's configured live-view RTSP stream URI via `ResolveStream`.

## Notes

- `NewRecordingApi` takes a `shredPasses int` argument (the boot-time `recording.shred` config value) and carries it on `recordingApi.shredPasses`, threading it into every `recording.RecorderConfig` the handler rebuilds on `PUT /api/recording/config`. Previously this was omitted, so saving *any* recording setting in the UI silently rebuilt the recorder with `ShredPasses=0`, degrading secure shred to a plain unlink for that camera's retention purge until the next restart.
- All routes are mounted under the protected subrouter and require local Basic Auth.
- The download endpoint opens the file by path stored in the segment row; if the file has been deleted manually it returns a `400` error.
- `Content-Length` is set from the stored `FileSize` only on the plaintext pass-through path (not when decrypting or transcoding, which stream without a known length). Range responses instead get `Content-Length`/`Content-Range` from `http.ServeContent` against the (possibly materialized) seekable source.
- Materialized playback copies live outside the encryption-at-rest and recording storage roots (`os.TempDir()`), so they are plaintext on disk for up to an hour; this is an accepted trade-off for tunnel/seek playback of otherwise-encrypted footage.
- The at-rest storage codec used when (re)configuring a recorder is read live from runtime settings (`IRuntimeSettingsService.Recording()`) on each `PUT /api/recording/config`, so a Settings → Recording change applies the next time a camera's config is saved. `RecordFallbackCopy` is passed through the same way, defaulting to enabled when `Storage.FallbackToCopy` is `nil`.
- `parseInt64Query` is a shared helper defined in this file; used by both recording and vision handlers in the same `apis` package.
