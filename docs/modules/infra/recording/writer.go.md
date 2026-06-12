# Module: infra/recording/writer.go

## Purpose

Encodes a slice of JPEG frames into an MP4 file by piping them to ffmpeg's `image2pipe` demuxer.

## Responsibilities

- Accept a slice of `FrameEntry` values and a target file path.
- Concatenate all JPEG bytes into a single stdin pipe and drive an ffmpeg process that decodes MJPEG and re-encodes to H.264 (`libx264`, `yuv420p`, `faststart`).
- Report the combined ffmpeg stderr output on failure for diagnostics.
- Resolve the ffmpeg executable path from the configured value or system `PATH`.

## Notes

- Output is compatible with browser `<video>` playback and range-request download via the recording API.
- Frame rate defaults to `1` fps when not configured; the caller sets this to match the vision monitor tick rate.
- `WriteMP4` is called from a background goroutine; it uses a five-minute context timeout to avoid hanging indefinitely on slow encoding.
- `libx264` must be available in the ffmpeg build; hardware-encoded output is not currently supported in tick mode.
