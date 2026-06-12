# Module: infra/rtsp/mjpeg.go

## Purpose

Converts RTSP camera streams into browser-friendly multipart MJPEG output by launching ffmpeg.

## Responsibilities

- Resolve the configured ffmpeg executable or fall back to `ffmpeg` from `PATH`.
- Build ffmpeg input arguments for RTSP transport, probe/analyze limits, optional hardware acceleration, optional hardware device initialization, optional video decoder selection, and low-latency flags.
- Bound live-view FPS and width before streaming.
- Encode MJPEG output with configured quality, thread count, flush behavior, and multipart boundary.
- Stream ffmpeg stdout into the HTTP response while respecting request cancellation.

## Notes

- Hardware acceleration options are passed as explicit ffmpeg arguments, not shell-expanded command text.
- `hwaccel: none` omits hardware flags and keeps CPU software decoding as the safest default.
- Browser WebRTC live view does not use this path; this module is for MJPEG fallback and related RTSP conversion only.
