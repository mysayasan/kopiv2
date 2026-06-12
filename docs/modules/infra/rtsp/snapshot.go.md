# Module: infra/rtsp/snapshot.go

## Purpose

Captures a single JPEG frame from an RTSP camera stream through ffmpeg.

## Responsibilities

- Resolve the configured ffmpeg executable.
- Reuse the same decoder input options as MJPEG streaming, including RTSP transport, probe/analyze limits, optional hardware acceleration, optional hardware device selection, optional decoder name, and low-latency flags.
- Scale captured frames to a bounded width and encode one JPEG frame to stdout.
- Return clear stderr-backed errors when ffmpeg cannot start, decode, or produce a frame.

## Notes

- MyMataSan's vision monitor uses this path when a saved camera has an RTSP URI.
- Snapshot URI capture from ONVIF cameras does not use ffmpeg.
