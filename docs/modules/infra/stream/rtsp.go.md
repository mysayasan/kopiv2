# Module: infra/stream/rtsp.go

## Purpose

Provides shared RTSP camera sessions for browser live view without spawning ffmpeg per stream.

## Responsibilities

- Parse and validate RTSP URLs.
- Open RTSP sessions with `gortsplib`.
- Select the first H264 video track announced by the camera (`firstH264`).
- Select the first G.711 audio track (PCMA or PCMU) announced by the camera (`firstG711`).
- Fan out cloned video RTP packets to active browser video subscriptions.
- Fan out cloned audio RTP packets to active browser audio subscriptions.
- Drop stale packets for slow subscribers so live view stays current.
- Stop the camera session when the last subscriber disconnects.
- Replace the camera session when the same source ID is requested with a different RTSP URI.

## Audio Detection

`firstG711` scans the RTSP session description for any `*format.G711` track (PCMA or PCMU).
When found, `OnPacketRTP` is registered for that media and packets are broadcast to `audioSubscribers`.
The resolved codec (`CodecPCMA` or `CodecPCMU`) is stored on the session and returned in each `Subscription.AudioCodec`.
Cameras without a G.711 track set `audioCodec = ""` and `AudioPackets = nil` in each subscription.

## Subscription Lifecycle

Each `subscribe()` call allocates a video channel and, if audio is available, an audio channel under the same subscriber ID.
`removeSubscriber` closes both channels and stops the session when the last subscriber exits.
`finish` (called on RTSP error or context cancel) closes all video and audio subscriber channels.

## Notes

- TCP transport is requested for steadier local-network camera behavior.
- Non-H264 streams are rejected by the WebRTC path and can still use the MJPEG fallback path.
- Stream selection changes (e.g., VIGI stream1 to stream2) reuse the source ID but change the URI; the connector stops the replaced session so browser tiles reconnect to the selected stream.
- Audio forwarding requires no transcoding: raw G.711 RTP packets are forwarded directly; browsers decode PCMA/PCMU natively.
