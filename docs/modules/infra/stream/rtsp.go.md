# Module: infra/stream/rtsp.go

## Purpose

Provides shared RTSP camera sessions for browser live view without spawning ffmpeg per stream.

## Responsibilities

- Parse and validate RTSP URLs.
- Open RTSP sessions with `gortsplib`.
- Select the first H264 video track announced by the camera.
- Fan out cloned RTP packets to active browser subscriptions.
- Drop stale packets for slow subscribers so live view stays current.
- Stop the camera session when the last subscriber disconnects.

## Notes

- TCP transport is requested for steadier local-network camera behavior.
- Non-H264 streams are rejected by the WebRTC path and can still use the MJPEG fallback path.
