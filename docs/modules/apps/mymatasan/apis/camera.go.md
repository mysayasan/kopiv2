# Module: apps/mymatasan/apis/camera.go

## Purpose

Exposes HTTP endpoints for camera (ONVIF device) management in the MyMataSan app.

## Endpoints

| Method | Path                    | Handler          | Notes |
|--------|-------------------------|------------------|-------|
| GET    | `/api/cameras`          | `list`           | Returns all saved cameras with details. |
| POST   | `/api/cameras`          | `save`           | Creates a new camera entry. |
| GET    | `/api/cameras/{id}`     | `getById`        | Returns one camera by ID. |
| PUT    | `/api/cameras/{id}`     | `updateDetails`  | Updates `name` and `description` only; preserves all other camera fields. |
| DELETE | `/api/cameras/{id}`     | `delete`         | Deletes a saved camera. Validates `{id}` via the shared `pathID()` helper (`400` on a non-numeric id) and confirms the camera exists via `GetById` (`404` when it does not) before calling `ICameraService.Delete`, so a bad or unknown id no longer falls through to a `500` — previously the handler discarded the `ParseUint` error, so any non-numeric id silently became id `0`, and the repo's "no rows affected" error for that nonexistent row surfaced as an internal-error response with a leaked message. Cascades: `ICameraService.Delete` (`services/camera.go`) first runs every cleanup registered via `services.CameraDeletionCascade` — stop the recorder/detect-only stream, purge segments, purge observations, delete detection rules, purge alerts, then delete the recording config — before removing the camera row. Any cleanup failure aborts the delete (returns an error, camera row untouched) rather than leaving the camera half-removed. See `apps/mymatasan/app/app.go.md`. |
| GET    | `/api/cameras/{id}/encoder` | `getEncoder` | Reads the camera's current ONVIF video encoder config (codec, resolution, fps, bitrate) for its recording profile. |
| POST   | `/api/cameras/{id}/encoder` | `applyEncoder` | Pushes a recording codec (`h264`/`h265`) + optional `bitrateLimitKbps` to the camera's own encoder via ONVIF (zero host cost; host stays stream-copy). |
| GET    | `/api/cameras/{id}/lpr-capability` | `lprCapability` | Returns `LPRCapabilityResult` — whether the camera can supply plate-legible frames and (when ONVIF profiles are readable) the highest-resolution profile's RTSP URL for auto-pick capture. Best-effort and cached server-side (15 min TTL). |
| GET    | `/api/cameras/{id}/talk` | `talkCapability` | Returns `TalkCapabilityResult` (`{supported, transport, needsPassword, hasPassword, detail}`) — whether the camera supports two-way audio (talk-back), over the ONVIF backchannel or the TP-Link Tapo/VIGI port-8800 protocol, and whether a speaker password is required/stored. Cached server-side (10 min TTL, `hasPassword` always live); backs the live-view mic button's show/hide and the Access tab's TP-Link password panel. |
| POST   | `/api/cameras/{id}/talk/password` | `saveTalkPassword` | Body `{"password": string}`. Stores the TP-Link cloud/speaker password used by the talk transport (`ICameraService.SaveTalkPassword`) and returns the refreshed `TalkCapabilityResult`. Only surfaced in the UI (Access tab) when `talkCapability` reports `needsPassword: true`, which only happens for a camera that genuinely fingerprinted as a TP-Link talk service (`infra/talk.Probe8800`). |
| POST   | `/api/cameras/{id}/talk/offer` | `createTalkAnswer` | Negotiates a browser→camera talk-back audio session: accepts a sendonly PCMA microphone WebRTC offer (`{type, sdp}`), opens a talk session to the camera (`ICameraService.OpenTalkSession`), and returns the WebRTC answer (`infra/talk.AnswerBrowserTalk`) that pumps the mic audio into the camera's speaker. |
| POST   | `/api/cameras/discovered` | `saveDiscovered` | Saves a discovered camera. Accepts optional `username`/`password`; if supplied, they are verified (`VerifyDeviceCredentials`) before saving — a camera that actively rejects the login returns `400` and is **not** saved. An unreachable camera is still allowed through. |
| GET    | `/api/cameras/{id}/auth-check` | `authCheck` | Returns `{"status": "ok"\|"unauthorized"\|"unreachable"}` for the camera's **stored** credentials (`CameraAuthStatus`). The camera node's access gate polls this and prompts for new credentials only on a definitive `"unauthorized"`. |
| GET    | `/api/cameras/{id}/onvif-users` | `listCameraUsers` | Lists the camera's local ONVIF user accounts (Device Management `GetUsers`). |
| POST   | `/api/cameras/{id}/onvif-users` | `createCameraUser` | Creates a local ONVIF user (`username`/`password`/`userLevel`); returns the refreshed user list. |
| DELETE | `/api/cameras/{id}/onvif-users/{username}` | `deleteCameraUser` | Deletes a local ONVIF user; refuses to delete the account the app itself authenticates with. Returns the refreshed user list. |
| POST   | `/api/cameras/{id}/reboot` | `rebootCamera` | ONVIF `SystemReboot`; returns `{"message": "..."}` with the device's reboot message. |
| POST   | `/api/cameras/{id}/factory-default` | `factoryDefaultCamera` | ONVIF `SetSystemFactoryDefault`. Body `{"hard": bool}` — `false` (default) keeps network config (Soft), `true` wipes it (Hard). |
| GET    | `/api/cameras/{id}/datetime` | `getCameraDateTime` | Returns the camera's clock configuration (`GetSystemDateAndTime` merged with `GetNTP`). |
| POST   | `/api/cameras/{id}/datetime` | `setCameraDateTime` | Sets the camera clock (`SetCameraDateTimeRequest` body); in NTP mode also pushes the NTP server list. Returns the refreshed date/time. |
| GET    | `/api/cameras/{id}/network` | `getCameraNetwork` | Returns the camera's network config (interfaces, gateway, DNS). |
| POST   | `/api/cameras/{id}/network` | `setCameraNetwork` | Sets one NIC's IPv4 config, gateway, and DNS (`SetCameraNetworkRequest` body). |
| GET    | `/api/cameras/{id}/capabilities` | `getCameraCapabilities` | Returns `CameraCapabilities`: which ONVIF services/operations the camera supports, so the UI can hide unsupported management boxes. |
| GET    | `/api/cameras/{id}/device-info` | `getCameraDeviceInfo` | Returns `CameraDeviceInfo` (manufacturer/model/firmware/hardware/serial/location/MAC/ONVIF version/URI) for the Live View → Camera Information panel. |

