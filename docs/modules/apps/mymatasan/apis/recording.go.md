# Module: apps/mymatasan/apis/recording.go

## Purpose

Exposes HTTP endpoints for managing per-camera recording configs, downloading or deleting recorded video clips, querying recorder status, and managing per-camera RTSP stream selection.

## Routes

| Method   | Path                                       | Description |
|----------|--------------------------------------------|-------------|
| `GET`    | `/api/recording/segments`                  | List recorded clips with optional `cameraId`, `alertId`, `startedAfter`, `startedBefore` query filters and `limit`/`offset` paging. |
| `DELETE` | `/api/recording/segments/{id}`             | Delete a clip by ID (removes the DB row and the file on disk). |
| `GET`    | `/api/recording/segments/{id}/download`    | Stream the MP4 file to the browser with `Content-Type: video/mp4`. |
| `GET`    | `/api/recording/config`                    | List all per-camera recording configs. |
| `GET`    | `/api/recording/config/{cameraId}`         | Fetch the recording config for one camera. |
| `PUT`    | `/api/recording/config`                    | Create or update the recording config for a camera (see below). |
| `GET`    | `/api/recording/status`                    | Return a `[]CameraStatus` snapshot for all configured recorders. |
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

### GET /api/recording/streams/{cameraId}

Returns all ONVIF media profiles using the credentials already stored for the device. The response from `StreamOptions` includes profile token, name, encoding, resolution, and RTSP URI for each profile.

### POST /api/recording/streams/{cameraId}/live

Body: `{"rtspUrl": "rtsp://..."}`. Updates the camera's configured live-view RTSP stream URI via `ResolveStream`.

## Notes

- All routes are mounted under the protected subrouter and require local Basic Auth.
- The download endpoint opens the file by path stored in the segment row; if the file has been deleted manually it returns a `400` error.
- `Content-Length` is set from the stored `FileSize` when non-zero, enabling browser progress bars.
- `parseInt64Query` is a shared helper defined in this file; used by both recording and vision handlers in the same `apis` package.
