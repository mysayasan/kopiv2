# Module: apps/mymatasan/apis/onvif.go

## Purpose

Registers ONVIF discovery and saved-device API routes for standalone `mymatasan`.

## Routes

- `POST /api/onvif/discover`: run local WS-Discovery with optional `timeoutMs`, upsert discovered devices by XAddr, then return best-effort camera metadata and unauthenticated stream hints when the camera exposes them.
- `POST /api/onvif/probe`: probe one manual IP, host, or ONVIF device-service URL.
- `GET /api/onvif/stream-config`: return current runtime WebRTC, ICE server, and MJPEG fallback live-view settings.
- `GET /api/onvif/devices`: list saved ONVIF devices.
- `POST /api/onvif/devices`: save or update an ONVIF device entity.
- `POST /api/onvif/devices/discovered`: save or update a discovery result.
- `POST /api/onvif/devices/{id}/stream-uri`: resolve a saved device to an RTSP URI.
- `POST /api/onvif/devices/{id}/camera-password`: change a camera-local ONVIF user password with Device Management `SetUser`.
- `POST /api/onvif/devices/{id}/rtsp-test`: probe the saved RTSP URI and return media track metadata.
- `POST /api/onvif/devices/{id}/live-view`: resolve the ONVIF stream and snapshot URIs for browser live view.
- `POST /api/onvif/devices/{id}/webrtc/offer`: answer a browser WebRTC offer and forward the saved RTSP H264 stream.
- `POST /api/onvif/devices/{id}/ptz/move`: move a PTZ-capable saved camera with ONVIF `ContinuousMove`.
- `POST /api/onvif/devices/{id}/ptz/stop`: stop PTZ movement.
- `GET /api/onvif/devices/{id}/live.mjpeg`: stream RTSP or snapshot frames as multipart MJPEG fallback.
- `DELETE /api/onvif/devices/{id}`: remove a saved device.

## Notes

- Route protection is provided by the app-level local Basic Auth middleware.
- Request bodies are capped at 1 MiB.
- WebRTC live view requires the RTSP stream to expose an H264 video track.
- If WebRTC is disabled by runtime settings, the frontend uses `live.mjpeg` directly.
- Camera password and PTZ routes require saved camera credentials; PTZ commands also require a PTZ service URL from ONVIF capabilities.
- Cameras that require ONVIF credentials may only expose host and XAddr during discovery; saving credentials and resolving live view fills the protected media fields.
