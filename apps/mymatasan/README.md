# mymatasan

`mymatasan` is the standalone camera and video intelligence app for `kopiv2`.

It is designed to run on small devices such as Raspberry Pi or Jetson-style micro computers. It discovers ONVIF cameras, persists camera records in a local SQLite database, and exposes live viewing through RTSP-backed streams. Browser live view uses WebRTC for H.264 camera tracks first, with MJPEG fallback retained for compatibility. It communicates with `myseliasan` through the LAN pairing protocol (authenticated UDP multicast discovery + HTTPS adoption).

## Current Scope

- Standalone DB-backed local Basic Auth with a first-run admin account seeded from `localAuth.username`/`localAuth.password` in config (falls back to `admin` / `admin` when unset), **forced password change** on first login regardless of which credential seeded the account, **failed-login lockout** (escalating backoff + countdown), and **three-role access control** — `admin` (full control), `operator` (day-to-day: watch, review footage, acknowledge alerts, PTZ, talk-back — cannot delete/purge or reconfigure), and `viewer` (watch live and see that an alert fired only). See *Security & Access* below.
- **First-run setup wizard** (password → capacity → add camera → recording + alerts) and a **camera-capacity estimator** (`/api/capacity`) that tells you how many cameras the host can handle.
- **Built-in user manual** (Help nav item, contextual `?` help, public `/api/manual`): four-language markdown articles compiled into the binary, reachable even from the sign-in screen and the wizard, with no network access required. See *Built-in user manual* below.
- **Encryption at rest** (default on): recordings, snapshots, and training images are AES-256-GCM encrypted on disk so the factory reset can **crypto-erase** them by destroying the key. The master key itself can be protected by an OS keystore (Windows DPAPI, Linux systemd-creds) or a portable passphrase (Docker), with an exportable/verifiable **recovery escrow** (Settings → Backup & Recovery) and an automatic or pre-login recovery flow if the key is ever lost.
- **Configuration backup & restore** (Settings → Backup & Recovery): export a portable, passphrase-encrypted `.mmbackup` of your cameras (incl. saved credentials), AI detection, notifications, and app settings, then restore it on a fresh install — including a **"Restore from backup"** branch in the first-run wizard — so you never reconfigure by hand. Machine identity (the at-rest key, pairing, certificates) is deliberately excluded.
- **Secure Wipe & Reset** (factory reset: crypto-erase key + erase media + drop/rebuild DB + TRIM/scrub + restart) behind `bootstrap.allowReset`, plus secure multi-pass **shredding** of deleted footage.
- **In-app self-update** (Settings → Version & Health → Updates): checks GitHub Releases on a schedule and on demand, and on portable/Windows-installer installs downloads, SHA-256-verifies, swaps, and restarts in place; `.deb`/`.rpm` and Docker installs get in-panel upgrade guidance instead (`MYMATASAN_MANAGED`).
- **In-app AI runtime installer** (Settings → AI): downloads a self-contained Python + GPU/CPU PyTorch + `ultralytics` for hosts with no usable Python, so the YOLO detector works with no terminal setup.
- **Native Windows service install**: the CI-built Windows installer (`packaging/windows/mymatasan.iss`) registers `mymatasan.exe` as a Windows service (no WinSW/NSSM wrapper needed), controllable from `services.msc`.
- **Resilient background workers**: every long-running background loop (vision monitor, per-camera samplers, camera/machine health monitors, the object-metadata recorder, the RTSP recorder's ffmpeg/segment/purge goroutines, and the retention purge jobs) recovers from a panic and restarts itself instead of taking the whole app down — previously a single malformed AI detection could crash the entire NVR, stopping recording and the API along with it. Rule cooldowns also now survive a restart: a rule's last-triggered time is persisted, so a crash-restart loop can no longer turn one busy scene into an alert/notification/clip-extraction storm.
- **Machine (host) health monitor + disk mitigation** (Settings → Machine Health): CPU/memory/disk usage sampling with debounced warn/critical notifications, an early retention purge at `purgeAtPercent`, and a last-resort action at `pauseRecordingAtPercent` — by default it **pauses** NVR recording until the disk drops below `resumePercent`; enabling **Disk Mitigation → Overwrite oldest** instead keeps recording continuous by deleting the oldest recorded segments across all cameras (bypassing per-camera retention, but never newer than the configurable keep-days floor) each time the threshold is hit, only falling back to pausing if nothing old enough remains to free. The pause/overwrite decision is evaluated against the fullest disk that actually **hosts recording storage** (resolved from each camera's configured `storagePath`), not the globally fullest monitored disk — so an unrelated full OS/DB volume can't trigger a pause or overwrite that would never free the space that's actually full; it falls back to the global worst disk only when no recordings volume can be resolved.
- **Unified notification feed** (topbar bell + Notifications page) across AI detection, camera/machine health, and login security; per-event acknowledge, annotated screenshot, and in-page clip playback.
- **Dashboard analytics**: the landing page aggregates every notification event (AI detections, camera/machine health, login security, system) into KPI tiles plus timeseries/donut/bar charts, backed by `GET /api/notifications/stats` (Go-side aggregation, works across sqlite/postgres/mariadb) and a dependency-free `@shared/charts` SVG module. Range selector (Today/7d/30d) and live refresh off the same bell signal as the notification feed.
- **Dashboard Intelligence** (heatmap, expected-activity band, anomaly alerts, camera reliability, alert noise): an hourly rollup table incrementally aggregated from the notification feed powers an **activity heatmap** (`GET /api/notifications/heatmap` — local day-of-week × hour-of-day grid) and an **expected-activity band** drawn behind the events-over-time chart (`GET /api/notifications/baseline` — robust median ± k·MAD, an 8-week trailing lookback). An **anomaly monitor** (Settings → AI / Dashboard card, opt-in) has two tiers: the default **smart** mode scores each closed hour per camera against its own learned baseline and raises a distinct `analytics.anomaly` notification for a spike or "unusual silence" (a normally-active camera going quiet — the tamper/obstruction/offline signal), tunable sensitivity/consecutive-hour debounce/cooldown; a **manual** mode instead compares the whole system's hourly event total against fixed upper/lower thresholds — no learning period needed, usable from day one. Both are previewable on demand (`GET /api/anomaly/scan`) before ever being enabled. Two further cards need no history at all: a **camera reliability scorecard** (`GET /api/notifications/reliability`, worst-first — per-camera uptime %, offline duration, incident count, currently-offline) derived from the camera-health monitor's own event stream, and an **alert noise ratio** (`GET /api/notifications/noise` — top cameras by AI-alert volume + unread %) to spot mis-tuned rules generating alerts nobody reviews.
- **Object Search** (dedicated Intelligence nav item, cross-camera): a searchable text log of "what objects each camera saw" — presence intervals (label, start/end, peak confidence/count) coalesced from the same AI inference the detection rules already run, so cameras with no rules at all can still log activity for free. Capture is tied to recording itself (Recording tab → **Object sighting cooldown (s)**, no separate on/off toggle — recording on means metadata capture is on). Filter by date range, camera, one or more objects (multi-select), and minimum confidence (`GET /api/observations`, DataTable-style server-side filter/sort/paging, including a multi-value `In` filter for the object picker); each paged result shows a footage screenshot with the detection box drawn on it plus play and maximize overlay buttons, seeking playback to the sighting's clearest (peak-confidence) frame; results export to CSV or PDF. The same footage-screenshot-with-play-overlay treatment also replaced the plain play buttons on the Recordings tab and the Notifications event snapshot. Retention follows the camera's own recording retention. Any person/vehicle row also carries a **Find similar** action — appearance search, a separate opt-in per-camera setting that ranks other sightings by how much they look like the one picked, scored by how far a result stands out from the others compared rather than by a raw match percentage (see *Appearance search* below).
- ONVIF discovery, manual probe, saved-device list, save, camera password change, PTZ move/stop, stream option listing, selected stream URI resolution, RTSP test, WebRTC live view, MJPEG fallback, and delete endpoints under `/api/onvif`.
- **Credential verification + access gate**: adding a discovered camera (or updating its saved credentials) verifies the login against the camera (ONVIF stream-URI resolve and/or RTSP `DESCRIBE`) before persisting — a camera that actively rejects the login is never saved, while an unreachable camera is still allowed through. `GET /api/cameras/{id}/auth-check` re-verifies a saved camera's stored credentials on demand; the camera node's UI blocks all tabs behind a credential-entry gate the moment stored credentials stop authenticating (e.g. after an out-of-band password change on the camera). The gate also offers a two-step-confirm **Remove camera** action for a camera whose new password is unknown, so it doesn't permanently lock the node out of the UI.
- **ONVIF device management**: local user accounts (list/create/delete), reboot, factory default (soft/hard), camera clock (manual or NTP) and network (IPv4/gateway/DNS) configuration under `/api/cameras/{id}/{onvif-users,reboot,factory-default,datetime,network}`. `GET /api/cameras/{id}/capabilities` probes which of these the camera's firmware actually supports so the Settings UI only shows boxes that will work; `GET /api/cameras/{id}/device-info` surfaces manufacturer/model/firmware/serial/MAC/ONVIF version/location for the Live View → Camera Information panel.
- Camera-first AI detection rules and alert events under `/api/vision`, backed by reusable `infra/vision` rule, schedule, motion, external-object, line-crossing, multi-line-crossing, and hybrid detection primitives.
- Line-crossing rules support an **Anything** wildcard class (`"*"`) that triggers on any detected YOLO object regardless of label, in addition to the named class list.
- **Crowd** detection: a rule mode that fires when at least `minCount` people (default 2) are in the zone in a single frame.
- **License-plate recognition (LPR)**: a rule mode (`detectionType: "lpr"`) that runs a second-stage plate detector + OCR on a dedicated high-resolution frame (default 1920 px wide) and fires on any readable plate or only on a watchlist (`matchMode: any/include/exclude`). Fuzzy watchlist matching (Levenshtein ≤ 1) absorbs common single-character OCR errors. Plate text, vehicle type, color, and watchlist flag ride the alert metadata and appear in notification templates (`{{plate}}`, `{{vehicleType}}`, `{{color}}`, `{{watchlisted}}`). Manage the plate-detector model and OCR dependencies from **Settings → AI** or via the `/api/training/lpr-model` endpoints.
- **Face recognition** (People tab, admin-only): enroll named people once in a **global** gallery (`/api/faces`) and recognize them on any camera with a face rule — the detect→embed→match sibling of LPR, running as a stacked stage (not the singleton custom-YOLO slot, so it coexists with a trained model). The People page shows a card per person (a photo count, so an enrolled-but-empty person — recognized by nothing — is visible as such), an enrolment dialog offering an uploaded photo (picker or drag-and-drop) or a live capture via the browser's own camera (`getUserMedia`; needs an HTTPS origin or localhost, a browser rule the dialog states plainly when it applies), and per-photo delete. **The "Enrolled people" roster scales to hundreds of people**: past 12 people the panel grows search, state filter chips with live counts (All / No photos / Few photos / Paused), a sort (name / recently seen / fewest photos), and a list-vs-cards view toggle that defaults by roster size (cards for a short roster, rows past 24) and remembers an explicit choice in `localStorage`; the list view adds sticky A–Z dividers, and either view pages in with a "Show more / Show all" control rather than mounting everyone at once. The per-camera watchlist picker also gained a max-height with scroll for the same reason. During a photo batch upload, the enrolment dialog shows progress ("Uploading photo 2 of 5…") and disables every way out — the X, Close, Esc, the backdrop, the drop zone, the mode toggle, and per-photo delete — plus a `beforeunload` guard, so closing the dialog (or the tab) mid-batch can no longer abandon the rest of the photos silently; an unreadable file in a batch no longer aborts the ones after it. Enrollment (`POST /api/faces/{id}/enroll`) refuses an image with zero, multiple, low-quality, or too-small faces — a bad enrollment silently poisons every future match — and runs an **enrolment scale ladder** (downscale + pad, tried at increasing margins) so a passport photo or a phone portrait is found; a **live scale ladder** (a cheap 640px-wide pass before the native-resolution one) separately fixes the case of somebody standing close to the camera, which the native-only path used to miss entirely. A rule (`detectionType: "face"`, first-class in the camera rules editor with its own panel) fires on any recognized person (`matchMode: known`), a chosen watchlist (`include` + a `people` list), or unrecognized strangers only (`unknown`), gated by a `minConfidence` floor (default 0.4 — the worker's own naming floor, so one decision isn't made twice against two numbers; a site that wants stricter sets `minConfidence` per rule). Recognized identity (`personId`/`personName`/`faceConfidence`) rides the alert metadata and reaches every notification consumer: a `{{person}}`/`{{recognized}}`/`{{faceConfidence}}` custom-field template, a `"• Alice"` (or `"• unknown face"` for a stranger) line appended to the body when the label field is off, and `person`/`personId`/`recognized`/`faceConfidence` in the structured payload every webhook/MQTT/Telegram/email destination receives. The in-app Notifications page likewise shows a **Person** field (and **Match** confidence) on face rows instead of the generic Confidence figure, which was a fabricated 50% floor on an unrecognized face. The box drawn on the alert **snapshot** carries the same identity — the person as enrolled (not title-cased, so "van der Berg" is not rewritten "Van Der Berg"), or "Unknown face" for a stranger — rather than the model's raw "Face" class tag, since the picture is what actually leaves the appliance (notification image, "Download with box", a case export); this applies wherever there is exactly one face box to label (a `MetaBox` in a multi-box crowd-style snapshot carries only a class label and confidence, never a person). The People page itself picks the alert mode per camera (known / watchlist / unknown-strangers) and shows each person's **last seen** camera and time from `GET /api/faces/sightings`, which reduces a bounded window of the alert log rather than keeping a separate tally. **Off by default, admin-only, and dormant** until face recognition is set up (a **Download and set up** panel on the People page and in Settings → AI drives the in-app installer — see below — or `setup.ps1 -Faces`) *and* someone is enrolled *and* a camera has a face rule — a fresh install pays no cost. Uses OpenCV's built-in YuNet (detect) + SFace (128-d embed) — no new heavy dependency beyond `opencv-python`/`numpy`, which LPR already needs.

  > **Biometric data — read before enabling.** Face templates are biometric data under GDPR Art. 9 / BIPA-class regimes: embeddings are encrypted at rest (the same AES-256-GCM envelope as recordings), every `/api/faces` route is admin-only, and deleting a person crypto-shreds their embeddings. The YuNet/SFace **weights** are permissively licensed (YuNet MIT, SFace Apache-2.0) but were trained on research-only face datasets (WIDERFace/CASIA) — an unsettled legal question shared by essentially every mainstream face-recognition model, not unique to this integration; get legal sign-off before deploying in a regulated context. Accuracy drops hard on non-frontal, low-resolution, or poorly-lit CCTV faces — mitigate with a strict `minConfidence`, the built-in confidence margin, and by treating a marginal match as "unknown" rather than a name.
- **Two-axis rule model**: a detection rule is now a **Mode** (presence, crowd, intrusion, line crossing, multi-line crossing, lpr, face) plus a **target class list** chosen from a data-driven **detection class registry** under `/api/vision/classes` (built-in classes + user-defined groups + trained classes) — face and LPR rules instead carry their own mode-specific `ruleConfig` shape (see above) rather than a class list. Legacy single-object rules (`person`/`vehicle`/`animal`/`fire`/`smoke`) still work and open in the new editor.
- **Custom model training** under `/api/training`: build labeled image datasets (upload or import from alert snapshots), draw/correct bounding boxes in-browser, auto-label with the running model, export a YOLO dataset zip, train a custom model (in-app on a GPU, or offline), import a `best.pt`, and **activate** it to hot-swap the live detector and register its classes for rules.
- YOLO Inference Tuning in Settings includes a **Best Calibration** button that applies recommended defaults (conf=0.20, IOU=0.35, imgsz=640, maxDet=100, augment on).
- Alert log with true server-side filtering, sorting, and paging (any `AlertEvent` column — time range, rule, label, confidence, acknowledged status) via the shared `DataTable` grid in server mode; defaults to the latest detections, with a **Today** button for a one-click date-range filter.
- NVR recording under `/api/recording`: RTSP-mode rolling `.ts` segment buffer with event-triggered MP4 clip extraction; tick-mode JPEG ring buffer for low-resource devices. Config hot-reload without restart. Live recorder status endpoint.
- **Recording continuity monitoring** (Settings → Camera health, default on): scores each closed hour of every recording-enabled camera against how much footage actually landed on disk (`GET /api/recording/coverage`, hour/day buckets — the same read model backs the Recordings page's coverage strip) and raises a critical **Recording gap** alert when a camera stops writing footage while still answering reachability checks (a wedged ffmpeg, a full disk, a stalled remux queue). Clears automatically on recovery.
- **Camera tamper detection** (Settings → Camera health, default on): notices a covered/sprayed lens, a camera knocked out of focus or turned away, or a frozen picture — none of which a reachability or continuity check alone catches, since the camera still answers and still writes files. Pure arithmetic over the frame already siphoned for AI detection (no extra decode cost); automatically suppressed in low light so a normal dusk-to-dawn transition never raises a false alarm.
- **Evidence export** (Recordings page, operator and up, `POST /api/evidence/exports`): turn a stated time range for one camera into a single `.zip` — the joined video, a `manifest.json`, and a plain-text `VERIFY.txt` — with a required reason, every gap in the range listed rather than silently skipped, and a labelled digest strength (`recorded` when the SHA-256 was taken at finalize before at-rest encryption, `computed-at-export` for older/recovered segments that predate hashing). Deleting footage stays admin-only; exporting is grantable to operators, and every export is audited at both request and download.
- **Timeline playback** (Workspace nav, beside Live Views; `GET /api/recording/timeline`, `GET /api/recording/timeline/seek`): play recorded footage by the clock instead of by file — a scrub bar (shaded from the same merged spans as the coverage strip) crosses segment boundaries on its own, up to **8 cameras** synchronised against one moment, playback speed **0.25x–8x**, and detection events plotted as clickable jump marks. Scrubbing to a moment with no footage snaps forward to the next covered moment and says how much was skipped rather than guessing. Reuses the existing Recordings page grant (`canRead("/api/recording")`) rather than a new one, so every role that can already browse footage gets it on upgrade; detection marks come from the separate `/api/vision/alerts` grant and are simply hidden when that read is refused. Capped at 8 cameras and a 31-day window per request.
- **Recording compression** (Settings → Recording, default off): shrink footage without hurting performance. Optionally re-encode segments to **H.265/H.264 once on the GPU (NVENC) at remux** (live capture and clips stay stream-copy; a shared NVENC semaphore queues encodes so recording never blocks); browsers that can't decode HEVC get an **on-the-fly HEVC→H.264 playback transcode**. A per-camera **Camera-side quality (ONVIF)** control can instead push H.265 + a bitrate cap to the camera's own encoder (zero host cost; host stream-copies). The capacity estimator accounts for the GPU encode load.
- **Storage-codec GPU fallback** (Settings → Recording storage, "Fall back to Copy if GPU re-encode fails", default **on**): if a re-encode codec (H.264/H.265) is selected but the host has no usable NVIDIA NVENC GPU — or a re-encode fails at runtime — segments are stored as plain stream-copy instead of being dropped, so recording never silently stops on a GPU-less machine. A sticky bottom-right warning appears (deep-linking back to this setting) whenever the configured storage codec can't actually run on the current host. Changing the storage codec itself takes effect the next time a camera's recorder is (re)configured (saving any recording setting, or app restart) rather than requiring a restart — the recorder reads it live instead of the value captured at boot.
- Per-camera split-stream configuration: separate `streamUrl` (recording) and live-view URI with `fallbackStreamUrl` automatic switching after repeated connection failures.
- ONVIF stream profile listing and live-view stream selection under `/api/recording/streams`.
- RTSP stream validation through the reusable `infra/rtsp` module.
- Shared RTSP-to-WebRTC sessions through the reusable `infra/stream` module, with **G.711 pass-through and AAC→Opus audio transcoding** so live view has sound regardless of the camera's audio codec.
- **Two-way audio (talk-back)**: a mic button next to the speaker toggle in live view lets an operator speak through a supported camera's own speaker, over either of two transports resolved server-side (`GET /api/cameras/{id}/talk`, cached 10 min): the standard **ONVIF RTSP audio backchannel** (Hikvision/Dahua/Axis/most Profile-T cameras — reuses the camera's stored ONVIF credentials, no extra password), or the **TP-Link Tapo/VIGI proprietary port-8800 protocol** for consumer cameras that expose no RTSP backchannel at all. The TP-Link path is only ever offered when the camera's port 8800 genuinely fingerprints as TP-Link's "Streamd" talk service (`infra/talk.Probe8800`) — unrelated devices with port 8800 open are never misdetected. When it needs one, the camera's **Access tab** shows a speaker-password field (Tapo: the TP-Link cloud-account password used to log into the Tapo app, not the RTSP password; VIGI: the camera admin password) saved via `POST /api/cameras/{id}/talk/password`; `talkCapability.needsPassword`/`hasPassword` drive when that field appears and whether it's already set. `POST /api/cameras/{id}/talk/offer` negotiates the browser→camera WebRTC session (sendonly PCMA mic track, via the reusable `infra/talk` module) that pumps the mic audio into the resolved transport. Cameras without a usable RTSP stream, ONVIF backchannel, or TP-Link talk service simply don't show the mic button.
- **AI capture modes** (`standalone` / `siphon` / `auto`): `siphon`/`auto` read decoded frames off the recorder (the recording stream) via a tee; the rule-editor preview draws on the exact frame the detector samples (`GET /api/vision/cameras/{id}/frame`).
- **Live Views paging**: the grid (1×1 / 2×2 / 3×2 / 3×3 / 4×3 / 4×4) is a per-page size, not a cap — all selected cameras are kept and paged through (toolbar pager + arrow keys, works in fullscreen).
- Shared public version and Swagger APIs from the shared app host.
- Runtime Decoder, Live Stream, and local user management settings backed by SQLite.
- Default cache provider is in-process memory.
- Default DB engine is SQLite at `apps/mymatasan/data/mymatasan.db`.
- MyIDSan JWT auth, SSO, RBAC, user, role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, and cache-service APIs are not mounted in `mymatasan`.
- **LAN pairing + mTLS management** (`/api/pairing`): secure single-parent adoption by a `myseliasan` control plane. An operator sets a shared fleet key, generates a short-lived claim code in Settings → Connectivity, and the control plane performs an authenticated LAN scan + adopt. Once adopted the node immediately enrolls for an ECDSA P-256 certificate from the control-plane fleet CA (the node generates the key locally; only a CSR is sent), and serves a mutual-TLS management listener on `pairing.mtlsPort` (code fallback 49532; the shipped `config.json` sets 39532) that the control plane uses for heartbeat probes and remote release. Certificates are renewed automatically before `pairing.renewBeforeHours` (default 48 h) of expiry. Once adopted, the node is invisible to further discovery probes. Admin self-drop (`POST /api/pairing/unpair`) or a control-plane release undoes the binding and tears down the mTLS listener.
- **Node-dialed media channel**: after enrollment, `mymatasan` dials `myseliasan`'s media listener (`pairing.mediaPort`, code fallback 49534; shipped `config.json` sets 39534) over fleet mTLS and maintains a persistent media connection (`MediaChannelManager`). When the control plane requests a camera stream, the node subscribes the camera's RTSP session, forwards codec metadata, replays the GOP backlog, and pumps live RTP to the parent. The parent re-broadcasts this as WebRTC to the requesting browser. The media channel reconnects with backoff if the connection drops; the control plane re-sends stream requests when the node reconnects.
- App-specific OpenAPI descriptions for ONVIF, RTSP setup, live view, vision, and recording endpoints.

## Run

From repository root:

```bash
go run . -app mymatasan
```

Default dev listener:

```text
http://localhost:3000
```

Default local credentials come from the `localAuth` block in `config.json` (`localAuth.username` / `localAuth.password`; an env `LOCAL_ADMIN_PASSWORD` overrides the password). When left empty, the fallback is:

```text
username: admin
password: admin
```

The seeded account is always forced to change its password on first login. Change credentials from Settings before deploying outside a trusted local development network.

Browser live view uses WebRTC directly from RTSP H.264 RTP packets and does not require an ffmpeg executable for the primary path. MJPEG fallback still uses the configured decoder ffmpeg path. `config.json` provides startup defaults; after first startup, the Settings page persists runtime values in SQLite and changes apply without restart.

WebRTC live view requires the selected camera RTSP stream to expose an H.264 video track. Some camera main streams, including common VIGI profiles, may be configured as H.265/HEVC. When the RTSP test sees video tracks but none are H.264, the browser live view skips WebRTC and uses MJPEG fallback if it is enabled. To use WebRTC for that camera, change the camera stream codec to H.264 or select an H.264 substream from the Stream tab.

```json
"decoder": {
  "browseRoots": [],
  "mjpeg": {
    "ffmpegPath": "ffmpeg",
    "quality": 7,
    "threads": 1
  },
  "ffmpeg": {
    "rtspTransport": "tcp",
    "hwaccel": "none",
    "hwaccelDevice": "",
    "initHwDevice": "",
    "videoDecoder": "",
    "probeSize": 1000000,
    "analyzeDuration": 1000000,
    "lowDelay": true,
    "noBuffer": true
  }
},
"stream": {
  "webrtc": {
    "enabled": true,
    "iceServers": []
  },
  "mjpegFallback": {
    "enabled": true
  }
}
```

Use an absolute path when the service process cannot find ffmpeg from `PATH`, for example `C:\\ffmpeg\\bin\\ffmpeg.exe` on Windows or `/usr/bin/ffmpeg` on Linux. The decoder runtime settings expose conservative ffmpeg tuning knobs: RTSP transport, hardware decode mode, GPU/device selection, optional decoder name, probing/analyze limits, low-latency flags, MJPEG quality, and thread count. Hardware modes such as `vaapi`, `cuda`, `qsv`, `d3d11va`, and `videotoolbox` require a matching ffmpeg build, driver, and camera codec support; leave `hwaccel` as `none` for CPU software decode.

The **FFmpeg path** field in Settings → Runtime → Decoder shows a live status icon (green tick / red cross, with a hover tooltip giving the detected version+path or why it is missing) and a **Check** button (`GET /api/settings/decoder/status`). When ffmpeg is not found, a **Download ffmpeg** button runs the same in-app installer the first-run wizard uses (`POST /api/settings/decoder/ffmpeg/install`, polled via `/install/status`) and a **Restart now** button applies it. A **folder icon inside the input** opens a server-side file picker (`GET /api/settings/fs/browse`) so you can pick the binary instead of typing the path. The picker is **admin-only, read-only**, and sandboxed to a whitelist of roots: the app directory + `bin/`, the user home, OS-specific common install locations (Windows `ProgramFiles`/`ProgramData`/`LOCALAPPDATA`/`C:\\ffmpeg`; macOS Homebrew/MacPorts/`Applications`; Linux `/usr/bin`, `/usr/local/bin`, `/opt`, `/snap/bin`, `/bin`), plus any paths you add to the `decoder.browseRoots` config array. You can still type any absolute path manually.

The **GPU/device** field in the Settings UI shows a dropdown populated from the detected device list. Choose a device from the list or select **Manual entry…** to type a custom value (useful for non-standard ffmpeg device identifiers). Clicking **Auto Tune** sets the GPU/device automatically based on detected hardware.

A **Version & Health** tab in Settings surfaces the running app/shared-core version (and commit/build) from `GET /api/version` plus live liveness/readiness/API-namespace health from `GET /health`, `GET /ready`, and `GET /api/health`, with a **Restart app** button that relaunches the process and reloads once it is back. The same tab includes an **Updates** panel (`GET /api/system/update`, `POST /api/system/update/check`) that shows whether a newer release is available; on portable-archive or Windows-installer installs, an **Update to vX.Y.Z** button (`POST /api/system/update/apply`) downloads, checksum-verifies, swaps, and restarts in place — see **Self-Update** below. Status messages across the app appear as **top-right toasts** that auto-dismiss after a few seconds and stack.

Local users are stored in SQLite with bcrypt password hashes. The Settings page provides user create, update, password reset, and delete actions. The app prevents deleting or disabling the last active admin user.

Example STUN/TURN configuration:

```json
"stream": {
  "webrtc": {
    "enabled": true,
    "iceServers": [
      { "urls": ["stun:stun.example.com:3478"] },
      {
        "urls": ["turn:turn.example.com:3478?transport=udp"],
        "username": "mymatasan",
        "credential": "change-me"
      }
    ]
  },
  "mjpegFallback": {
    "enabled": true
  }
}
```

To force MJPEG-only live view:

```json
"stream": {
  "webrtc": {
    "enabled": false,
    "iceServers": []
  },
  "mjpegFallback": {
    "enabled": true
  }
}
```

Vision monitoring is configured from the startup `vision` block. `motion` mode is dependency-free and preserves the original consecutive-frame detector. It also provides a native motion-centroid fallback for line-crossing rules when an external AI tool is unavailable. `external` mode starts one detector command per sampled frame. `hybrid` mode combines external object detection with motion fallback for configured rule types such as intrusion. `persistent` mode keeps one detector worker process alive, which is the recommended YOLO path because the model loads once. If the configured AI command is missing and `useMotionFallback` is enabled, MyMataSan starts with native motion fallback instead of failing the app.

```json
"vision": {
  "enabled": true,
  "intervalMs": 2000,
  "captureTimeoutMs": 12000,
  "diagnosticCooldownSeconds": 30,
  "detector": {
    "mode": "persistent",
    "command": "python",
    "args": ["./apps/mymatasan/ai/yolo_worker.py"],
    "timeoutMs": 8000,
    "useMotionFallback": true,
    "useMotionIntrusion": true,
    "minObjectConfidence": 0.25,
    "classMap": {
      "fire": ["fire"],
      "smoke": ["smoke"],
      "person": ["person"],
      "vehicle": ["vehicle", "car", "truck", "bus", "motorcycle", "bicycle"],
      "animal": ["animal", "bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra", "giraffe", "mouse", "rat"],
      "intrusion": ["person", "vehicle", "car", "truck", "bus", "motorcycle", "bicycle"],
      "line_crossing": ["person", "vehicle", "car", "truck", "bus", "motorcycle", "bicycle"],
      "multi_line_crossing": ["person", "vehicle", "car", "truck", "bus", "motorcycle", "bicycle"]
    }
  },
  "training": {
    "dataDir": ""
  }
}
```

`vision.training.dataDir` is the on-disk root for custom-model training datasets, exported YOLO datasets, and trained model weights. When empty it defaults to a `training` sibling of `snapshotDir` (so all AI artifacts live under the volume the machine-health monitor watches). The `classMap` above seeds the built-in detection classes on first run; thereafter the class registry (`/api/vision/classes`) is the source of truth and is editable in the UI. See **Custom Model Training API** below.

**Easiest (GPU hosts):** on a machine with an NVIDIA GPU, open **Models → Train in-app** and click **Install GPU support**. The app verifies the GPU is CUDA-capable, pauses detection (so PyTorch's files aren't locked), installs the CUDA PyTorch build into its own Python, and shows a live log — no terminal needed. Restart the server when it finishes.

Otherwise, install the YOLO worker dependencies in the Python environment used by the app. The bundled setup script auto-detects an NVIDIA GPU and installs the matching PyTorch build (CUDA when a GPU is present, otherwise CPU) plus ultralytics and OpenCV:

```bash
# Windows (PowerShell)
powershell -ExecutionPolicy Bypass -File apps/mymatasan/ai/setup.ps1
# Linux / macOS / Raspberry Pi
apps/mymatasan/ai/setup.sh
```

Run it with the **same Python** the app launches the detector with (`vision.detector.command`, usually `python`). Pass an explicit interpreter or CUDA wheel tag if needed: `setup.ps1 -Python C:\path\python.exe -Cuda cu121` / `setup.sh /usr/bin/python3 cu121`.

To also install the LPR OCR dependencies (`easyocr`, `opencv-python`, `numpy`) in one pass, add the `-Lpr` flag (PowerShell) or `--lpr` flag (shell):

```bash
powershell -ExecutionPolicy Bypass -File apps/mymatasan/ai/setup.ps1 -Lpr
apps/mymatasan/ai/setup.sh --lpr
```

Or install them separately (CPU-only, no GPU required):

```bash
python -m pip install -r apps/mymatasan/ai/requirements-lpr.txt
```

To also install face-recognition dependencies (`opencv-python`, `numpy` — no new heavy dependency beyond what LPR already needs) and download the two permissively-licensed model files (YuNet MIT, SFace Apache-2.0, from `opencv_zoo`), add the `-Faces` flag — **Windows (`setup.ps1`) only for now**; `setup.sh` has no `--faces` equivalent yet, so on Linux/macOS/Raspberry Pi install `requirements-face.txt` and download the two `.onnx` files from `opencv_zoo` by hand. On a machine with outbound access to `github.com`, the same install also has an in-app route: a **Download and set up** panel on the People page (and in Settings → AI) fetches only whatever is missing, pip-installs opencv when the detector's interpreter lacks the face classes, and verifies each file loads before reporting ready (`GET/POST /api/faces/models*`) — the button an operator standing in the browser actually has, versus a PowerShell script named in an error message. An air-gapped appliance still needs the script (or a manual fetch) on a connected machine and a file copy.

```bash
powershell -ExecutionPolicy Bypass -File apps/mymatasan/ai/setup.ps1 -Faces
```

Or install the Python side separately (the model downloads still need `-Faces` or a manual fetch):

```bash
python -m pip install -r apps/mymatasan/ai/requirements-face.txt
```

Or install core YOLO deps only:

```bash
python -m pip install -r apps/mymatasan/ai/requirements-yolo.txt
```

> In-app training (Models → Train) needs the **CUDA build** of PyTorch to use a GPU. The default `pip install torch` (and what ultralytics pulls) is often the CPU-only build (`+cpu`), which can't use the GPU — the setup script installs the CUDA build automatically when a GPU is detected. The Train-in-app panel reports the detected PyTorch/CUDA state.

MyMataSan keeps ML runtime files under `apps/mymatasan/ai` to avoid confusing them with Go/domain object models such as `domain/models`.

<a id="api-credentials"></a>
> **The `curl` examples below use `$ADMIN_PW`.** There is no shipped default password — a fresh
> install generates one, prints it in the startup banner and writes it to
> `INITIAL_ADMIN_LOGIN.txt` in the data directory. Export your admin password before running any
> of them:
>
> ```bash
> export ADMIN_PW='the-password-you-set'
> ```
>
> (The examples previously hard-coded `Admin123`, which no new install has ever had. See the
> built-in manual's *Signing in for the first time* for the full first-run flow.)

Check whether the configured AI tool is ready without downloading anything:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/settings/vision/ai-tool/status"
```

The Settings page exposes the same check. It reports the resolved command path, Python package readiness, worker script, model file, and whether native fallback is available. Users can skip AI downloads; semantic rules such as person, vehicle, animal, fire, and smoke will not produce object-label detections without an AI worker, while native motion and motion-based line crossing can still run.

**No Python at all?** Settings → AI also offers an **Install AI runtime** button (`GET`/`POST /api/settings/vision/ai-runtime/status,install,install/status`) that downloads a self-contained Python (no system install needed), pip-installs a GPU (CUDA) or CPU PyTorch build plus `ultralytics` depending on whether an NVIDIA GPU is detected, and points `vision.detector.command` at it — a no-terminal path to a working detector on a bare host. The download can be 200 MB–2.5 GB depending on GPU/CPU. This is separate from the **Install GPU support** button in Models → Train in-app (which instead swaps an existing Python installation for a CUDA-capable one for training); restart the app after either finishes.

The bundled worker defaults to `yolo11n.pt`, which can detect COCO labels such as `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`. Fire, smoke, mouse, rat, and other non-COCO labels need a custom YOLO model trained with those labels; set `MYMATASAN_YOLO_MODEL` to that model path before starting the app. For CCTV or IR scenes, start person, vehicle, and animal rules around `threshold: 0.35` and `minFrames: 2`, then tune upward if false positives are too noisy.

Persistent worker stdout can be either an array or an object with `detections` or `objects`. Candidate boxes are normalized from `0` to `1`:

```json
[
  {"label":"person","confidence":0.91,"box":{"x":0.2,"y":0.1,"w":0.3,"h":0.5}}
]
```

## Packaged Releases

Besides `go run . -app mymatasan` from a source checkout, GoReleaser (`.goreleaser.yaml`) builds cross-platform release artifacts in one run: linux/windows × amd64/arm64 archives, `.deb`/`.rpm` packages (install to `/opt/mymatasan` with a bundled systemd unit and dedicated `mymatasan` service user), and multi-arch Docker images (`ghcr.io/mysayasan/mymatasan`). All package types resolve a read-only app **home** directory (static assets, AI scripts, default config) separately from a writable **data** directory (mutable config, database, recordings, logs, encryption key) via `MYMATASAN_HOME`/`MYMATASAN_DATA` (or generic `KOPIV2_HOME`/`KOPIV2_DATA`) — a source checkout keeps both pointed at the same directory, unchanged. If HTTPS is enabled and no certificate exists yet, a self-signed one is generated on first boot so a fresh install serves HTTPS immediately. See `deploy/README.md` for install/TLS/reverse-proxy details.

## UI

The browser UI is a React 18 single-page application bundled with Webpack 5 into `static/59.js`.

### Multi-language UI (i18n)

The UI is fully localized into **English**, **Malay (Bahasa Melayu)**, **Chinese Simplified**, and **Arabic (العربية)**. Arabic is a right-to-left (RTL) locale; selecting it sets `<html dir="rtl">` so the entire layout mirrors automatically. A language switcher (`LanguageDropdown` from `@shared`) appears in the top bar as an inline row of buttons (`English | Melayu | 中文 | العربية`). The active language is persisted to `localStorage`. App-specific translations live one-per-locale under `views/react-webpack/src/views/i18n/` (`en.js`/`ms.js`/`zh.js`/`ar.js`); `views/react-webpack/src/views/i18n.js` is a thin loader that ships English eagerly (it is every key's fallback and must be present on first paint) and dynamically imports the other three as separate Webpack chunks (`i18n-ms`/`i18n-zh`/`i18n-ar`) only when selected, so a single-language user never downloads translations they don't use. A returning user whose saved language isn't English briefly gates first paint while that locale's chunk loads (English users never wait); switching language likewise loads the chunk before applying it, so the UI never flashes English. Translations layer over the shared base dictionary (`frontend/shared/src/i18n/index.js`) via `LangProvider`/`useT()`. The shared provider also maintains `RTL_LANGS = ['ar']` and sets `<html lang/dir>` on every locale change. Keys missing from a locale fall back to English, then to the key itself, so no render path can crash from a missing translation.

### Shared footer

An `AppFooter` component (`@shared`) renders at the bottom of the app shell, showing the app name, app version, shared-core version, short commit hash, and build date (fetched from `/api/version`), plus the r450k product tagline. All version fields are optional; if `/api/version` is unreachable, only the app name and tagline are shown.

### Camera page header

The Cameras detail page (which previously dropped straight into the tab bar with no header at all) now renders `@shared/CameraHero` above the tabs: a breadcrumb trail (`Cameras > <camera name>`, the root crumb routing back through the side-nav's own camera-root selection so the rail highlight follows), a status-tinted camera tile with a reachability dot, the camera name + description, and health/stream status chips. This is the same shared header myseliasan's Nodes → Camera page renders, so a camera reads identically whether you open it in mymatasan directly or through the myseliasan control plane.

### Theming

A **Theme** dropdown in the top bar lets you switch between **Light**, **Dark**, and **Slate** themes. The selection is persisted in `localStorage` and applied via a CSS custom-property theme class on `<html>`. Additional themes can be added by extending the `THEMES`, `THEME_LABELS`, and `THEME_ICONS` constants at the top of `App.js`.

### Form UX standard

All forms with a save action follow a consistent UX pattern:

- **Save button disabled** when there are no unsaved changes. The button enables as soon as a field is edited and disables again after a successful save.
- **Discard Changes button** appears alongside Save; clicking it reverts all unsaved edits to the last saved/loaded state without an API call.
- **Loading overlay** — a centred spinner covers the form while a save request is in flight, preventing double-submission and giving clear feedback.

This pattern applies to: Runtime Settings, Camera Details, Camera Credentials, Login, and Vision Rule forms. Future forms should follow the same pattern by using the `FormBusyOverlay` component and tracking a `savedState` alongside the draft state.

### YOLO Inference Tuning

The Settings → YOLO Inference Tuning section includes a **Best Calibration** button that applies a recommended starting configuration:

| Field | Value | Reason |
|---|---|---|
| Confidence | 0.20 | Catches back-facing / crouching persons without excessive false positives |
| IOU threshold | 0.35 | Keeps overlapping boxes for partially-occluded subjects |
| Image size | 640 | Standard YOLO resolution — good accuracy/speed balance |
| Max detections | 100 | Reasonable cap for typical scenes |
| Augment (TTA) | on | Biggest single accuracy boost for hard-to-detect poses |
| Half precision | off | No benefit on CPU; safe to enable on CUDA GPU |

## Security & Access

MyMataSan is an internet-of-things appliance, so the auth defaults are hardened:

- **Forced first-login password change.** There is no shipped default password: on a fresh install the seeded `admin` gets a password GENERATED for that install, printed in the startup banner and written to `INITIAL_ADMIN_LOGIN.txt` in the data dir. It — and an existing admin still on the legacy `Admin123` — is flagged `must change password`. Until it is changed, the user is gated to `GET /api/auth/session` and `POST /api/auth/change-password` (everything else returns `403 password_change_required`). Set `LOCAL_ADMIN_PASSWORD` before first run to provision a strong password and skip the prompt.
- **Failed-login lockout.** After `loginSecurity.maxAttempts` failed sign-ins from one IP within `windowSeconds`, that IP is locked with an escalating (doubling) backoff up to `lockoutMaxSeconds`; locked requests return `429` + `Retry-After`, the login page shows a countdown, and a lockout trips a Critical notification. The lockout counts against the sign-in surface: `POST /api/auth/login`, the endpoint the SPA now signs in through, and — for Basic-only clients — the interactive login probe `GET /auth/session`. A wrong credential on any other protected route is denied but never consumes the budget, so a client replaying a stale credential across a page load can't self-lock a legitimate user. Tunables live in the `loginSecurity` config block.
- **Cookie-session sign-in.** The SPA exchanges the credential once (`POST /api/auth/login`) for an HttpOnly session cookie instead of holding the password in memory and replaying HTTP Basic on every request — a page reload now restores the session (via a `GET /api/auth/session` probe on boot) instead of signing the user out, and requests no longer each pay a bcrypt verification. `POST /api/auth/logout` clears the cookie server-side. The session cookie also slides: a request past half its 12h lifetime re-issues it, so an active session is not dropped mid-shift by a clock that started at sign-in. HTTP Basic still works for scripts and API clients (see the `curl` examples below) and still backs `<img>`/`<video>` media tiles.
- **"Magic word" easter egg (cosmetic only).** On the 3rd consecutive failed sign-in attempt (client-side counter, resets on success), the login page shows a full-screen green-CRT overlay with a wagging cartoon "big head" that speaks "Ah, ah, ah… You didn't say the magic word!" via the Web Speech API (a Jurassic Park/Dennis Nedry homage). It dismisses on click, `Escape`, or after ~6.5s. Purely presentational — it never affects the real lockout counter/backoff above and adds no server-side state.
- **Role-based access — three roles.** Every request (not just writes) is checked against the signed-in user's role permission matrix, deny-by-default and enforced server-side:

  | Role | Can | Cannot |
  |---|---|---|
  | **viewer** | Watch Live Views. See that an alert fired (Notifications feed) and that AI rules exist. Change their own password. | Open recorded footage or object-observation search. Acknowledge alerts, PTZ, talk-back. Anything under Settings, AI rule edits, onvif/training/teach, system actions. |
  | **operator** | Everything a viewer can, **plus**: play back and download recorded footage, **export a time range as a verifiable evidence bundle**, search recorded object observations, acknowledge alerts, PTZ, talk through a camera's speaker. | **Delete or purge anything** (recordings, alerts, cameras) — this is the line that makes the NVR evidentiary: an operator who was present at an incident cannot destroy the footage of it, though they can hand a copy of it to somebody. Also cannot change AI rules, notification/machine/decoder settings, add/remove/reconfigure cameras, or manage users/roles, and cannot read the audit trail. |
  | **admin** | Everything, including user/role management, deleting/purging footage, system reset, self-update, and reading the audit trail. | — |

  New users are created as **operator** by default via Settings → Users (assign a different role there); an admin can move any account to `viewer` for a stricter, watch-only account. **Existing installs upgrading into this model:** every account without a role yet is auto-assigned one on first boot — existing admins stay admin, and every other existing local user becomes **operator** (not viewer), because operator is what preserves what a non-admin could already do (review footage, acknowledge alerts) with no loss of access; they additionally gain PTZ and talk-back. No config change or manual step is required.

  A permission a later release adds to this matrix reaches an already-running install automatically: on boot, `viewer`/`operator` are backfilled with any catalog row they don't yet have, without touching a row a site has tuned by hand via the permission matrix editor. (This closed a real gap: a release once shipped a corrected rule — `viewer`/`operator` could not even sign in, because the catalog had no rule for the session-probe endpoint the app calls first — that an already-seeded install would otherwise never have received.)
- **Audit trail** (admin-only, read-only, `GET /api/audit` / `GET /api/audit.csv`): every credential change, footage view/download/delete/purge, retention change, evidence export, and user create/update/delete is recorded — who did it, from where, with what client, and, for a deletion, which camera and time range were lost. There is deliberately no delete or update route: a trail the recorded party can edit is not a trail. mymatasan was previously the only app in the suite with no audit log, despite being the one holding the actual video; the implementation is shared with `myidsan` and `myseliasan` (`domain/shared/audit`). The read screen is not built yet — the API and the permission grant exist, CSV export works today via `curl`/a browser.
- **JWT secret hardening (shared host).** On startup an empty/placeholder/too-short `jwt.secret` is auto-replaced with a generated 32-byte secret written back into the config file (or supply `JWT_SECRET`). The mymatasan session cookie is sha256-based, not JWT, so rotating the secret does not log local users out.
- **Hardened response headers.** Every response — including auth 401s, rate-limit 429s, static assets, and the setup page — gets `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: strict-origin-when-cross-origin`, `Strict-Transport-Security` over TLS, and never advertises a `Server` header/version. Tunable via the `securityHeaders` config block. mymatasan additionally ships a tested opt-in `Content-Security-Policy` (`securityHeaders.contentSecurityPolicy` in `config.json`); the front-end also self-hosts its Quicksand font (`assets/fonts.css`) instead of loading it from Google Fonts, so there is no cross-origin font request for the CSP to allow.

```bash
# Who am I + must-change/role flags
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/auth/session"
# Change your own password (clears the must-change flag, rotates the session cookie)
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"currentPassword":"'"$ADMIN_PW"'","newPassword":"a-strong-passphrase"}' \
  "http://localhost:3000/api/auth/change-password"
```

## Setup wizard & camera capacity

On a fresh install an admin is walked through a **setup wizard** (Welcome/password → System check → AI → Capacity → Add camera → Recording → Alerts → Done), persisted via `GET /api/setup/state` and `POST /api/setup/complete`; it shows until completed or skipped and auto-tiles the added cameras on finish. The **Add camera** step lets you rename a discovered camera (editable name, defaulting to the discovered title) before adding it, and the **Alerts** step can attach a delivery destination — **Webhook, Telegram, or MQTT** (broker URL + topic; advanced auth/TLS is configured later in Settings → Notifications). The **System check** step also offers the in-app ffmpeg installer when no video engine is found. The **Welcome** step additionally offers **"Restore from backup"** — upload a `.mmbackup` from a previous install and its passphrase to adopt that whole configuration, then restart so the running services (recording, vision, notifications) pick it up. A successful restore also marks first-run setup complete, so the wizard does not reappear after the restart — the restored machine is treated as already configured. See *Configuration backup & restore* above.

The **camera-capacity estimator** answers "how many cameras can this host handle?" It models AI inference (CPU/GPU), memory, and live-view decode, reports the limiting resource, and treats recording as a rolling buffer: rather than zeroing the camera count on a small disk (footage auto-purges), it caps cameras at a **~1-day minimum-retention floor** and reports the retention actually achievable at the recommended count — balancing cameras against retention. The AI figure sharpens through three tiers — a static spec-sheet model, a live extrapolation from real CPU load once cameras run, and a **calibrated** figure from a real detector benchmark:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/capacity"            # estimate
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/capacity/calibrate"  # benchmark the detector (best run idle)
```

The estimate and a **Run calibration** button are surfaced on the Settings → Machine Health card (and reused by the wizard).

## Deployment shape (single instance by design)

mymatasan runs as **one instance**, and Settings → System says so rather than leaving an
operator hunting for a setting that does not exist. It owns its camera capture pipelines,
writes recordings to local disk and pins detection to this host's GPU, so a second copy
would not share that work — it would open its own streams against the same cameras and
write a second, divergent set of recordings. Availability here is a redundancy question (a
spare recorder, redundant storage, regular backups), not a load-balancer one.

The notice is the shared `DeploymentPanel` (`frontend/shared/src/Deployment.js`) rendering
its appliance branch, driven by `GET /api/deployment/preflight`, which reports
`clusterable: false` with the reason code `local-media`. There is deliberately no
`POST /api/deployment/mode` on this app — the mode is not a choice. See
`docs/HOWTO.md` → *Which apps can actually be clustered* for the suite-wide picture;
`myseliasan` and `myidsan` are the two apps that can genuinely run behind a load balancer.

## Built-in user manual

mymatasan ships a **built-in user manual** — markdown articles compiled into the binary, so the docs a reader sees always match the running software and work with no network access (phase 1 of a suite-wide manual; other apps adopt the same shared library later). Content lives under `apps/mymatasan/manual/{en,ms,zh,ar}/*.md` (`apps/mymatasan/manual/manual.go`); indexing, language fallback, search, and printing are generalized in `domain/shared/manual`, reused by any future app that embeds its own articles the same way.

The manual covers the whole app end to end — **36 articles** per language across eight sections: **Getting started** (welcome, first sign-in, the setup wizard, restoring from backup, workspace tour, using this manual), **Daily use** (dashboard, live views, notifications, recordings, object search, people), **Cameras** (adding cameras, camera properties, camera health, ONVIF management), **Detection** (how detection works, detection rules, object classes, teach mode, fire/smoke/plate recognition, training models), **Recording** (recording configuration, storage and capacity), **Notifications** (notification destinations), **Administration** (users and roles, settings reference, encryption at rest, backup and restore, updates and restart, secure wipe and reset, control plane, machine health), and an **Appendix** (troubleshooting, FAQ, glossary).

Served publicly (no auth) under `/api/manual`, deliberately mounted before the auth middleware and left off the RBAC permission matrix — the sign-in screen and the first-run wizard are exactly where a reader most needs help and least can authenticate:

```bash
curl "http://localhost:3000/api/manual?lang=ms"          # article index (metadata only)
curl "http://localhost:3000/api/manual/bundle?lang=ms"   # whole book, bodies included (client search/print)
curl "http://localhost:3000/api/manual/welcome"          # one article by slug (falls back to English lang if untranslated)
curl "http://localhost:3000/api/manual/assets/<name>"    # a figure
```

In the UI, a **Help** nav entry (every role, including viewer) opens the full manual on its own page, and the sign-in / change-password / recovery-gate screens plus every step of the first-run wizard carry their own help links — all backed by the shared `@shared/Manual` module (`frontend/shared/README.md`). Beyond that, a `HelpButton` ("?") is wired into the specific place each topic applies rather than only into a generic workspace header: every Settings tab (Runtime, AI, Notifications, Camera Health, Machine Health, Users, Connectivity, Backup, System — via a `help` slug/anchor pair on `SETTINGS_TABS`), every camera settings tab (Details, Access, Stream, Recording, ONVIF), the AI Detection rules header, the People page and its consent gate (pointed at the consent section specifically, since that is what an operator enrolling someone needs to have read), the Recordings header, the Object Search page, the dashboard's anomaly panel, and the Notifications header — each deep-linking straight to the section of the manual that explains it. The top-level `Help` nav's own tab-scoped "?" (`TAB_HELP` in `components/layout.js`) and the setup wizard's per-step help (`STEP_HELP` in `components/setup.js`) point at the same real articles. "Print the whole manual" renders every article with a table of contents and calls the browser print dialog, so Save-as-PDF works in every language (a server-side PDF could not: `domain/report`'s pure-Go writer is cp1252-only).

New articles must land in **all four** language folders (`en`/`ms`/`zh`/`ar`) — `apps/mymatasan/manual/manual_test.go` enforces language parity, cross-link resolution, and heading-anchor consistency via `domain/shared/manual/manualcheck`, and fails the build otherwise. A second test, `TestManualUIReferences`, checks the opposite direction: it scans the SPA's frontend source for every `HelpButton`/`help:`/`TAB_HELP`/`STEP_HELP` target (currently 42) and fails if any names an article slug or `{#anchor}` heading id that doesn't actually exist — the failure mode a compiler can't catch, since a stale deep link just opens the wrong page instead of erroring.

## Encryption at rest

Recordings, snapshots/alert images, and training dataset images are **encrypted on disk** by default (AES-256-GCM chunked streaming, reusable `infra/atrest` module) with a master key stored outside the media roots. This makes the factory reset's wipe **guaranteed and device-independent**: destroying the key (**crypto-erase**) renders all ciphertext instantly unrecoverable regardless of size or storage medium, which plain overwrite cannot promise on SSD/NVMe. Toggle via the `security` config block (`encryptAtRest`, default `true`; `keyPath`, default `secret/atrest.key`). With encryption off, data is written as plaintext; pre-existing plaintext files are always read transparently (magic-detect passthrough), so the setting flips cleanly with no migration. Model `.pt` weights stay plaintext (the Python worker reads them directly).

### Key protection & recovery

The master key on disk (the DEK) can itself be wrapped by a `security.keyProtector`:

- `file` (default, backward-compatible) — the bare key, plaintext.
- `auto` — platform default: Windows DPAPI, or Linux `systemd-creds` when available (TPM2-backed when present); falls back to `file` otherwise (e.g. containers without `systemd-creds` — set `passphrase` explicitly for Docker).
- `dpapi` / `systemd-creds` — OS-native keystore, **machine-scoped/host-bound**: unwraps with no operator input on this host, but the key file cannot be moved to another machine.
- `passphrase` — Argon2id-derived key-encryption key from `security.passphrase`/`passphraseFile`/`passphraseEnv` (or `$ATREST_PASSPHRASE`); portable across hosts, the right choice for Docker/portable installs.

Switching `keyProtector` re-wraps the same key on the next boot (a lossless migration) — existing encrypted recordings, snapshots, and training images stay readable either way.

Because a host-bound key can't be unwrapped after a hardware failure, reimage, or move, **Settings → Backup & Recovery** lets an admin export a passphrase-protected **recovery escrow** (a `.atrestkey` file) of the current key and later verify a saved copy still works and still matches the active key:

```bash
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"passphrase":"a strong passphrase"}' \
  "http://localhost:3000/api/system/recovery/export"   # downloads mymatasan-recovery-YYYYMMDD.atrestkey (base64 in the response)
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"passphrase":"a strong passphrase","keyBase64":"<file contents, base64>"}' \
  "http://localhost:3000/api/system/recovery/verify"
```

A non-secret **init marker** written beside the key on first use lets a later boot tell "key missing because this is a genuinely new install" apart from "key missing but data encrypted with it still exists" — the app never silently mints a replacement key in the latter case. If the key ever goes missing on a host that had one:

1. If `security.recoveryPath` (default `recovery.atrestkey` beside the key) points at an escrow file and a passphrase is configured (`security.passphrase`/`passphraseFile`/`passphraseEnv`), the app restores the key from it automatically on boot — no prompt, normal startup. See `apps/mymatasan/config.sample.recovery.json` for a ready-to-merge `security` block.
2. Otherwise the app boots into a **public, pre-login recovery gate**: no camera/vision/recording services start, and the browser can reach nothing else until an operator uploads the exported escrow file + its passphrase, at which point the key is restored and the process restarts into normal operation.

### Configuration backup & restore

Distinct from the recovery escrow above (which protects the encryption key), the **Configuration backup** panel on the same Settings → Backup & Recovery tab exports your *settings* so a new machine can be brought up without reconfiguring. Pick any of four sections — **Cameras** (camera + ONVIF rows incl. stored credentials + recording configs), **AI detection** (rules + the detection-class registry), **Notifications** (destinations + Telegram/webhook/MQTT secrets), **App settings** (decoder/vision/capture + camera & machine health) — set a passphrase, and download a single `.mmbackup` file.

Because the file carries plaintext secrets the normal API never emits, it is always encrypted with your passphrase using a portable Argon2id + AES-256-GCM primitive that is **not** tied to this machine's at-rest key, so the file opens on any host. In Settings, restore previews the file's contents first, then applies it — **Replace** overwrites the selected sections, **Merge** appends — remapping foreign keys since primary keys are reassigned on insert. The first-run wizard's welcome step offers the same restore ("Restore from backup") but applies it directly in `replace` mode without a separate preview, letting a fresh install adopt an existing configuration; a restart afterwards is still needed for running services to pick it up. A restore also marks first-run setup complete, so the wizard does not reappear after the restart (the setup flag itself is not carried in the backup — it is set on restore because a restored machine is already configured).

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/settings/backup/sections"   # row counts per section
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"sections":["cameras","ai","notifications","settings"],"passphrase":"a strong passphrase"}' \
  "http://localhost:3000/api/settings/backup/export"   # returns {filename, dataBase64} — the .mmbackup bytes
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"dataBase64":"<file contents, base64>","passphrase":"a strong passphrase","mode":"replace"}' \
  "http://localhost:3000/api/settings/backup/restore"
