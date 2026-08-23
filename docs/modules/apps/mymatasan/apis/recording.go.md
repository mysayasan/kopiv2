# Module: apps/mymatasan/apis/recording.go

## Purpose

Exposes HTTP endpoints for managing per-camera recording configs, downloading or deleting recorded video clips, querying recorder status, and managing per-camera RTSP stream selection.

## Routes

| Method   | Path                                       | Description |
|----------|--------------------------------------------|-------------|
| `GET`    | `/api/recording/segments`                  | List recorded clips with optional `cameraId`, `alertId`, `startedAfter`, `startedBefore` query filters and `limit`/`offset` paging. |
| `POST`   | `/api/recording/segments/purge`            | Purge segments already past each camera's `retentionDays` (the same safe sweep the disk-mitigation job runs automatically). |
| `POST`   | `/api/recording/purge-camera`              | The per-camera **"Purge now"** action: deletes ALL footage, AI-event snapshots and object metadata (including appearance descriptors) for one camera regardless of expiry (see below). |
| `DELETE` | `/api/recording/segments/{id}`             | Delete a clip by ID (removes the DB row and the file on disk). |
| `GET`    | `/api/recording/segments/{id}/download`    | Stream the MP4 file to the browser with `Content-Type: video/mp4`. Accepts `?transcode=h264`: when the segment is stored as HEVC and the request asks for h264 (the player sets this only for browsers that can't decode HEVC), the decrypted stream is transcoded HEVC→H.264 on the fly (fragmented MP4, via the shared NVENC semaphore). Capable browsers and non-HEVC segments stream the stored bytes untouched. Honors `Range` (see below); a `Range` request is required for tunneled playback (`myseliasan`'s `/api/nodes/{id}/recording-stream/{segId}`) since it can only forward bounded chunks. |
| `GET`    | `/api/recording/segments/{id}/frame`       | Return a small JPEG frame of the segment at `?seek=<seconds>` (see below). |
| `GET`    | `/api/recording/config`                    | List all per-camera recording configs. |
| `GET`    | `/api/recording/config/{cameraId}`         | Fetch the recording config for one camera. |
| `PUT`    | `/api/recording/config`                    | Create or update the recording config for a camera (see below). |
| `GET`    | `/api/recording/status`                    | Return a `[]CameraStatus` snapshot for all configured recorders. |
| `GET`    | `/api/recording/storage/status`            | Report whether the configured at-rest storage codec can actually be produced on this host (see below). |
| `GET`    | `/api/recording/coverage?cameraId=&from=&to=&bucket=hour\|day` | Was there actually footage for a camera over a range, bucketed hourly or daily (see below). |
| `GET`    | `/api/recording/timeline?cameraId=&from=&to=` | The Timeline screen's scrub bar: each camera's playable segment index and merged footage spans over a window (see below). |
| `GET`    | `/api/recording/timeline/seek?cameraId=&at=` | Resolve one wall-clock moment to a playable `(segment, offset)` per camera, snapping forward over a gap (see below). |
| `GET`    | `/api/recording/streams/{cameraId}`        | List all ONVIF media stream profiles for a camera using stored credentials. |
| `POST`   | `/api/recording/streams/{cameraId}/live`   | Update the camera's configured live-view RTSP URI from a selected profile or explicit URL. |

### PUT /api/recording/config — request body

```json
{
  "cameraId": 1,
  "enabled": true,
  "preRollSec": 30,
  "postRollSec": 10,
  "storagePath": "./recordings",
  "retentionDays": 7,
  "segmentMinutes": 15,
  "streamUrl": "",
  "fallbackStreamUrl": ""
}
```

- `streamUrl` — optional recording-stream RTSP override. When set it takes precedence over the ONVIF-discovered URI. Use this to point the recorder at a sub-stream while live view uses the main stream.
- `fallbackStreamUrl` — optional fallback RTSP URI, automatically activated after 2 consecutive quick connection failures of the primary stream.

### PUT /api/recording/config — response

```json
{
  "config": { ... },
  "recorderWarning": ""
}
```

`recorderWarning` is a non-empty string when the hot-reload attempt encountered an issue (e.g., no RTSP URI found), allowing the UI to surface it without treating the config save as an error.