(PTZ, stream-options, stream-uri, live-view, webrtc, and password routes are also registered here.)

## Credential verification (Add flow + access gate)

`saveDiscovered` and `saveCredentials` both call `ICameraService.VerifyDeviceCredentials` / the underlying `verifyCredentials` core before persisting: it resolves the ONVIF `GetStreamURI` and/or probes RTSP `DESCRIBE` with the supplied creds. `classifyCredError` maps the failure to `"unauthorized"` (HTTP 400/401/403, "NotAuthorized", "forbidden", "authentication" — many ONVIF cameras reject a bad WS-Security digest with a `400` + `NotAuthorized` SOAP fault rather than a `401`) or `"unreachable"` (timeout, connection refused, DNS — never treated as bad credentials, since a temporarily offline camera must not block a save). Only a definitive `"unauthorized"` blocks the save/update; `SaveCredentials` returns `ErrCameraUnauthorized` wrapped with the underlying probe error in that case. `GET /api/cameras/{id}/auth-check` exposes the same check for already-saved cameras so the camera node's UI can gate access when stored credentials stop working (e.g. after the operator changes the camera's password out-of-band).

## Per-stream preview / test without persisting

The WebRTC offer handler (`createWebRTCAnswer`) and `testStream` both accept an optional `rtspUrl` in the request body. When set, they route to `PreviewSource`/`TestStreamURL` instead of `SnapshotSource`/`TestStream` — resolving/probing that specific detected-profile URL with the camera's stored credentials but **never writing it back** to the camera's saved detail. The WebRTC preview also uses a distinct stream-manager source ID (`camera-{id}-preview` vs `camera-{id}`) so a live preview of an alternate stream never collides with or disturbs the camera's active stream (used by recording and detection).

## Camera-side encoder (getEncoder / applyEncoder)

`applyEncoder` accepts `{ "encoding": "h264"|"h265", "bitrateLimitKbps": int }` and calls `ICameraService.ApplyCameraEncoder`, which resolves the camera's ONVIF device/media/profile and applies + verifies the change (Media2-first, Media1 fallback — see `infra/onvif/encoder.go`). On failure both handlers pass the **descriptive error itself** to `SendError` (not the generic `ErrBadRequest`), so the camera's real reason — e.g. "the camera did not apply H265 — it kept H264 …" — reaches the client instead of a bare "bad request".

## updateDetails

Accepts `{ "name": string, "description": string }`.
Loads the existing camera record first to preserve all other fields (RTSP URI, credentials, ONVIF config, etc.), then replaces only `name` and `description` before calling `Save`.
Returns a `"succeed"` envelope on success.

## Notes

- The route group is registered under `/api/cameras` with Basic Auth applied to all camera routes.
- `PUT /api/cameras/{id}` was added to fix a bug where "Save Details" in the Camera tab silently reported success without persisting data (the previous implementation called `POST /api/cameras` which had no POST handler, causing the request to be silently dropped by the router).
- `NewCameraApi` takes an extra `audit *Auditor` parameter (`apis/audit.go.md`). `saveCredentials` records `services.ActionCameraCredentialChange` (target `camera`, id) on both success and failure — the USERNAME is recorded, the password never is, since the trail is readable by every admin and exported to CSV.
