# Module: infra/stream/types.go

## Purpose

Defines the small stream contracts shared by the RTSP connector, WebRTC manager, and app API layer.

## Responsibilities

- Identify one camera stream source by ID and URI.
- Model stream manager options and optional ICE server settings.
- Represent browser WebRTC session descriptions as JSON-friendly `type` and `sdp` fields.
- Represent RTP packet subscriptions from camera sessions, including optional audio.
- Define the connector interface used by the WebRTC manager.

## Codec Constants

| Constant    | Value    | Notes |
|-------------|----------|-------|
| `CodecH264` | `"h264"` | H264 video; required for WebRTC live view. |
| `CodecPCMA` | `"pcma"` | G.711 A-law audio (RTP PT=8); natively decoded by all browsers. |
| `CodecPCMU` | `"pcmu"` | G.711 µ-law audio (RTP PT=0); natively decoded by all browsers. |

## Subscription Fields

| Field          | Notes |
|----------------|-------|
| `Codec`        | Video codec detected from the RTSP stream (always `CodecH264` currently). |
| `Packets`      | Channel of cloned video RTP packets for one browser peer. |
| `AudioCodec`   | Audio codec if the RTSP stream exposes a G.711 track; empty string if no audio. |
| `AudioPackets` | Channel of cloned audio RTP packets; `nil` when camera has no audio track. |
| `Close`        | Removes this subscription and stops the camera session when the last subscriber disconnects. |

## Notes

- The package is intentionally independent of ONVIF persistence so other app/device protocols can reuse it later.
- PCMA and PCMU are RFC 3551 static payload types and require no SDP negotiation overhead.
- Callers must check `AudioPackets != nil` before using audio; cameras without a G.711 track leave it nil.
