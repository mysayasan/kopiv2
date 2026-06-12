# Module: infra/recording/rtsp.go

## Purpose

Implements the RTSP-mode per-camera recorder that runs a dedicated ffmpeg segmenter process and extracts MP4 clips from the rolling segment buffer on alert events.

## Responsibilities

- Run an ffmpeg process that writes the camera RTSP stream into rolling `.ts` segment files using the `-f segment` muxer with `-strftime 1` filename templating (`%Y%m%d_%H%M%S.ts`).
- Pass `-fflags +genpts` to ffmpeg to generate PTS from DTS for cameras that omit stream timestamps, preventing non-monotonic DTS warnings.
- Filter known harmless ffmpeg warnings (`Non-monotonic DTS`, `Timestamps are unset`, `DTS discontinuity`, `starts with a non keyframe`, `[segment @...]`) from `lastErrMsg` so the status panel shows only meaningful errors.
- Restart ffmpeg automatically on unexpected process exit.
- After two consecutive quick failures (runtime < 10 s), toggle between `RTSPURI` (primary) and `FallbackRTSPURI` (fallback) to handle cameras that reject a second RTSP connection on the main stream. Fallback switching is transparent; the active stream URL is exposed via `CameraStatus`.
- Read ffmpeg stderr line-by-line with `bufio.Scanner` to avoid partial-line artefacts from raw `Read()` calls.
- List live `.ts` segment files and parse filenames as **local time** (`time.Local`) because ffmpeg `strftime` writes in the local timezone; parsing as UTC was the historical source of the "Clip not found" error.
- On `TriggerEvent(alertId, frameCapturedAt)`: use `frameCapturedAt` (the Unix second timestamp of the source frame) as the clip anchor. If `frameCapturedAt` is zero, fall back to `time.Now()`. Wait for the configured post-roll duration, then select the `.ts` segments that overlap the `[frameCapturedAt − preRollSec, frameCapturedAt + postRollSec]` window. Anchoring on the frame timestamp rather than the detection-completion time ensures YOLO inference latency (100–500 ms) does not shift the clip window away from when the subject was actually visible.
- Write a concat list and run a second ffmpeg pass (also with `-fflags +genpts`) to extract and remux the clip as MP4 with `-ss`, `-t`, and `-movflags +faststart`.
- Notify the `SegmentSink` with the resulting `SegmentResult`.
- Expose `CameraStatus` including `state` (`streaming`/`stopped`/`error`), `ffmpegRunning`, `liveFiles`, `liveDir`, `lastError`, `activeStreamUrl`, `usingFallback`, ring-buffer frame count, and ring-buffer capacity.

## Notes

- `WriteFrame` is a no-op in RTSP mode; frames are consumed directly by the ffmpeg subprocess.
- The segment wrap count is calculated from `ceil((preRoll + postRoll) / segmentTime) + 3` to ensure enough history is always retained.
- **Timezone invariant**: segment filenames are written in local time by ffmpeg `strftime`. Both `listLiveSegments` calls use `time.ParseInLocation(..., time.Local)` — never `time.UTC`.
- The live segment directory is placed under `{storagePath}/cam{cameraId}/live/`; extracted clip files are placed under `{storagePath}/cam{cameraId}/`.
- When the context is cancelled, the ffmpeg process is killed and the watcher goroutine exits.
- `-c copy` is used for both segmenting and clip extraction, so no re-encoding occurs in RTSP mode; the output codec matches the camera stream.
- Diagnostic `log.Printf` lines in `extractClip` log the clip window and all available segments with local times to aid debugging when a clip is not found.
