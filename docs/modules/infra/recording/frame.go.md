# Module: infra/recording/frame.go

## Purpose

Extracts a single JPEG frame at an arbitrary seek offset from a recorded (mp4) segment, for footage-preview thumbnails — the primitive behind Object Search's per-result screenshot and the Recordings/Notifications play thumbnails.

## Responsibilities

- `ExtractFrameJPEG(ctx, ffmpegPath, src io.Reader, seekSeconds, width)` — grabs one JPEG frame at `seekSeconds` and scales it to `width` px wide (aspect preserved, `-2` height). A pipe/stream is not seekable, so the (already-decrypted) `src` is spooled to a temp file first, letting ffmpeg fast-seek by input option (`-ss` before `-i`) instead of decoding from the start; the temp file is removed on return. If the seek lands past the clip end (empty output), it retries once at `seekSeconds=0` so a thumbnail is still produced rather than an error.
- `runFrameGrab(ctx, ffmpeg, srcPath, seekSeconds, width)` — the actual ffmpeg invocation: `-ss <seek> -i <path> -frames:v 1 -vf scale=<width>:-2 -f mjpeg pipe:1`, captured from stdout; a non-zero exit returns the error wrapped with trimmed stderr.

## Notes

- `ffmpegPath` is resolved via the shared `resolveFFmpeg` helper (same resolution as the rest of the recording package — runtime decoder settings, falling back to `PATH`).
- Stateless and cheap enough to call per request; the caller (`apis/recording.go`'s `segmentFrame` handler) is responsible for disk-caching the result keyed by `(segment, seek, width)`, since the frame itself depends only on those three inputs.
- Width has no built-in cap here; the HTTP handler clamps it (`160`–`1920`) before calling in.