```

Machine identity — the at-rest key, node pairing/enrollment, mTLS certificates, `config.json`, and the setup-complete flag — is deliberately never included, so a backup can't clone one host's identity onto another. Custom AI model `.pt` weights are referenced but not embedded (v1); local user accounts are out of scope (v1).

## Secure Wipe & Reset

A **factory reset** returns the appliance to a clean state. Ordered so the irreversible work survives an interrupt, it: stops camera services → **destroys the at-rest encryption key (crypto-erase)** → fast-erases all media (snapshots, training data, uploads, per-camera recordings — instant unlink) → closes the app's own database connection → drops and rebuilds the database (schema + stock seed) → securely scrubs the freed disk space (per-volume TRIM/discard then a best-effort, time-budgeted random overwrite for HDDs) → restarts the process. It is **disabled by default** and hidden unless `bootstrap.allowReset` is `true`; it is irreversible and does not touch `config.json` (runtime settings live in the DB and reset with it). Deleted footage during normal operation is also shredded by default, controlled by the `recording.shred` config block (`enabled`, `passes`, default 3). While a reset is running, every other API request gets a clean `503 reset in progress` instead of failing outright, since the database connection is already closed.

As a security measure the wipe is **intentionally unstoppable and best-effort**: the Postgres drop uses `DROP DATABASE ... WITH (FORCE)` to evict any connection still holding the database, and the orchestrator records a stage problem (an un-erasable file, a failed key destroy, a database-wipe error) as a non-fatal *warning* rather than aborting — it always drives to a restart (which re-runs bootstrap and can finish an interrupted rebuild). The real wipe guarantees are the crypto-erase and the instant unlink; TRIM + free-space scrub are defense-in-depth (no overwrite reliably erases original cells on flash) and a missing TRIM privilege (no Administrator/root) is reported as a warning, not a failure, since it doesn't affect the real guarantees. On sqlite, the database file couldn't previously be deleted on Windows because this process still held it open — the reset now closes its own connection first so the drop actually succeeds. In the UI the button lives in a **Danger Zone** on Settings → Machine Health, behind a 10-second auto-proceed countdown (cancel to stop), then a full-screen progress overlay that polls progress and reloads once the restarted server's health recovers.

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/system/reset/state"      # is reset allowed?
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/system/reset"    # wipe + reset + restart
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/system/reset/progress"   # in-flight progress
```

