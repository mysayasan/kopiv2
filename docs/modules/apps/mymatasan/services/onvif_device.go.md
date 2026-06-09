# Module: apps/mymatasan/services/onvif_device.go

## Purpose

Connects the reusable ONVIF infra client to `mymatasan` persistence.

## Responsibilities

- Run discovery and manual probe operations through `infra/onvif`.
- List saved ONVIF devices ordered by latest sighting.
- Save discovered or manually entered devices.
- Upsert by unique `XAddr` so repeated discovery refreshes existing records.
- Refresh media and PTZ capability URLs after saving credentials, resolving live view, or resolving stream state.
- Change a camera-local ONVIF user password and update the stored credentials used by later live view.
- Send PTZ move and stop commands for saved cameras that expose a PTZ service.
- Resolve saved devices to RTSP stream URIs through ONVIF media services.
- Resolve saved devices to ONVIF snapshot URIs for browser MJPEG live view.
- Probe saved RTSP URIs through `infra/rtsp` and persist transport/track status.
- Delete saved devices by ID.

## Notes

- The service records `LastSeenAt`, `CreatedAt`, and `UpdatedAt` using Unix seconds.
- Discovery metadata arrays are persisted as space-separated strings to stay compatible with the current reflection-based schema bootstrap.
- Camera credentials are stored on the device record for this first standalone pass; the later `myseliasan` control protocol should replace this simple storage boundary.
- PTZ direction requests are normalized to pan/tilt velocity values before calling the infra ONVIF client.
