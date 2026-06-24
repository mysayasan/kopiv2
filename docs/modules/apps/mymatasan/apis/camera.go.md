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
| DELETE | `/api/cameras/{id}`     | `delete`         | Deletes a saved camera. |
| GET    | `/api/cameras/{id}/encoder` | `getEncoder` | Reads the camera's current ONVIF video encoder config (codec, resolution, fps, bitrate) for its recording profile. |
| POST   | `/api/cameras/{id}/encoder` | `applyEncoder` | Pushes a recording codec (`h264`/`h265`) + optional `bitrateLimitKbps` to the camera's own encoder via ONVIF (zero host cost; host stays stream-copy). |
| GET    | `/api/cameras/{id}/lpr-capability` | `lprCapability` | Returns `LPRCapabilityResult` — whether the camera can supply plate-legible frames and (when ONVIF profiles are readable) the highest-resolution profile's RTSP URL for auto-pick capture. Best-effort and cached server-side (15 min TTL). |

(PTZ, stream-options, stream-uri, live-view, webrtc, and password routes are also registered here.)

## Camera-side encoder (getEncoder / applyEncoder)

`applyEncoder` accepts `{ "encoding": "h264"|"h265", "bitrateLimitKbps": int }` and calls `ICameraService.ApplyCameraEncoder`, which resolves the camera's ONVIF device/media/profile and applies + verifies the change (Media2-first, Media1 fallback — see `infra/onvif/encoder.go`). On failure both handlers pass the **descriptive error itself** to `SendError` (not the generic `ErrBadRequest`), so the camera's real reason — e.g. "the camera did not apply H265 — it kept H264 …" — reaches the client instead of a bare "bad request".

## updateDetails

Accepts `{ "name": string, "description": string }`.
Loads the existing camera record first to preserve all other fields (RTSP URI, credentials, ONVIF config, etc.), then replaces only `name` and `description` before calling `Save`.
Returns a `"succeed"` envelope on success.

## Notes

- The route group is registered under `/api/cameras` with Basic Auth applied to all camera routes.
- `PUT /api/cameras/{id}` was added to fix a bug where "Save Details" in the Camera tab silently reported success without persisting data (the previous implementation called `POST /api/cameras` which had no POST handler, causing the request to be silently dropped by the router).