Docker deployments need a restart policy so the post-reset relaunch comes back up; bare-metal relaunches itself.

## Self-Update

Settings → Version & Health → **Updates** checks GitHub Releases for a newer `mymatasan` (on a 6-hour schedule, plus **Check now**) and shows current vs. latest version. On installs that own their files — the portable archive or the Windows service installer — an **Update to vX.Y.Z** button downloads the matching release archive, verifies its SHA-256 against the release's `checksums.txt`, swaps the binary and `static/`/`ai/` assets into place, and restarts (via the same `apphost.Restarter` the factory reset uses).

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/system/update"          # cached status
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/system/update/check"  # force a check
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/system/update/apply"  # download + verify + swap + restart, whatever is newest
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"version":"1.128.0"}' "http://localhost:3000/api/system/update/apply"  # pin an exact release
```

Self-update is intentionally unavailable — with in-panel guidance shown instead — on `.deb`/`.rpm` installs (`MYMATASAN_MANAGED=package`, set by `deploy/nfpm/mymatasan.service`: upgrade via `apt`/`dnf`) and Docker (`MYMATASAN_MANAGED=docker`, set by `deploy/Dockerfile.release`: pull the new image and recreate the container), and whenever the app's home directory isn't writable.

An optional `{"version": "..."}` body on `/api/system/update/apply` pins the exact release to
install — looked up by release **tag**, never "whatever GitHub calls latest" — instead of
whatever `Check now` most recently cached. This is what a `myseliasan` fleet lets an operator
drive: a **staged rollout** that upgrades an estate of nodes a ring at a time, canary first, and
only advances a ring once every node in it has come back reporting the version it was asked for
(see `apps/myseliasan/README.md` → "Staged version rollout"). Two guarantees hold regardless of
who calls this endpoint: the node **refuses to downgrade** (this suite's DB migrations are
forward-only, so an older build can meet a schema it has never seen) and it never installs
anything other than the exact version it was asked for.

## ONVIF API

All app-specific ONVIF routes use HTTP Basic Auth backed by local users stored in SQLite.

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"timeoutMs":3000}' \
  "http://localhost:3000/api/onvif/discover"
```

