# Module: infra/recording/encode.go

## Purpose

Provides the GPU (NVENC) encode primitives shared by the recording subsystem: a global concurrency semaphore, the remux-time video-encode argument builder, and the on-the-fly HEVC→H.264 playback transcode. This is the core of the at-rest recording-compression feature.

## Responsibilities

- Maintain a process-global **NVENC semaphore** sized by `SetNVENCConcurrency(n)` (default 2). It is shared by remux-time re-encoding and playback transcode so the GPU is never oversubscribed past its concurrent-session cap.
- `AcquireNVENC(ctx)` blocks until an encode slot is free (or `ctx` is cancelled) and returns an idempotent `release` func. Because the segment `.ts` is already on disk before remux runs, waiting here can only delay finalize — it never drops footage.
- `remuxVideoArgs(codec, quality)` builds the ffmpeg output args for finalizing a segment: stream copy (`-c copy`) for `copy`/empty/unknown codecs, or NVENC re-encode (`-c:v {h264,hevc}_nvenc -preset p5 -rc vbr -cq <q> -c:a copy`) for `h264`/`hevc`. HEVC additionally emits `-tag:v hvc1` so HEVC-in-mp4 plays in Safari and Media Source Extensions. Returns the canonical on-disk codec label for the segments row. Default CQ is `defaultRecordCQ` (26).
- `TranscodeH264(ctx, ffmpegPath, src, dst)` streams an input reader to a writer as a fragmented MP4 (`-movflags frag_keyframe+empty_moov`) with `h264_nvenc` video and copied audio, for browsers that cannot decode the stored HEVC. Runs under the NVENC semaphore. Input is software-decoded (no input hwaccel) so a playback never consumes a scarce NVDEC decode session — only the encode uses the GPU.
- `nvencEncoder`, `canonicalCodec`, `reencodes` map codec spellings to NVENC encoder names / canonical labels and report whether a codec triggers a re-encode.
- `NVENCUsable(ffmpegPath, encoder)` reports whether a given NVENC encoder actually runs on this host: it runs one tiny throwaway `-f lavfi color=... -frames:v 2 -c:v <encoder> -f null -` encode (15s timeout) the first time and caches the boolean result per encoder name for the process lifetime. An empty/`copy` encoder is always reported usable (it never touches the GPU). A failed probe logs once explaining that segments configured for that codec will be stored as stream-copy instead.
- `ReEncodes(codec)` — exported wrapper over `reencodes` so the API layer can report whether a storage codec depends on the GPU without duplicating the codec table.
- `StorageCodecUsable(ffmpegPath, codec)` — combines `nvencEncoder` + `NVENCUsable`: copy/empty codecs are always usable; `h264`/`hevc` are usable only when their NVENC encoder passes the probe. Backs the `GET /api/recording/storage/status` compatibility check.

## Notes

- `SetNVENCConcurrency` should be called once at startup, before any recorder starts (app wiring reads the runtime `recording.storage.maxConcurrentEncodes` setting).
- Stream copy takes no NVENC slot; only h264/hevc re-encode and the playback transcode acquire the semaphore.
- The encoders require an NVIDIA GPU (NVENC). With no usable GPU, `NVENCUsable`/`StorageCodecUsable` let callers detect this up front instead of discovering it only when ffmpeg fails at runtime; `infra/recording/rtsp.go`'s remux path and the capacity estimator both use this to avoid dropping footage or misreporting a `0`-camera GPU ceiling when `RecordFallbackCopy` is on.
- No persistent transcode cache: HEVC→H.264 is re-done per incapable-browser playback, deliberately avoiding writing plaintext H.264 to disk (consistent with encryption-at-rest).