### Hot-reload behaviour

`PUT /api/recording/config` persists the config **and** immediately calls `recording.Manager.Configure` to apply the change without a restart. The RTSP URI resolution order is:

1. `streamUrl` field in the request body (explicit override)
2. ONVIF `SnapshotSource` for the camera (stored credentials)
3. Error surfaced as `recorderWarning` if neither yields a URI and `enabled` is true

### GET /api/recording/storage/status

Reports whether the currently configured at-rest storage codec (`recording.storage.codec`) can actually be produced on this host, so the UI can warn the operator when the setting doesn't match the hardware.

```json
{
  "codec": "hevc",
  "reEncode": true,
  "nvencUsable": false,
  "fallbackToCopy": true,
  "compatible": false
}
```

- `reEncode` — `true` when the codec is `h264`/`hevc` (needs the GPU); `false` for `copy`/empty.
- `nvencUsable` — result of `recording.StorageCodecUsable` (probed and cached via `NVENCUsable`; always `true` when `reEncode` is `false`).
- `fallbackToCopy` — the effective `recording.storage.fallbackToCopy` value (`nil` config = `true`).
- `compatible` — `!reEncode || nvencUsable`; `false` means the configured codec needs a GPU this host doesn't have. The recorder still records — as stream-copy if `fallbackToCopy` is on, or by dropping segments if it's off — but the storage codec setting itself is wrong for the hardware.

### GET /api/recording/segments/{id}/frame

Extracts a single JPEG frame of the segment at `?seek=<seconds>` via `recording.ExtractFrameJPEG` (fast-seek ffmpeg grab; decrypted first when at-rest encryption is on). `?w=` sets the output width (default 480, clamped 160–1920 — a larger width is used for the maximized/full view). The extracted frame is cached on disk (`os.TempDir()/mymatasan-thumbs`, keyed by segment id + seek + width) since it depends only on those three inputs; an optional `?box=x,y,w,h` (normalized 0..1) plus `?label=` draws a detection box on top via `vision.AnnotateJPEG` **after** the cache read/write, so the same cached frame serves any box. Backs the Object Search result-row footage screenshot as well as the Recordings-tab and Notifications-event play thumbnails.

### GET /api/recording/segments/{id}/download — Range support