Discovery upserts WS-Discovery matches into the local database by XAddr and returns best-effort unauthenticated ONVIF metadata such as model, manufacturer, media service URL, RTSP URI, and snapshot URI when the camera allows it. Cameras that require ONVIF credentials may only return host and XAddr until you save credentials and resolve live view.

### Camera credentials (`/api/cameras`)

Saving a discovered camera with credentials verifies the login before persisting — a camera that actively rejects the login (e.g. wrong password) is **not** saved and returns `400`; an unreachable camera is saved anyway (we simply couldn't verify):

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"xAddr":"http://192.168.1.40/onvif/device_service","username":"admin","password":"cameraPass"}' \
  "http://localhost:3000/api/cameras/discovered"
```

Check whether a saved camera's stored credentials still authenticate (used by the camera node's access gate to decide whether to prompt for new credentials):

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/auth-check"
# {"status":"ok"}  |  {"status":"unauthorized"}  |  {"status":"unreachable"}
```

### Deleting a camera

`DELETE /api/cameras/{id}` cascades: before the camera row is removed, the app stops its recorder and detect-only stream, purges its recorded footage and object-metadata rows, deletes its detection rules, purges its alert events/snapshots, and deletes its recording config — in that order, so nothing is ever left half-torn-down. Any step failing aborts the whole delete (the camera row is untouched and the request errors) rather than leaving an orphaned recorder still writing segments or a stale rule that keeps the vision monitor sampling a camera that no longer exists.

