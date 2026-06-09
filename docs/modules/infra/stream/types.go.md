# Module: infra/stream/types.go

## Purpose

Defines the small stream contracts shared by the RTSP connector, WebRTC manager, and app API layer.

## Responsibilities

- Identify one camera stream source by ID and URI.
- Model stream manager options and optional ICE server settings.
- Represent browser WebRTC session descriptions as JSON-friendly `type` and `sdp` fields.
- Represent RTP packet subscriptions from camera sessions.
- Define the connector interface used by the WebRTC manager.

## Notes

- The first supported browser codec is H264.
- The package is intentionally independent of ONVIF persistence so other app/device protocols can reuse it later.