A `Range` header (browser `<video>` seeking, or the control-plane tunnel's chunked player)
is served via `http.ServeContent`, which requires a seekable `io.ReadSeeker`:

- **Plaintext, no transcode**: the on-disk file is already seekable — opened directly, no
  extra work.
- **Encrypted and/or `?transcode=h264`**: the decrypted (and, if requested, HEVC→H.264
  transcoded) stream is not seekable, so it is first materialized to a plaintext temp copy
  via `segmentPlayFile` before being served with `http.ServeContent`.
- Non-`Range` requests are unaffected: they still stream the (decrypted/transcoded) file
  directly with `io.Copy`, exactly as before.

`segmentPlayFile(r, filePath, segID, transcode)` writes the materialized copy to
`os.TempDir()/mymatasan-playcache/seg_<id>[_h264].mp4` (write-to-`.tmp`-then-rename so a
concurrent reader never sees a partial file); a later request for the same segment/variant
reuses the cached file instead of re-decrypting/re-transcoding. `cleanupPlayCache` is run
(best-effort) on each call and removes cached files whose mtime is older than one hour, so
the cache is self-pruning with no dedicated sweep job.

### POST /api/recording/purge-camera

Body or query: `cameraId`. The per-camera **"Purge now"** action (Recording tab): unlike the retention sweep, this deletes every recorded segment for the camera regardless of expiry via `recordingService.PurgeAllForCamera`, every alert event and its snapshot file for the camera via `visionService.PurgeAlertsForCamera` (injected as `IVisionService` on `recordingApi`), **and** every object-metadata observation for the camera via `observationService.PurgeAllForCamera` (injected as `*services.ObservationService` — see `NewRecordingApi`'s new `observation` parameter below). Footage removal is authoritative — its error fails the request — while snapshot and metadata removal are both best-effort (logged, not fatal) so a hiccup in either can't leave the footage half-purged. Returns `{"segments": N, "snapshots": N, "observations": N}`.

**The metadata leg is W3-2** and closes a real gap: before it, "Purge now" destroyed the video while leaving the object index — and, once appearance search shipped, an appearance descriptor for every person/vehicle the camera had seen — intact and pointing at footage that no longer existed. `observationService.PurgeAllForCamera` cascades to appearance descriptors internally (deletes them BEFORE the observation rows that own them — see `services/observation.go.md`'s *Appearance purge*), so this one call also removes the appearance index. Found by the W3-2 bench, which purged a camera and then successfully ranked a sighting recorded on it.

The UI gates this behind a 5-second cancellable countdown confirmation, mirroring the factory-reset wipe, and refreshes only the Recording tab's own segment list afterward (no full-page reload). Reachable from `myseliasan`'s embedded node camera Recording tab over the node proxy tunnel.

### GET /api/recording/streams/{cameraId}

Returns all ONVIF media profiles using the credentials already stored for the device. The response from `StreamOptions` includes profile token, name, encoding, resolution, and RTSP URI for each profile. `listCameraStreams` first confirms the camera exists via `camera.GetById` and returns `404` when it does not, rather than letting `StreamOptions`'s device-lookup miss surface as a `500` — previously probing an unknown `cameraId` answered "internal server error".

### POST /api/recording/streams/{cameraId}/live

Body: `{"rtspUrl": "rtsp://..."}`. Updates the camera's configured live-view RTSP stream URI via `ResolveStream`.

### GET /api/recording/coverage

The read model behind the coverage strip UI — and the same one `RecordingContinuityMonitor` scores against, so the screen and the alert can never disagree (see `services/recording_coverage.go.md`). `cameraId` is required; `to` defaults to now, `from` defaults to the last day (hour buckets) or last month (day buckets) when omitted. Bounded to `coverageMaxBuckets` (768) buckets per request — a month of days, or roughly a month of hours — and returns a `400` naming the cap rather than silently truncating the response, since a silently shortened coverage report reads as "the footage is missing" when it only means "you asked for too much".

### GET /api/recording/timeline and /api/recording/timeline/seek

Deliberately under `/api/recording` rather than a new top-level prefix: the Recordings page grant is `canRead("/api/recording")` (`services/pages.go.md`), so every role that can already browse footage gets the Timeline screen on upgrade with no separate grant to add — a new prefix would have shipped ungranted on every existing role. Backed by `IRecordingService.Timeline`/`SeekAt` (`services/recording_timeline.go.md`), which share their span arithmetic with `Coverage` (`services/recording_coverage.go.md`) so the scrub bar, the coverage strip, and the continuity monitor can never disagree about the same hour.

**Detection marks are deliberately NOT served here.** They come from `/api/vision/alerts`, a different page grant — an operator granted Recordings but not Alerts must not learn what the AI recognised by scrubbing a bar; the frontend fetches marks separately and simply omits them (`tl.marksDenied`) when that read is refused.

`timelineCameraIds(r)` reads `?cameraId=` in either repeated (`cameraId=1&cameraId=2`) or comma-separated (`cameraId=1,2`) form, de-duplicating while preserving the caller's order (the tile order on screen). Both routes cap at `timelineMaxCameras` (8) and require at least one camera id, `400`ing otherwise.

- `GET /api/recording/timeline?cameraId=&from=&to=` — `to` defaults to (and is clamped to) now, since an in-progress segment has no `EndedAt` and is credited to the end of the window; `from` defaults to `to - 86400`. The span is capped at `timelineMaxSpan` (31 days), `400`ing with the cap rather than truncating silently, same reasoning as `/coverage`. A camera with more than `timelineMaxSegments` (12000) rows in the window also `400`s (`services.ErrTimelineTooManySegments`) rather than returning a truncated, newest-first list — a truncated read would silently drop the *oldest* segments and render the left half of the bar as an empty gap, which reads as "no footage" rather than "ask for a narrower window".
- `GET /api/recording/timeline/seek?cameraId=&at=` — `at` (unix seconds) is required. Response is `{"at": <requested>, "cameras": [TimelineSeek, ...]}`; each `TimelineSeek` reports whether that camera has footage at or after `at`, and — this is deliberately a server call rather than browser-side arithmetic over the cached index — resolves against the live segment table, so a segment the browser's index still names can already have been evicted by disk-pressure purge.

## Auditing

`NewRecordingApi` takes an extra `audit *Auditor` parameter (`apis/audit.go.md`); a nil value is tolerated (a no-op) so a partially-wired test handler still works. Recorded:

- `recording.view` / `recording.download` (`downloadSegment`) — once per playback. A ranged request is a scrubbing `<video>` element rather than a distinct viewing, so only the FIRST range of a playback is recorded (the opening request of any playback is unranged), otherwise seeking through one clip would write dozens of rows and bury the trail.
- `recording.delete` (`deleteSegment`) — the row is read BEFORE the delete so the camera and time window are captured in the entry; afterwards "recording 412 was deleted" answers nothing about what footage was lost.
- `recording.purge` (`purgeExpired`, `purgeCameraNow`) — `purgeCameraNow`'s detail/metadata now also carries the `observations` count (W3-2).
- `recording.config_change` (`saveConfig`) — carries the retention days and enabled flag BEFORE and AFTER the save, since shortening retention is a slower way of deleting footage and only the before/after pair says whether footage was given up.

See `apis/audit.go.md` for the full "what is recorded" table across the app.

## Notes

- `NewRecordingApi` (W3-2) takes an added `observation *services.ObservationService` argument (carried on `recordingApi.observation`), used only by `purgeCameraNow` — see above. `nil` is tolerated (the metadata leg of the purge is silently skipped), the same defensive shape every other optional collaborator on this handler uses.
- `parseFloatQuery` is defined in this file and shared with `apis/observation.go`'s appearance search (`minStandout`) — see `apis/observation.go.md`. Absent or unparseable both return `0` so the caller's own default applies, deliberately the same answer: a threshold typed wrong must fall back to the documented default rather than silently becoming `0`, which on a similarity floor means "return everything".
- `NewRecordingApi` takes a `recorderCfg *services.RecorderConfigBuilder` argument (carried on `recordingApi.recorderCfg`) instead of hand-rolling a `recording.RecorderConfig`. `saveConfig` calls `recorderCfg.ForRecording(ctx, cfg)` and passes the result straight to `recorder.Configure`; the `warning` return becomes `recorderWarning` in the response. See `docs/modules/apps/mymatasan/services/recorder_config.go.md` for what the builder carries (shred passes, cipher, metrics, live decoder/storage settings) and why it exists.
- **The two/three-site `RecorderConfig` duplication trap this file used to document is removed.** `apps/mymatasan/app/app.go` (startup fan-out and the detect-only stream resolver) and this handler's `saveConfig` all now call the same `RecorderConfigBuilder`, so a field can no longer be silently dropped in one site while present in the others — which is exactly how `ShredPasses` was lost here previously (secure shred silently degraded to a plain unlink the moment an operator saved any recording setting). `recordingApi` no longer carries its own `shredPasses`/`metrics` fields; both live on the shared builder now. `cipher` is still carried directly on `recordingApi`, but only for the playback-decrypt path (`GET /api/recording/segments/{id}/download`, `/frame`) — the recorder's own cipher comes from the builder.
- All routes are mounted under the protected subrouter and require local Basic Auth.
- The download endpoint opens the file by path stored in the segment row; if the file has been deleted manually it returns a `400` error.
- `Content-Length` is set from the stored `FileSize` only on the plaintext pass-through path (not when decrypting or transcoding, which stream without a known length). Range responses instead get `Content-Length`/`Content-Range` from `http.ServeContent` against the (possibly materialized) seekable source.
- Materialized playback copies live outside the encryption-at-rest and recording storage roots (`os.TempDir()`), so they are plaintext on disk for up to an hour; this is an accepted trade-off for tunnel/seek playback of otherwise-encrypted footage.
- The at-rest storage codec used when (re)configuring a recorder is read live by the builder (`IRuntimeSettingsService.Recording()`) on each `PUT /api/recording/config`, so a Settings → Recording change applies the next time a camera's config is saved. `RecordFallbackCopy` is passed through the same way, defaulting to enabled when `Storage.FallbackToCopy` is `nil`.
- `parseInt64Query` is a shared helper defined in this file; used by both recording and vision handlers in the same `apis` package.