### ONVIF device management (`/api/cameras/{id}/...`)

```bash
# Which management operations this camera's firmware supports
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/capabilities"

# Device identity for the Live View "Camera Information" panel
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/device-info"

# Local ONVIF user accounts
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/onvif-users"
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"username":"operator","password":"changeMe123","userLevel":"Operator"}' \
  "http://localhost:3000/api/cameras/1/onvif-users"
curl -u admin:"$ADMIN_PW" -X DELETE "http://localhost:3000/api/cameras/1/onvif-users/operator"

# Reboot / factory default (hard wipes network config, soft keeps it)
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/cameras/1/reboot"
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" -d '{"hard":false}' \
  "http://localhost:3000/api/cameras/1/factory-default"

# Clock: read, then set to NTP
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/datetime"
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"dateTimeType":"NTP","ntpServers":["pool.ntp.org"]}' \
  "http://localhost:3000/api/cameras/1/datetime"

# Network: read, then set a static IPv4
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/network"
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"interfaceToken":"eth0","dhcp":false,"ipAddress":"192.168.1.40","prefixLength":24,"gateway":"192.168.1.1","dns":["1.1.1.1"]}' \
  "http://localhost:3000/api/cameras/1/network"
```

The Camera node's Settings tab only shows the boxes for operations `capabilities` reports as supported, since Device-Management calls all share one mandatory ONVIF service and can't otherwise be told apart without probing.

Manual probe:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"address":"192.168.1.40"}' \
  "http://localhost:3000/api/onvif/probe"
```

List saved devices:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/onvif/devices?limit=50&offset=0"
```

Read live-view stream configuration:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/onvif/stream-config"
```

Read runtime settings:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/settings/runtime"
```

Update runtime settings:

```bash
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"decoder":{"mjpeg":{"ffmpegPath":"ffmpeg","quality":7,"threads":1},"ffmpeg":{"rtspTransport":"tcp","hwaccel":"none","hwaccelDevice":"","initHwDevice":"","videoDecoder":"","probeSize":1000000,"analyzeDuration":1000000,"lowDelay":true,"noBuffer":true}},"stream":{"webrtc":{"enabled":true,"iceServers":[]},"mjpegFallback":{"enabled":true}}}' \
  "http://localhost:3000/api/settings/runtime"
```

Auto-tune decoder runtime settings from saved camera RTSP metadata and local ffmpeg capabilities:

```bash
curl -u admin:"$ADMIN_PW" -X POST \
  "http://localhost:3000/api/settings/runtime/auto-tune"
```

Auto-tune inspects saved camera RTSP metadata, runs ffmpeg capability probing, and detects available GPU hardware before applying settings. Run RTSP Test on saved cameras first so auto-tune can see stored stream codec metadata.

Hardware selection priority:

| Platform | Priority order |
|---|---|
| **Linux** | CUDA (nvidia-smi confirmed) → VAAPI (detected render node) → software |
| **Windows** | CUDA (nvidia-smi confirmed) → d3d11va with discrete GPU → d3d11va default → dxva2 → software |
| **macOS** | VideoToolbox → software |

On Linux, auto-tune detects Docker, containerd, Kubernetes, and LXC container environments. When running in a container without GPU device passthrough, the auto-tune response includes an observation explaining which flags to add:

```text
Running inside a container. GPU hardware decode requires device passthrough:
  --device /dev/dri/renderD128   # VAAPI — Intel/AMD
  --gpus all                     # CUDA  — Nvidia
```

Add the appropriate flag to your `docker run` command, then click Auto Tune again.

The Settings page also queries `GET /api/settings/runtime/gpu-devices` and populates the GPU/device dropdown. Selecting a device from the list is sufficient; a free-text manual entry field appears only when **Manual entry…** is chosen from the dropdown.

- **Linux**: VAAPI render nodes (e.g. `/dev/dri/renderD128`) and CUDA GPU indices from `nvidia-smi`. Render nodes are only visible inside Docker when mounted with `--device`.
- **Windows**: display adapters listed in DXGI order matching Task Manager GPU numbering. Nvidia GPUs also appear as separate CUDA options. Index 0 in the d3d11va list corresponds to Task Manager GPU 0.
- **macOS**: VideoToolbox display names (device value is always empty; VideoToolbox selects the platform default).

List all ONVIF stream options exposed by a saved camera:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/stream-options"
```

The response contains `options[]` with `profileToken`, name, encoding, resolution, RTSP URI, and preferred/selected markers. The MyMataSan Stream tab uses this to show stream1/stream2 choices before saving.

Resolve a saved device to an RTSP URI. Omit `profileToken` to save the preferred profile, or pass a token returned by `stream-options` to pin a specific stream:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password","profileToken":"sub"}' \
  "http://localhost:3000/api/onvif/devices/1/stream-uri"
```

Probe the saved RTSP URI:

```bash
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/onvif/devices/1/rtsp-test"
```

If a camera returns `406 Not Acceptable` for an ONVIF-provided RTSP URL, stream selection and RTSP test try same-host stream paths derived from the selected profile. For TP-Link/VIGI-style main and sub profiles this means `/stream1` or `/stream2`. When a fallback succeeds, MyMataSan saves the working URL so live view and AI capture keep using it even after switching between stream1 and stream2.

If the RTSP test reports tracks but no H.264 video track, RTSP is reachable but the selected stream cannot be forwarded through the WebRTC path. Keep MJPEG fallback enabled, switch the selected stream to an H.264 substream, or change the camera's selected stream encoding to H.264 in the camera settings.

Change a camera-local ONVIF user password:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"targetUsername":"camera-user","newPassword":"new-camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/camera-password"
```

Move a saved PTZ-capable camera:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"direction":"left","speed":0.35,"durationMs":350}' \
  "http://localhost:3000/api/onvif/devices/1/ptz/move"
```

Stop PTZ movement:

```bash
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/onvif/devices/1/ptz/stop"
```

Prepare browser live view from the camera ONVIF media endpoints:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/live-view"
```

Create a WebRTC answer for browser live view:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"type":"offer","sdp":"..."}' \
  "http://localhost:3000/api/onvif/devices/1/webrtc/offer"
```

Check two-way audio (talk-back) capability and open a browser-mic session:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/talk"
# {"supported":true,"transport":"onvif","needsPassword":false,"hasPassword":false}
# TP-Link Tapo example: {"supported":true,"transport":"tapo","needsPassword":true,"hasPassword":false}

# TP-Link cameras only: save the cloud/speaker password before talk-back will connect.
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"password":"your-tplink-cloud-password"}' \
  "http://localhost:3000/api/cameras/1/talk/password"

curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"type":"offer","sdp":"..."}' \
  "http://localhost:3000/api/cameras/1/talk/offer"
```

Open the multipart MJPEG fallback stream:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/onvif/devices/1/live.mjpeg?fps=2"
```

Browser fallback requests add `preferSnapshot=1`, which tries the ONVIF snapshot URI first and then falls back to RTSP-to-MJPEG conversion when snapshots are unavailable. You can force snapshot-only output with `source=snapshot`.

## Recording API

All recording routes use the same local Basic Auth as the other APIs.

Recording enables event-triggered video clips saved around each AI alert. When an alert fires, the monitor sends the alert ID to the recording manager, which assembles a pre-roll buffer (captured before the alert) and a post-roll window (captured after) into a single MP4 file. Pre-roll and post-roll durations are configurable per camera.

Two recording modes are supported:

- **tick** — reuses the JPEG frames the vision monitor already captures (~1–2 fps). Zero extra CPU/network cost. Clip quality matches the monitor frame rate.
- **rtsp** — opens a dedicated full-fps RTSP connection to the camera and writes rolling `.ts` segments. Clip quality matches the camera stream. Requires an extra RTSP connection and an ffmpeg executable.

### Segment finalization & crash safety (RTSP mode)

