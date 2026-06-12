# Module: infra/rtsp/client.go

## Purpose

Provides a cross-platform RTSP boundary for camera stream probing without an external transcoder process.

## Responsibilities

- Parse and validate `rtsp://` and `rtsps://` URLs.
- Open RTSP sessions through `gortsplib`.
- Run DESCRIBE and SETUP to confirm announced media tracks are usable.
- Return transport, media type, codec, payload type, clock rate, and check timestamp.

## Notes

- This module does not transcode or render video frames.
- `mymatasan` uses it after ONVIF resolves the camera's RTSP URI.
