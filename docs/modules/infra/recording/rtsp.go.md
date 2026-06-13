# Module: infra/recording/rtsp.go

## Purpose

Implements the RTSP-mode per-camera recorder that runs a dedicated ffmpeg segmenter process and extracts MP4 clips from the rolling segment buffer on alert events.

## Responsibilities

- Run an ffmpeg process that writes the camera RTSP stream into rolling `.ts` segment files using the `-f segment` muxer with `-strftime 1` filename templating (`%Y%m%d_%H%M%S.ts`).
- Pass `-fflags +genpts` to ffmpeg to generate PTS from DTS for cameras that omit stream timestamps.
- Copy the video stream (`-c:v copy`) and transcode audio to AAC (`-c:a aac`) so all camera audio codecs (pcm_alaw/G.711, G.726, etc.) are muxed as a native MPEG-TS stream type without a private data stream warning.
- Pass `-af aresample=async=1` to compensate for cameras that send non-monotonic audio timestamps, preventing the `[aac] Queue input is backward in time` error.
- Filter known harmless ffmpeg warnings from `lastErrMsg` via `isNoisyFFmpegWarning` so the status panel shows only meaningful errors. Suppressed patterns include:
  - `Non-monotonic DTS`
  - `Timestamps are unset`
  - `This may result in incorrect timestamps`
  - `DTS discontinuity`
  - `starts with a non keyframe`
  - `is muxed as a private data stream`
  - `Queue input is backward in time`
  - `[segment @...]` address-only lines
- Restart ffmpeg automatically on unexpected process exit.
- After two consecutive quick failures (runtime < 10 s), toggle between `RTSPURI` (primary) and `FallbackRTSPURI` (fallback) to handle cameras that reject a second RTSP connection on the main stream. Fallback switching is transparent; the active stream URL is exposed via `CameraStatus`.
- Read ffmpeg stderr line-by-line with `bufio.Scanner` to avoid partial-line artefacts from raw `Read()` calls; the noisy-warning check runs before logging so filtered lines are never written to the log.
- List live `.ts` segment files and parse filenames as **local time** (`time.Local`) because ffmpeg `strftime` writes in the local timezone.
- On `TriggerEvent(alertId, frameCapturedAt)`: use `frameCapturedAt` as the clip anchor to compensate for YOLO inference latency; fall back to `time.Now()` when zero. Wait for post-roll, then select `.ts` segments covering `[frameCapturedAt − preRollSec, frameCapturedAt + postRollSec]`.
- Write a concat list and run a second ffmpeg pass with `-fflags +genpts`, `-ss`, `-t`, and `-movflags +faststart` to extract and remux the clip as MP4.
- Notify the `SegmentSink` with the resulting `SegmentResult`.
- Watch completed segments, remux each `.ts` to MP4 with `ffmpeg -c copy`, persist to DB, and delete the source `.ts` after a successful DB save.
- Expose `CameraStatus` including `state`, `ffmpegRunning`, `liveFiles`, `liveDir`, `lastError`, `activeStreamUrl`, `usingFallback`, ring-buffer frame count, and ring-buffer capacity.

## Notes

- `WriteFrame` is a no-op in RTSP mode; frames are consumed directly by the ffmpeg subprocess.
- AAC audio transcoding adds negligible CPU compared to video copy.
- `aresample=async=1` resamples audio to match timestamps when the camera sends them out of order, absorbing backward jumps before the AAC encoder sees them.
- **Timezone invariant**: segment filenames are written in local time by ffmpeg `strftime`. Both `listLiveSegments` calls use `time.ParseInLocation(..., time.Local)` — never `time.UTC`.
- The live segment directory is placed under `{storagePath}/cam{cameraId}/live/`; extracted clip files are placed under `{storagePath}/cam{cameraId}/clips/`.
- Diagnostic `log.Printf` lines in `extractClip` log the clip window and all available segments with local times to aid debugging when a clip is not found.