A completed `.ts` segment is remuxed to `<stem>.mp4.part` and only renamed into place at `<stem>.mp4` **after** it has been encrypted (when encryption at rest is on); the source `.ts` is deleted only once that rename succeeds. This makes a bare `.mp4` on disk unambiguous proof of a complete, encrypted segment, and a `.ts` sibling unambiguous proof the segment was never finalized — so an interrupted remux (crash, restart, out-of-space) can never be mistaken for good footage, and the app always retries from the intact `.ts` instead of adopting a truncated file. A segment that fails to finalize after 3 attempts is moved to a `quarantine/` directory (a sibling of `live/` under each camera's storage path) rather than being left for the retention purge to silently discard — the footage is preserved and still ages out under the camera's normal retention, but it will not appear in the recordings list until an operator investigates.

### Split-stream setup (recommended for RTSP mode)

Many budget cameras allow only one concurrent RTSP connection. If the recorder holds the main stream, the live view tile goes black. The solution is to record on the sub-stream and keep the main stream for live view:

```json
{
  "cameraId": 1,
  "enabled": true,
  "streamUrl": "rtsp://user:pass@192.168.1.10/stream2",
  "fallbackStreamUrl": ""
}
```

`streamUrl` overrides the ONVIF-discovered URI for recording only. Live view continues to use the URI stored on the camera device record. Leave `streamUrl` empty to use the ONVIF URI for both.

`fallbackStreamUrl` is tried automatically after two consecutive quick connection failures of the primary stream (runtime < 10 s), which can happen when switching between sub-stream and main-stream profiles mid-session. The recorder toggles back to the primary on subsequent restarts.

### Configuration

Get the recording config for a camera (returns empty when not yet configured):

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/recording/config/1"
```

Create or update a per-camera recording config:

```bash
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"cameraId":1,"enabled":true,"preRollSec":30,"postRollSec":10,"storagePath":"./recordings","retentionDays":7,"segmentMinutes":15,"streamUrl":"","fallbackStreamUrl":""}' \
  "http://localhost:3000/api/recording/config"
```

The response includes a `recorderWarning` field that is non-empty when the recorder could not be hot-reloaded (e.g., no RTSP URI found), so the UI can surface it without treating the config save as a hard error.

Config changes are applied **immediately** without a restart via hot-reload.

List all recording configs:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/recording/config"
```

### Recorder status

Query the live state of all configured recorders:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/recording/status"
```

Each entry in the response array includes:

| Field             | Notes |
|-------------------|-------|
| `cameraId`        | Camera identifier. |
| `state`           | `streaming`, `stopped`, or `error`. |
| `mode`            | `rtsp` (writing footage) or `detect` (AI frame-source pull only, no recording — set when the recorder config is detect-only). The Recording tab uses `state`+`mode` together — `streaming`+`detect` renders a distinct blue "detect-only" badge instead of the green "Recording active" badge, since a detect-only stream running while NVR recording is off must not read as "recording". |
| `ffmpegRunning`   | Whether the ffmpeg subprocess is alive. |
| `liveFiles`       | Number of `.ts` segments currently on disk. |
| `liveDir`         | Path to the live segment directory. |
| `lastError`       | Most recent meaningful ffmpeg error (noisy harmless warnings are filtered). |
| `activeStreamUrl` | RTSP URI currently in use. |
| `usingFallback`   | `true` when the fallback URI is active. |

The Recording tab in the browser UI polls this endpoint every 10 seconds and shows a live status panel.

### Stream profile selection

List all ONVIF media profiles for a camera (uses stored credentials):

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/recording/streams/1"
```

Set the camera live-view stream URI to a specific RTSP URL:

```bash
curl -u admin:"$ADMIN_PW" -X POST -H "Content-Type: application/json" \
  -d '{"rtspUrl":"rtsp://user:pass@192.168.1.10/stream1"}' \
  "http://localhost:3000/api/recording/streams/1/live"
```

The Recording tab exposes an **Auto-configure** button that reads ONVIF profiles and automatically sets the main stream for live view and the sub-stream for recording.

### Clips

List recorded clips (filterable by camera, alert, and time range):

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/recording/segments?cameraId=1&limit=50&offset=0"
```

Filter clips by alert ID:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/recording/segments?alertId=42"
```

Download a clip:

```bash
curl -u admin:"$ADMIN_PW" -o clip.mp4 \
  "http://localhost:3000/api/recording/segments/1/download"
```

Delete a clip (removes the DB row and the file on disk; the file is securely shredded when `recording.shred` is enabled):

```bash
curl -u admin:"$ADMIN_PW" -X DELETE "http://localhost:3000/api/recording/segments/1"
```

Purge expired clips on demand — deletes only segments already past each camera's `retentionDays` (the same safe sweep the disk-mitigation job runs automatically), returning the count removed. Retention applies as soon as `retentionDays > 0`, regardless of whether recording is currently enabled for that camera — turning recording off only stops new segments being written, it does not freeze existing footage on disk forever:

```bash
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/recording/segments/purge"
```

The Recording tab in the browser UI shows the per-camera config form, the live recorder status panel, lists clips with inline download and delete buttons, and (for admins) a **Purge expired** button.

**Purge now** (Recording tab, admins) deletes ALL footage and AI-event snapshots for the selected camera immediately, ignoring `retentionDays` entirely — for when an operator needs a camera's history gone right away rather than waiting on the retention sweep. It is gated behind a 5-second cancellable countdown confirmation, mirroring the factory-reset wipe, and only refreshes that panel's own segment list afterward (no full-page reload):

```bash
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/recording/purge-camera?cameraId=1"
# {"segments": 42, "snapshots": 7}
```

Footage removal (`recordingService.PurgeAllForCamera`) is authoritative — its error fails the request; snapshot removal (`visionService.PurgeAlertsForCamera`) is best-effort so a snapshot hiccup can't leave the footage half-purged. `myseliasan` exposes the same action on a node's embedded Recording tab over the control tunnel proxy.

## Vision API

All app-specific vision routes use the same local Basic Auth as ONVIF routes.

The AI page is organized by camera first. Select a saved camera, create or edit rules for that camera, and draw the detection zone(s) or crossing lines over the live preview using a floating Paint-style drawing toolbox (select/add/delete zone tools, undo/redo, snap-to-grid, refresh-frame, flip H/V, center box, full frame, line-direction cycle, extend-to-edges, keyboard nudge/delete). A rule can have **multiple detection zones** (drag a rectangle, or click for a default box, to add each one); a detection counts if it falls inside *any* configured zone (union). Live-view camera tiles show a visible indicator when recent AI alert events are raised for that camera.

List detection rules:

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/vision/rules?limit=50&offset=0"
```

Create or update a rule:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Porch person after hours","detectionType":"person","zonePolygon":"[[0.1,0.1],[0.9,0.1],[0.9,0.8],[0.1,0.8]]","threshold":0.35,"minFrames":2,"cooldownSeconds":30,"soundEnabled":true,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

`zonePolygon` is JSON stored as normalized points from `0` to `1`, where `[0,0]` is the top-left of the video and `[1,1]` is the bottom-right. If no polygon is supplied, the reusable motion detector treats the whole frame as the zone.

Line crossing rules use the same YOLO object candidates as person, vehicle, and animal rules. `line_crossing` triggers when a tracked object crosses any configured line. `multi_line_crossing` triggers only when the same tracked object crosses the configured lines in sequence, up to five lines:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Entry sequence","detectionType":"multi_line_crossing","zonePolygon":"[[0,0],[1,0],[1,1],[0,1]]","ruleConfig":"{\"classes\":[\"person\",\"car\"],\"direction\":\"both\",\"maxSecondsBetweenLines\":20,\"lines\":[{\"id\":\"start\",\"points\":[[0.35,0.2],[0.35,0.8]]},{\"id\":\"end\",\"points\":[[0.65,0.2],[0.65,0.8]]}]}","threshold":0.55,"minFrames":1,"cooldownSeconds":10,"soundEnabled":true,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

Rule schedules are stored per rule in `schedulePolicy`. An empty value means the rule is always active. Weekly windows use local time by default or the configured IANA timezone:

```json
{
  "preset": "custom",
  "timezone": "Asia/Kuala_Lumpur",
  "mode": "allow",
  "windows": [
    {
      "days": ["mon", "tue", "wed", "thu", "fri"],
      "start": "18:00",
      "end": "07:00"
    }
  ]
}
```

Specific date and time ranges use RFC3339 timestamps:

```json
{
  "preset": "range",
  "timezone": "Asia/Kuala_Lumpur",
  "mode": "allow",
  "dateRanges": [
    {
      "start": "2026-06-09T22:00:00+08:00",
      "end": "2026-06-10T06:00:00+08:00"
    }
  ]
}
```

Use `"mode":"deny"` to keep a rule active except during matching windows or date ranges.

List alert events (newest first, all cameras, all dates):

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/vision/alerts?limit=50&offset=0"
```

Filter alerts to a specific camera and date range using the generic `filters`/`sorters` query contract (DataTable-shaped JSON, validated against `AlertEvent` fields):

```bash
curl -u admin:"$ADMIN_PW" -G \
  --data-urlencode 'cameraId=1' \
  --data-urlencode 'filters=[{"fieldName":"createdAt","compare":5,"value":1749657600},{"fieldName":"createdAt","compare":6,"value":1749743999}]' \
  --data-urlencode 'sorters=[{"fieldName":"createdAt","sort":2}]' \
  --data-urlencode 'limit=20' --data-urlencode 'offset=0' \
  "http://localhost:3000/api/vision/alerts"
```

The Alert Log panel in the browser UI is a `@shared/DataTable` grid in server mode: every column filter and sort click re-queries the backend directly (true DB-side paging, not a client-side slice), so it stays fast even with a large detection history. It defaults to the latest detections; a **Today** button seeds a `createdAt` date-range filter for the current day.

Acknowledge an alert:

```bash
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/vision/alerts/1/ack"
```

The monitor captures JPEG frames from the saved camera RTSP or snapshot source, applies the configured detector, then applies threshold/min-frame/cooldown settings before persisting alert events. When `vision.detector.mode` is `external`, `hybrid`, or `persistent`, object candidates from the configured detector process are matched to rule types, zone polygons, thresholds, min-frame counts, line-crossing state, and cooldowns before alert persistence.

Each detection result carries the **frame capture timestamp** (`FrameCapturedAt`), which is passed through to the recording manager as the clip anchor. This means the pre-roll/post-roll window is always centred on when the subject was visible in the frame, not on when YOLO finished processing it. Without this, YOLO's inference latency (100–500 ms per frame) caused recordings to start after the subject had already left, producing empty clips when motion and person detection were both enabled.

### Detection class registry and the two-axis rule model

A detection rule has two independent axes:

- **Mode** (the `detectionType` field) — *how* to detect: `presence`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`.
- **Target classes** (`ruleConfig.classes`) — *what* to detect, chosen from the **class registry**.

The class registry decouples object classes from rule modes so trained/custom classes are first-class. Built-in classes (`person`, `vehicle`, `animal`, `fire`, `smoke`) are seeded from the configured `classMap` on first run. You can also create **groups** (e.g. `delivery = courier + van`) that expand to their members, and **trained** classes are added automatically when a custom model is activated. At rule-load time the monitor resolves each rule's target slugs (categories/groups) to concrete model labels, so the app-neutral detector never needs to know about the registry.

```bash
# List registry classes (built-in, trained, and groups)
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/vision/classes"

# Create a group
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"name":"delivery","displayName":"Delivery","kind":"group","members":["courier","van"]}' \
  "http://localhost:3000/api/vision/classes"
