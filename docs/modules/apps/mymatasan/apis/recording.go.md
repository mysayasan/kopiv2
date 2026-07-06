# Module: apps/mymatasan/apis/recording.go

## Purpose

Exposes HTTP endpoints for managing per-camera recording configs, downloading or deleting recorded video clips, querying recorder status, and managing per-camera RTSP stream selection.

## Routes

| Method   | Path                                       | Description |
|----------|--------------------------------------------|-------------|
| `GET`    | `/api/recording/segments`                  | List recorded clips with optional `cameraId`, `alertId`, `startedAfter`, `startedBefore` query filters and `limit`/`offset` paging. |
| `DELETE` | `/api/recording/segments/{id}`             | Delete a clip by ID (removes the DB row and the file on disk). |
| `GET`    | `/api/recording/segments/{id}/download`    | Stream the MP4 file to the browser with `Content-Type: video/mp4`. Accepts `?transcode=h264`: when the segment is stored as HEVC and the request asks for h264 (the player sets this only for browsers that can't decode HEVC), the decrypted stream is transcoded HEVC→H.264 on the fly (fragmented MP4, via the shared NVENC semaphore). Capable browsers and non-HEVC segments stream the stored bytes untouched. |
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

### GET /api/recording/streams/{cameraId}

Returns all ONVIF media profiles using the credentials already stored for the device. The response from `StreamOptions` includes profile token, name, encoding, resolution, and RTSP URI for each profile.

### POST /api/recording/streams/{cameraId}/live

Body: `{"rtspUrl": "rtsp://..."}`. Updates the camera's configured live-view RTSP stream URI via `ResolveStream`.

## Notes

- All routes are mounted under the protected subrouter and require local Basic Auth.
- The download endpoint opens the file by path stored in the segment row; if the file has been deleted manually it returns a `400` error.
- `Content-Length` is set from the stored `FileSize` only on the plaintext pass-through path (not when decrypting or transcoding, which stream without a known length).
- The at-rest storage codec used when (re)configuring a recorder is read live from runtime settings (`IRuntimeSettingsService.Recording()`) on each `PUT /api/recording/config`, so a Settings → Recording change applies the next time a camera's config is saved. `RecordFallbackCopy` is passed through the same way, defaulting to enabled when `Storage.FallbackToCopy` is `nil`.
- `parseInt64Query` is a shared helper defined in this file; used by both recording and vision handlers in the same `apis` package.