```

A presence rule that watches one or more registry classes stores them in `ruleConfig.classes` and uses `detectionType:"presence"`:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Courier at gate","detectionType":"presence","zonePolygon":"[[0.1,0.1],[0.9,0.1],[0.9,0.9],[0.1,0.9]]","ruleConfig":"{\"classes\":[\"courier\"]}","threshold":0.4,"minFrames":2,"cooldownSeconds":30,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

A crowd rule fires when at least `minCount` people are in the zone in a single frame:

```bash
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Crowd in lobby","detectionType":"crowd","zonePolygon":"[[0,0],[1,0],[1,1],[0,1]]","ruleConfig":"{\"minCount\":3,\"classes\":[\"person\"]}","threshold":0.4,"minFrames":2,"cooldownSeconds":30,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

### License-plate recognition (LPR)

LPR rules use `detectionType:"lpr"` and a `ruleConfig` JSON object. They require a plate-detector model to be active (Settings → AI → License Plate Model); OCR is provided by `easyocr` (install-deps from the same panel).

```bash
# Fire on any readable plate
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Gate plate log","detectionType":"lpr","ruleConfig":"{\"matchMode\":\"any\"}","cooldownSeconds":10,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"

# Fire only on plates in the watchlist (VIP/fleet)
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"VIP arrival","detectionType":"lpr","ruleConfig":"{\"matchMode\":\"include\",\"plates\":[\"WXY1234\",\"ABC999\"]}","cooldownSeconds":30,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"

# Fire on any plate NOT in the allowed list (unknown vehicle at gate)
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Unknown vehicle","detectionType":"lpr","ruleConfig":"{\"matchMode\":\"exclude\",\"plates\":[\"KnownCar1\",\"KnownCar2\"]}","cooldownSeconds":30,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

`ruleConfig` keys for LPR:

| Key | Default | Notes |
|---|---|---|
| `matchMode` | `"any"` (no plates) or `"include"` (with plates) | `any`, `include`, or `exclude`. |
| `plates` | `[]` | Watchlist; entries are normalized to uppercase alphanumerics (`"WXY 1234"` → `"WXY1234"`). |
| `minOcrConfidence` | `0.5` | OCR reads below this floor are ignored. |
| `plateLabel` | `"license plate"` | Override when a custom plate model uses a different class name. |

LPR alert metadata includes `plate`, `vehicleType`, `color`, `ocrConfidence`, and `watchlisted`; notification templates can reference `{{plate}}`, `{{vehicleType}}`, `{{color}}`, and `{{watchlisted}}`. The in-app Notifications page shows a **Plate** field on LPR rows for the same reason the face section above gets a **Person** field: the identity (or plate) is the point of the alert and used to only be visible inside the generic Object/label text.

Check whether the camera has sufficient resolution for LPR (requires an ONVIF camera; result is cached 15 min):

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/cameras/1/lpr-capability"
```

Purge alert events (e.g. to clean up a backlog of diagnostic rows):

```bash
# Purge only diagnostic alerts older than 7 days
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/vision/alerts/purge?days=7&onlyDiagnostics=true"
# Purge ALL alerts older than 30 days (real detections included)
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/vision/alerts/purge?days=30"
```

## Teach wizard

The **Teach** page (`/api/teach`) is the primary, zero-ML-knowledge way to give a camera a new detection skill — it drives the training/detection machinery below without exposing datasets, boxes, epochs, or the word "YOLO". A skill has three kinds:

- **Recognize a new object** — spot a thing anywhere in view (e.g. a courier uniform, a forklift). Standard object detection.
- **Tell good from bad** — inspection: judge items that appear in the same spot (good product vs defective). Trains a contrast pair (`<slug>` / `not <slug>`).
- **Spot anything unusual** — anomaly detection from good-only footage: an image-embedding memory bank (resnet18) learns what normal looks like and alerts on deviations. Anomaly models run **alongside** stock/custom detection via a per-camera manifest (`ai/anomaly_worker.py` fits, `yolo_worker.py` scores), so they never occupy the single custom-model slot.

The wizard is a six-step flow, each step resumable (the skill row stores its progress): **name it → what kind → where (draw an ROI on the live frame) → show examples → check accuracy → turn it on**. Sample collection is *session-based* — you press "show me good ones" and the camera auto-captures presence-gated, deduplicated frames labelled by the session, with a live filmstrip and a plain-language sample coach. "Check accuracy" quick-trains and evaluates on a held-back split, reporting in human terms ("I got 19 of 20 right") with a gallery of the misses and an F1-tuned suggested threshold. A **test drive** runs the candidate model on the live camera (a second, throwaway detector worker via an env override — the live pipeline is untouched) before you commit. **Turn it on** trains the final model, hot-swaps it in, and auto-creates the detection rule + alert; **Keep teaching** files ✓/✗ verdicts on live alerts back into the dataset so the skill improves. Skills export/import between devices as passphrase-encrypted **`.mmskill`** packages (`infra/atrest` Argon2id + AES-GCM). Rules created by a taught skill carry a **"Taught"** badge in AI Detection.

> **Where the old Training page went.** The manual Training tab was retired. Its lower-level surfaces live on: the **model registry** (import a `best.pt` / activate / deactivate, Settings → AI) and **Object Classes** management, now its own top-level **Intelligence → Object Classes** nav item (alongside Teach and Object Search) rather than nested in Settings; the dataset/label/train/model REST endpoints below are unchanged (the Teach wizard drives them server-side).

## Custom Model Training API

The `/api/training` endpoints power the model registry and the datasets the Teach wizard builds (and are still usable directly for advanced offline workflows): collect images → label → train → activate. All routes use the same local Basic Auth. Training artifacts are stored under `vision.training.dataDir` (defaults to a `training` sibling of `snapshotDir`).

The underlying workflow has three stages (mirrored by the API):

1. **Datasets** — a dataset is a named collection of labeled images for one set of classes.
2. **Images & Labels** — upload JPEGs or import alert snapshots (which arrive pre-labeled from the alert's detection box), then draw/move/delete bounding boxes and assign a class per box. **Auto-label** runs the active detector on an image and fills boxes for you to correct. Background images (false-positive corrections) export as YOLO empty-label frames, capped at ~15% of the labelled set.
3. **Models** — **Train** the dataset (in-app when a CUDA GPU is present, or **Export** a YOLO zip to train elsewhere), then **Activate** a model. Model import/activate is available from **Settings → AI**.

```bash
# Datasets
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/training/datasets"
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"name":"Couriers","classes":["courier","van"]}' \
  "http://localhost:3000/api/training/datasets"

# Add images: multipart upload, or import an existing alert snapshot (pre-labeled)
curl -u admin:"$ADMIN_PW" -F "file=@frame.jpg" "http://localhost:3000/api/training/datasets/1/images/upload"
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"alertId":42}' "http://localhost:3000/api/training/datasets/1/images/from-alert"

# Label: save boxes (normalized top-left x,y + w,h), or auto-label with the active model
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"annotations":[{"className":"courier","x":0.3,"y":0.25,"w":0.2,"h":0.4,"source":"manual"}]}' \
  "http://localhost:3000/api/training/images/7/annotations"
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/training/images/7/autolabel"

# Export a YOLO dataset zip (data.yaml + images/labels train/val split) to train elsewhere
curl -u admin:"$ADMIN_PW" -OJ "http://localhost:3000/api/training/datasets/1/export"

# Check in-app training capability (Python / ultralytics / CUDA)
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/training/capability"

# Stock (base) model — Settings → AI also exposes this as a dropdown
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/training/stock-model"
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"model":"yolo11s.pt"}' "http://localhost:3000/api/training/stock-model"

# Train in-app (background job; poll the models list for progress/status)
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"datasetId":1,"epochs":50,"imgsz":640}' "http://localhost:3000/api/training/models"

# Import a best.pt trained offline
curl -u admin:"$ADMIN_PW" -F "file=@best.pt" -F "name=Couriers v1" -F "classes=courier,van" \
  "http://localhost:3000/api/training/models/import"

# Activate a model — hot-swaps the live detector and registers its classes
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/training/models/3/activate"

# Revert to the stock model (so the active custom model can be deleted)
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/training/models/deactivate"
```

The active model cannot be deleted while in use — **Deactivate** first (reverts the worker to the bundled `yolo11n.pt`), then delete.

**Hot-swap mechanism.** Activation writes the model's weights path to an active-model pointer file that the YOLO worker reads on (re)start (`MYMATASAN_ACTIVE_MODEL_FILE`, default `active_model.txt` next to the worker script). The persistent worker is then restarted so the next inference loads the new weights. The model's classes are upserted into the class registry so they appear in the rule target picker immediately.

**Using a trained model in the AI tab.** Once activated, the model's classes show up in the **Detect (what)** target picker when you create or edit a rule, so you can build rules on your custom classes just like built-in ones.

> **Parallel stock + custom, custom takes priority.** The stock model (`yolo11n.pt`, or `MYMATASAN_YOLO_MODEL`) **always runs**. Activating a custom model runs it **alongside** stock — the worker loads both, runs both per frame, and merges their detections: custom (taught) detections are kept first (same-label overlaps de-duplicated), then a stock detection is **dropped** if a kept custom detection overlaps its box — regardless of label — so stock `dog` + custom `cat` on the same animal collapses to just `cat` rather than reporting both. Stock detections the custom model doesn't touch are unaffected, so a model trained only on `papa` still **adds** `papa` on top of stock `person`/`vehicle`/`animal`/… elsewhere in the frame. This precedence also applies to **AI alert rules** — a rule targeting a stock class won't fire on an object the taught model has relabeled (e.g. a stock "dog" rule won't fire on something the custom model calls "cat"). **Only one** custom model runs at a time — activating another switches to it; **Deactivate** reverts to stock-only. Running two models is somewhat slower per frame (each frame is inferenced twice), which matters most on CPU-only devices.

**In-app training requirements.** In-app training needs `python` + `ultralytics` (the same Python the detector uses). The **Train** button is disabled when these are missing and warns when only a CPU is available — CPU training is impractically slow, so export and train on a GPU instead. One training run executes at a time. The bundled trainer is `apps/mymatasan/ai/train_worker.py`.

### License-plate model management

A **License Plate Model** card in **Settings → AI** manages the optional second-stage plate-detector model (separate from the stock/custom general model). Use it to:

- Select a curated model from the catalog (YOLOv11 nano/small plate finetunes, downloaded from Hugging Face) or paste any https URL.
- Upload your own `.pt` plate-detector file.
- Deactivate the plate model (disables LPR OCR).
- Install OCR dependencies (`easyocr`, `opencv-python`, `numpy`) via the in-app installer (streams progress to the same log as GPU setup; CPU-only, no GPU required).

```bash
# Current plate model info + catalog + OCR readiness
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/training/lpr-model"

# Select a catalog model (downloads from Hugging Face, stores under <dataDir>/lpr/)
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"model":"yolo11n-license-plate"}' "http://localhost:3000/api/training/lpr-model"

# Select by URL
curl -u admin:"$ADMIN_PW" -H "Content-Type: application/json" \
  -d '{"model":"https://example.com/myplate.pt"}' "http://localhost:3000/api/training/lpr-model"

# Upload a plate model file
curl -u admin:"$ADMIN_PW" -F "file=@plate.pt" -F "name=myplate" \
  "http://localhost:3000/api/training/lpr-model/import"

# Deactivate (disables the plate-localization + OCR stage)
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/training/lpr-model/deactivate"

# Install OCR dependencies (poll GET /api/training/setup-deps/status for progress)
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/training/lpr-model/install-deps"
```

The plate model path is written to `lpr_model.txt` (next to `active_model.txt`) and the YOLO worker reads it via `MYMATASAN_LPR_MODEL_FILE`. OCR readiness is probed with `importlib.util.find_spec('easyocr')` so the probe is fast (no torch import). A plate model can be active even when OCR deps are not yet installed — plates will be localized but not read until deps are present.

## Notifications

Every source — AI detection, camera health, machine health, and login security — funnels into one persisted notification store. The **topbar bell** and a dedicated **Notifications page** read it via `GET /api/notifications`: the bell badge is server-truth unread, and clicking an entry opens the Notifications page focused on it. Besides the existing Unread/All and category-type chip filters, a **source-camera dropdown** (`meta.allCameras` default) narrows the feed to one camera by passing `cameraId` straight through to `GET /api/notifications` — server-side, so it never over-fetches and filters in the browser; only AI and camera-health rows carry a `cameraId`, so picking a camera naturally excludes machine-health/login-security rows. AI rows are rich — annotated event screenshot, detection fields, **Acknowledge**, and in-page **clip playback** when a recording segment exists; acknowledging an alert dismisses its notification at the source (`notifier.MarkReadByRef`). Diagnostics never become notifications, so the feed is inherently diagnostic-free.

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/notifications?unread=true&limit=30"
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/notifications/1/read"
```

Old notifications are purged automatically on the configured **Retention** (Settings → Notifications: days, "only delete read", interval). The same purge can be run on demand — `olderThanDays` is required and `onlyRead=true` keeps unread entries — surfaced as a **Purge expired now** button in that settings panel:

```bash
curl -u admin:"$ADMIN_PW" -X POST "http://localhost:3000/api/notifications/purge?olderThanDays=30&onlyRead=true"
```

### Dashboard Intelligence: heatmap, expected-activity band, anomaly alerts, reliability, noise

An hourly rollup table (`notification_rollup`), incrementally aggregated from the notification feed by a background maintainer, backs three analytics on the Dashboard without re-scanning raw history:

```bash
# Activity heatmap: local day-of-week x hour-of-day grid over the last 28 days (default)
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/notifications/heatmap?tzOffset=-480"
# Expected-activity band for the events-over-time chart (bucket=hour or day)
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/notifications/baseline?bucket=hour&tzOffset=-480"
```

`cameraId=` scopes either endpoint to one camera. The baseline is a robust median ± k·MAD band (Poisson floor for sparse slots) built from the trailing 8 weeks of the same weekday+hour (or day-of-week) slot; a bucket reports `learning: true` until it has at least 2 historical samples.

An **anomaly monitor** (Settings → AI / Dashboard card, **opt-in**) has two tiers, selected by `mode`:

- **`smart`** (default) reuses the per-camera baseline above to score each closed hour and raise a distinct `analytics.anomaly` notification for a spike or "unusual silence" (a normally-active camera going quiet — the tamper/obstruction/offline signal that a plain motion/AI rule can't catch). Needs a few weeks of activity history to be meaningful.
- **`manual`** compares the whole system's hourly event total against fixed `manualUpper`/`manualLower` thresholds instead (`0` disables a side) — no learning period, usable from day one; findings are site-wide (`cameraId: 0`), not per-camera.

```bash
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/anomaly/settings"
# Smart mode
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"enabled":true,"mode":"smart","sensitivity":3.0,"detectHigh":true,"detectLow":true,"minActivity":3,"requireConsecutive":1,"cooldownHours":6,"checkIntervalMs":300000}' \
  "http://localhost:3000/api/anomaly/settings"
# Manual mode: alert when the whole system logs >500 or <5 events in an hour
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"enabled":true,"mode":"manual","manualUpper":500,"manualLower":5,"requireConsecutive":1,"cooldownHours":6}' \
  "http://localhost:3000/api/anomaly/settings"
# Preview what would alert right now (runs whichever tier `mode` selects), without waiting for the background monitor or affecting its debounce/cooldown state
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/anomaly/scan"
```

`sensitivity` (k, 1.0–6.0, default 3.0 — lower is more sensitive, smart mode) sets the band half-width; `minActivity` keeps genuinely-quiet hours from being flagged as "unusual silence" (smart mode); `requireConsecutive` debounces one-off blips and `cooldownHours` prevents a sustained anomaly from alerting every hour (both modes).

Two further cards need no baseline/history at all:

```bash
# Camera reliability scorecard: worst-uptime-first, last 7 days (from/to default)
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/notifications/reliability"
# Alert noise ratio: top cameras by AI-alert volume + unread count, last 7 days
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/notifications/noise?limit=8"
```

`reliability` derives per-camera uptime %, offline seconds, incident count, and whether it's currently offline by pairing the camera-health monitor's own offline/recovery notification events over the window (a still-open outage at the window's end is extended to `to` rather than dropped). `noise` surfaces which cameras are generating the most AI alerts and what fraction go unread — a camera firing constantly while being ignored is usually a tuning candidate (threshold, zone, or schedule).

### Object Search (object metadata recorder)

A dedicated **Intelligence → Object Search** page: a searchable, cross-camera text log of "what each camera saw" — presence intervals coalesced from the same AI inference the detection rules already run (a metadata-only camera with no rules still gets exactly one inference pass per sample, no extra decode). Capture tracks the camera's recording `enabled` flag directly — there is no separate metadata on/off toggle, only the per-camera cooldown:

```bash
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"cameraId":1,"enabled":true,"metadataGapSeconds":5}' \
  "http://localhost:3000/api/recording/config"
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/observations?cameraId=1&limit=50"
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/observations/labels?cameraId=1"
# Multi-select object filter (any of "person"/"car") via the In compare operator (7)
curl -u admin:"$ADMIN_PW" -G "http://localhost:3000/api/observations" \
  --data-urlencode 'filters=[{"fieldName":"Label","compare":7,"value":["person","car"]}]'
```

`metadataGapSeconds` (default ~5s, the Recording tab's **"Object sighting cooldown (s)"** field) is how long a label may go unseen before its interval closes — a brief occlusion or dropped frame doesn't split one presence into many rows. Search is restricted to cameras whose continuous NVR recording is actually on: metadata capture is independent of footage recording, so a detect-only camera (recording disabled) logs observations with no footage behind them — without this restriction those sightings would surface as rows permanently stuck on "Finalizing…" that never resolve, since a detect-only recorder keeps no segments at all. Each remaining result is enriched with the recording segment covering it (`segmentId`, `segmentCodec`, `seekSeconds` — seeking to the sighting's peak-confidence frame, not the interval start) for one-click playback, or `footagePending: true` when the sighting is inside the camera's still-recording (not yet finalized) segment rather than a genuine gap. Retention follows the camera's own recording `retentionDays` (30-day fallback for metadata-only cameras).

The search UI adds date-range, camera, multi-object, and minimum-confidence filters over a paged results grid; each row's Time column shows the single sighting timestamp (its `startedAt`), a footage screenshot (`GET /api/recording/segments/{id}/frame?seek=&box=&label=` — extracted and disk-cached, with the detection box drawn on request) with play and maximize/camera overlay buttons, and results export to CSV or PDF. The same footage-screenshot-with-play-overlay treatment replaced the plain play buttons on the Recordings tab and the Notifications event snapshot.

#### Appearance search ("Find similar")

Any person or vehicle result row also carries a **Find similar** action: pick a sighting and
the search ranks every other sighting the camera has recorded by how much it *looks like* the
one picked — clothing colour, build, vehicle shape/colour. It is a separate per-camera switch
from object metadata, **requiring** `metadataEnabled` (the descriptor rides on the sighting
metadata creates) and off by default, since it costs real compute — a neural-network forward
pass over every person/vehicle in every sampled frame, not free the way metadata capture is:

```bash
curl -u admin:"$ADMIN_PW" -X PUT -H "Content-Type: application/json" \
  -d '{"cameraId":1,"enabled":true,"metadataGapSeconds":5,"appearanceEnabled":true}' \
  "http://localhost:3000/api/recording/config"
curl -u admin:"$ADMIN_PW" "http://localhost:3000/api/observations/appearance?observationId=42&limit=25"
```

The ranking is deliberately **not** a match percentage. Measured against the real model, two
crops of the SAME subject score cosine similarity ~0.98, and two crops of two OBVIOUSLY
DIFFERENT subjects score ~0.95 — the raw number barely moves regardless of who is compared, so
showing it as a percentage would read as near-certainty on every row. Instead each candidate is
scored by **`standout`** — a robust z-score (deviations above the median similarity of
everything actually compared, scaled by the median absolute deviation) — with `calibrated:
false` and a raw-similarity fallback ranking reported honestly when fewer than 12 candidates
were compared. The descriptor is a general-purpose ImageNet embedding (the same resnet18
backbone the taught-anomaly feature already loads, so this needs no extra model download on an
install with no internet egress), not a person re-identification network: results are a ranked
shortlist for an operator to confirm by eye, never an identification, and vectors from
different models are never compared against each other. `GET /api/observations/appearance`
(search, by `observationId` or by a supplied `vector`+`model`+`label`) and
`GET /api/observations/appearance/vector` (the wire-form descriptor, used when `myseliasan`
federates the same question across the fleet — see its README) sit under the same page grant
as Object Search. Appearance descriptors are purged with their sighting on both retention and
the per-camera **"Purge now"** action (`POST /api/recording/purge-camera`, which now purges
object metadata generally, not just footage and AI snapshots).

### Outbound delivery destinations

Notifications are also delivered **per destination**. In **Settings → Notifications** you add any number of delivery destinations (**webhook**, **Telegram**, or **MQTT**), each with its own:

- **Enabled** flag and **minimum severity** floor.
- **Receives** — a category subscription: AI detection alerts (`vision.alert`), Health (`health.check`, camera offline + machine health), and System (`system`).
- **Detection fields** — which detection details a vision alert includes.
- **Snapshot delivery** — `Inline` embeds the image (webhook/MQTT base64, Telegram photo) or `Link only` sends just the reference (`link` + `data.snapshotPath`) so the consumer fetches it — keeps payloads/broker messages small.
- **Custom fields** — static key/value pairs added to the payload. A custom field **overrides** a built-in field of the same key (the matching detection-field toggle is then disabled). Values may use `{{token}}` templates: `{{ruleName}}`, `{{cameraName}}`, `{{label}}`, `{{confidence}}`, `{{detectionType}}`, `{{alertId}}`, `{{ruleId}}`, `{{cameraId}}`. For LPR alerts, additionally: `{{plate}}`, `{{vehicleType}}`, `{{color}}`, `{{watchlisted}}` (empty string on non-LPR alerts). For face-recognition alerts, additionally: `{{person}}` (recognized name, `"unknown"` for a stranger), `{{recognized}}` (`true`/`false`), `{{faceConfidence}}` (empty string on non-face alerts).

The in-app notification feed always records one entry per alert in full; destinations receive separately rendered copies. **Per-rule routing:** a detection rule's **Notification routing** panel picks which destinations its alerts go to (none selected = all).

Saving is per-section: `PUT /api/settings/notification/destination` upserts one destination and `DELETE /api/settings/notification/destination/{id}` removes one, both via read-modify-write against the stored settings so editing or deleting one destination never clobbers another destination's config or the retention section; `PUT /api/settings/notification/retention` saves retention on its own the same way.

**Cold-start delivery reliability.** All outbound channels (webhook, Telegram, MQTT) now retry transient failures on a short backoff schedule (1 s → 3 s → 6 s, total ≤ 10 s) before dropping a notification. This prevents the first alert after boot from being silently lost when the network, DNS, or MQTT broker is still coming up. Marshal/build failures are marked permanent and not retried.

### MQTT

MQTT is the industrial transport: each notification is published as the same JSON payload (see below) to a broker topic. Per destination you configure the **broker URL** (`tcp://host:1883` or `ssl://host:8883`), **topic**, optional **client id**, **QoS** (0/1/2, default 1), and **retain**. Auth is **username/password** and/or **TLS** — paste PEM contents for the CA certificate and, for mutual-TLS (client-certificate auth), the client certificate + key. The broker connects in the background with automatic reconnect; the first publish also waits for the initial broker connect to complete so the cold-start race (event fires before the TCP handshake finishes) no longer drops the first message.

The **topic may be templated** with `{{token}}` placeholders (e.g. `matasan/alerts/{{cameraName}}/{{label}}`). Tokens resolve from the payload `data` (`cameraName`, `alertId`, `ruleId`, `detectionType`, plus `label`/`confidence`/`ruleName` when those detection fields are enabled), falling back to the notification's own fields `cameraId`, `category`, `severity`, `refId`, `id`. A token that resolves to nothing leaves **no empty level** — `matasan/alerts/{{cameraId}}` with no camera (e.g. the Test button) publishes to `matasan/alerts`, not `matasan/alerts/`. (Wildcards `#`/`+` are subscribe-side only; you never publish to them.)

### Webhook payload

Each notification is POSTed as a single JSON object. The core `Notification` fields are at the top level; detection details and custom fields are under `data`. When a snapshot is included it is inlined as base64 (so receivers that cannot reach the auth-protected snapshot endpoint still get the image).

```json
{
  "id": "9f2c1a7e-4b3d-4f9a-8c21-6e0b1d4a7f55",
  "category": "vision.alert",
  "severity": "critical",
  "title": "Fire detected — Kitchen",
  "body": "Front Gate • fire • 92% confidence\nsite: Front Gate",
  "source": "vision-monitor",
  "cameraId": 3,
  "refType": "alert_event",
  "refId": 481,
  "link": "/api/vision/alerts/481/snapshot",
  "data": {
    "alertId": 481,
    "ruleId": 12,
    "cameraName": "Front Gate",
    "detectionType": "presence",
    "ruleName": "Fire detected — Kitchen",
    "label": "fire",
    "confidence": 0.92,
    "boundingBox": "[0.41,0.22,0.18,0.30]",
    "zonePolygon": "[[0.1,0.1],[0.9,0.1],[0.9,0.9],[0.1,0.9]]",
    "snapshotPath": "recordings/cam3/snapshots/481.jpg",
    "site": "Front Gate"
  },
  "createdAt": 1781779140,
  "snapshotBase64": "/9j/4AAQSkZJRgABAQAA...(base64 JPEG)...",
  "snapshotContentType": "image/jpeg",
  "snapshotFilename": "alert-481.jpg"
}
```

Notes:
- Identifiers (`data.alertId/ruleId/cameraName/detectionType`) are always present; the other `data.*` keys and the `snapshot*` fields appear only when that destination enables them.
- `boundingBox` and `zonePolygon` are JSON **strings** (parse them as nested JSON on the receiving end).
- Custom fields appear in `data` **and** as `key: value` lines appended to `body`, so text-only channels (Telegram) display them too.

## Runtime Metrics

Prometheus is enabled by default (`telemetry.prometheus.enabled`, see root `README.md` → Telemetry) and scraped from `/metrics`. On top of the shared `kopiv2_api_*`/`kopiv2_tx_*` series, mymatasan records the app-specific numbers an operator needs to diagnose a site they can't log into — most of these were previously answerable only by reading logs, if at all.

| Metric | Type | Labels | What it tells you |
|---|---|---|---|
| `mymatasan_inference_duration_ms` | histogram | `camera` | Detector pass latency. First thing to check when detection "feels slow" or frames are being skipped. Timed on both success and failure. |
| `mymatasan_frames_total` | counter | `camera`, `outcome` (`ok`/`capture_failed`/`detect_failed`) | Sampled-frame outcomes. A camera silently failing every capture is otherwise indistinguishable from a quiet one. |
| `mymatasan_alerts_total` | counter | `camera`, `kind` (`detection`/`diagnostic`) | Emitted alerts by kind, so a diagnostic flood is distinguishable from real activity. |
| `mymatasan_camera_online` | gauge | `camera` | `1` when the camera's last health probe succeeded, else `0`. |
| `mymatasan_cameras_offline` | gauge | — | Count of currently unreachable cameras. |
| `mymatasan_disk_used_percent` | gauge | `mount` | Disk usage per mount, including the recordings volume. |
| `mymatasan_recording_paused` | gauge | — | `1` while the disk guard has recording paused — no footage is being written while this is `1`. |
| `mymatasan_disk_mitigation_total` | counter | `action` (`pause`/`resume`/`overwrite`) | Disk-guard actions taken; explains "why did recording stop overnight?" |
| `mymatasan_recording_coverage_percent` | gauge | `camera` | Percentage of the last scored hour that has footage on disk, per camera — what the continuity monitor scores against. |
| `mymatasan_recording_gap_cameras` | gauge | — | Cameras currently alerting for missing footage; the single number worth alerting on for continuity. |
| `mymatasan_camera_tamper_total` | counter | `kind` (`frozen`/`covered`/`moved`) | Camera tamper alerts raised, by kind. |
| `mymatasan_audit_write_failures_total` | counter | — | Audit entries that could not be persisted. The audit service swallows its own write errors on purpose (auditing must never fail the action being audited), so this is the ONLY symptom a trail that has silently stopped recording produces. |
| `mymatasan_audit_retention_purged_total` | counter | — | Audit rows removed by age-based retention (not yet wired to a scheduler in mymatasan; the counter exists in the shared service regardless). |
| `kopiv2_recording_ffmpeg_restarts_total` | counter | `camera` | Capture-ffmpeg restarts. Thrashing here means a camera is failing to hold its RTSP connection — the earliest signal of a flapping stream. |
| `kopiv2_recording_segment_finalize_total` | counter | `camera`, `outcome` (`saved`/`discarded`/`failed`/`unsaved`/`quarantined`) | Segment finalize attempts by outcome. Anything but `saved` accumulating means footage isn't reaching the recordings list. |
| `kopiv2_notification_delivery_total` | counter | `channel`, `outcome` (`ok`/`failed`/`panic`) | Outbound notification delivery attempts (webhook/telegram/MQTT/etc). Delivery is at-most-once — a drop here used to be invisible outside a log line. |
| `kopiv2_control_events_forwarded_total` | counter | `kind` | Node events (notifications, going-offline) successfully pushed up the fleet control channel to `myseliasan`. Only meaningful next to the drop counter below — a drop count with no total is a number nobody can size. |
| `kopiv2_control_events_dropped_total` | counter | `kind`, `reason` (`disconnected`/`write_failed`) | Node events that could **not** be forwarded — the control channel was down, or the write itself failed mid-flight. Both paths used to return silently with no record. The running count since the last successful hello also rides upstream on the node's next control-channel hello, so `myseliasan` sees it too (`myseliasan_node_events_dropped_total`). |
| `mymatasan_task_panics_total` | counter | `task` | Recovered panics in `infra/safego`-supervised background tasks (camera samplers, monitors, etc). A supervised task that panics is restarted automatically — that's the whole point — but otherwise leaves no other trace than one log line, so a task crash-looping every couple of minutes looks like a healthy process from the outside without this counter. |

What's worth alerting on:
- `mymatasan_cameras_offline > 0` sustained — a camera the health monitor can no longer reach.
- `mymatasan_recording_paused == 1` sustained — the disk guard has stopped all recording to protect the host.
- Any increase in `kopiv2_recording_segment_finalize_total{outcome="quarantined"}` — footage is on disk but will never appear in the recordings list; page on this one.
- A rising `kopiv2_notification_delivery_total{outcome="failed"}` on one `channel` — the difference between "alerts stopped" and "alerts are still firing, that one destination is down".
- `mymatasan_disk_used_percent` approaching the configured mitigation threshold, or a rising `kopiv2_recording_ffmpeg_restarts_total` for one camera (a flapping stream).
- Any increase in `kopiv2_control_events_dropped_total` while this node is adopted into a fleet — an AI alert or health event that never reached `myseliasan`'s unified feed live (the reconnect replay is expected to recover it, but only inside `myseliasan`'s 72h replay window).
- A rising `mymatasan_task_panics_total` for any `task` — a background subsystem is crash-looping while the process stays up and the API keeps answering.
- `mymatasan_recording_gap_cameras > 0` sustained — a camera the continuity monitor believes has stopped writing footage.
- Any increase in `mymatasan_audit_write_failures_total` — the security/evidence trail has a gap.

All of the above are emitted via the shared `infra/telemetry.Metrics` registry (see root `README.md` → Telemetry → App-defined metrics), which caps cardinality per metric name and surfaces truncation in the scrape rather than silently dropping series.
