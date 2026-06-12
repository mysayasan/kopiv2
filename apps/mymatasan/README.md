# mymatasan

`mymatasan` is the standalone camera and video intelligence app for `kopiv2`.

It is designed to run on small devices such as Raspberry Pi or Jetson-style micro computers. It discovers ONVIF cameras, persists camera records in a local SQLite database, and exposes live viewing through RTSP-backed streams. Browser live view uses WebRTC for H.264 camera tracks first, with MJPEG fallback retained for compatibility. It will later communicate with `myseliasan` through a strict device-control protocol.

## Current Scope

- Standalone DB-backed local Basic Auth with a first-run `admin` / `Admin123` seed.
- ONVIF discovery, manual probe, saved-device list, save, camera password change, PTZ move/stop, stream option listing, selected stream URI resolution, RTSP test, WebRTC live view, MJPEG fallback, and delete endpoints under `/api/onvif`.
- Camera-first AI detection rules and alert events under `/api/vision`, backed by reusable `infra/vision` rule, schedule, motion, external-object, line-crossing, multi-line-crossing, and hybrid detection primitives.
- Line-crossing rules support an **Anything** wildcard class (`"*"`) that triggers on any detected YOLO object regardless of label, in addition to the named class list.
- YOLO Inference Tuning in Settings includes a **Best Calibration** button that applies recommended defaults (conf=0.20, IOU=0.35, imgsz=640, maxDet=100, augment on).
- Alert log with server-side filtering by camera ID and unix-timestamp date range; the browser UI defaults to today's alerts and pages 20 at a time.
- NVR recording under `/api/recording`: RTSP-mode rolling `.ts` segment buffer with event-triggered MP4 clip extraction; tick-mode JPEG ring buffer for low-resource devices. Config hot-reload without restart. Live recorder status endpoint.
- Per-camera split-stream configuration: separate `streamUrl` (recording) and live-view URI with `fallbackStreamUrl` automatic switching after repeated connection failures.
- ONVIF stream profile listing and live-view stream selection under `/api/recording/streams`.
- RTSP stream validation through the reusable `infra/rtsp` module.
- Shared RTSP-to-WebRTC sessions through the reusable `infra/stream` module.
- Shared public version and Swagger APIs from the shared app host.
- Runtime Decoder, Live Stream, and local user management settings backed by SQLite.
- Default cache provider is in-process memory.
- Default DB engine is SQLite at `apps/mymatasan/data/mymatasan.db`.
- MyIDSan JWT auth, SSO, RBAC, user, role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, and cache-service APIs are not mounted in `mymatasan`.
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

Default local credentials:

```text
username: admin
password: Admin123
```

Change these from Settings before deploying outside a trusted local development network.

Browser live view uses WebRTC directly from RTSP H.264 RTP packets and does not require an ffmpeg executable for the primary path. MJPEG fallback still uses the configured decoder ffmpeg path. `config.json` provides startup defaults; after first startup, the Settings page persists runtime values in SQLite and changes apply without restart.

WebRTC live view requires the selected camera RTSP stream to expose an H.264 video track. Some camera main streams, including common VIGI profiles, may be configured as H.265/HEVC. When the RTSP test sees video tracks but none are H.264, the browser live view skips WebRTC and uses MJPEG fallback if it is enabled. To use WebRTC for that camera, change the camera stream codec to H.264 or select an H.264 substream from the Stream tab.

```json
"decoder": {
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

The **GPU/device** field in the Settings UI shows a dropdown populated from the detected device list. Choose a device from the list or select **Manual entry…** to type a custom value (useful for non-standard ffmpeg device identifiers). Clicking **Auto Tune** sets the GPU/device automatically based on detected hardware.

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
  }
}
```

Install the YOLO worker dependencies in the Python environment used by the app:

```bash
python -m pip install -r apps/mymatasan/ai/requirements-yolo.txt
```

MyMataSan keeps ML runtime files under `apps/mymatasan/ai` to avoid confusing them with Go/domain object models such as `domain/models`.

Check whether the configured AI tool is ready without downloading anything:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/settings/vision/ai-tool/status"
```

The Settings page exposes the same check. It reports the resolved command path, Python package readiness, worker script, model file, and whether native fallback is available. Users can skip AI downloads; semantic rules such as person, vehicle, animal, fire, and smoke will not produce object-label detections without an AI worker, while native motion and motion-based line crossing can still run.

The bundled worker defaults to `yolo11n.pt`, which can detect COCO labels such as `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`. Fire, smoke, mouse, rat, and other non-COCO labels need a custom YOLO model trained with those labels; set `MYMATASAN_YOLO_MODEL` to that model path before starting the app. For CCTV or IR scenes, start person, vehicle, and animal rules around `threshold: 0.35` and `minFrames: 2`, then tune upward if false positives are too noisy.

Persistent worker stdout can be either an array or an object with `detections` or `objects`. Candidate boxes are normalized from `0` to `1`:

```json
[
  {"label":"person","confidence":0.91,"box":{"x":0.2,"y":0.1,"w":0.3,"h":0.5}}
]
```

## UI

The browser UI is a React 18 single-page application bundled with Webpack 5 into `static/59.js`.

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

## ONVIF API

All app-specific ONVIF routes use HTTP Basic Auth backed by local users stored in SQLite.

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"timeoutMs":3000}' \
  "http://localhost:3000/api/onvif/discover"
```

Discovery upserts WS-Discovery matches into the local database by XAddr and returns best-effort unauthenticated ONVIF metadata such as model, manufacturer, media service URL, RTSP URI, and snapshot URI when the camera allows it. Cameras that require ONVIF credentials may only return host and XAddr until you save credentials and resolve live view.

Manual probe:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"address":"192.168.1.40"}' \
  "http://localhost:3000/api/onvif/probe"
```

List saved devices:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/onvif/devices?limit=50&offset=0"
```

Read live-view stream configuration:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/onvif/stream-config"
```

Read runtime settings:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/settings/runtime"
```

Update runtime settings:

```bash
curl -u admin:Admin123 -X PUT -H "Content-Type: application/json" \
  -d '{"decoder":{"mjpeg":{"ffmpegPath":"ffmpeg","quality":7,"threads":1},"ffmpeg":{"rtspTransport":"tcp","hwaccel":"none","hwaccelDevice":"","initHwDevice":"","videoDecoder":"","probeSize":1000000,"analyzeDuration":1000000,"lowDelay":true,"noBuffer":true}},"stream":{"webrtc":{"enabled":true,"iceServers":[]},"mjpegFallback":{"enabled":true}}}' \
  "http://localhost:3000/api/settings/runtime"
```

Auto-tune decoder runtime settings from saved camera RTSP metadata and local ffmpeg capabilities:

```bash
curl -u admin:Admin123 -X POST \
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
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/stream-options"
```

The response contains `options[]` with `profileToken`, name, encoding, resolution, RTSP URI, and preferred/selected markers. The MyMataSan Stream tab uses this to show stream1/stream2 choices before saving.

Resolve a saved device to an RTSP URI. Omit `profileToken` to save the preferred profile, or pass a token returned by `stream-options` to pin a specific stream:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password","profileToken":"sub"}' \
  "http://localhost:3000/api/onvif/devices/1/stream-uri"
```

Probe the saved RTSP URI:

```bash
curl -u admin:Admin123 -X POST "http://localhost:3000/api/onvif/devices/1/rtsp-test"
```

If a camera returns `406 Not Acceptable` for an ONVIF-provided RTSP URL, stream selection and RTSP test try same-host stream paths derived from the selected profile. For TP-Link/VIGI-style main and sub profiles this means `/stream1` or `/stream2`. When a fallback succeeds, MyMataSan saves the working URL so live view and AI capture keep using it even after switching between stream1 and stream2.

If the RTSP test reports tracks but no H.264 video track, RTSP is reachable but the selected stream cannot be forwarded through the WebRTC path. Keep MJPEG fallback enabled, switch the selected stream to an H.264 substream, or change the camera's selected stream encoding to H.264 in the camera settings.

Change a camera-local ONVIF user password:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"targetUsername":"camera-user","newPassword":"new-camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/camera-password"
```

Move a saved PTZ-capable camera:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"direction":"left","speed":0.35,"durationMs":350}' \
  "http://localhost:3000/api/onvif/devices/1/ptz/move"
```

Stop PTZ movement:

```bash
curl -u admin:Admin123 -X POST "http://localhost:3000/api/onvif/devices/1/ptz/stop"
```

Prepare browser live view from the camera ONVIF media endpoints:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/live-view"
```

Create a WebRTC answer for browser live view:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"type":"offer","sdp":"..."}' \
  "http://localhost:3000/api/onvif/devices/1/webrtc/offer"
```

Open the multipart MJPEG fallback stream:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/onvif/devices/1/live.mjpeg?fps=2"
```

Browser fallback requests add `preferSnapshot=1`, which tries the ONVIF snapshot URI first and then falls back to RTSP-to-MJPEG conversion when snapshots are unavailable. You can force snapshot-only output with `source=snapshot`.

## Recording API

All recording routes use the same local Basic Auth as the other APIs.

Recording enables event-triggered video clips saved around each AI alert. When an alert fires, the monitor sends the alert ID to the recording manager, which assembles a pre-roll buffer (captured before the alert) and a post-roll window (captured after) into a single MP4 file. Pre-roll and post-roll durations are configurable per camera.

Two recording modes are supported:

- **tick** — reuses the JPEG frames the vision monitor already captures (~1–2 fps). Zero extra CPU/network cost. Clip quality matches the monitor frame rate.
- **rtsp** — opens a dedicated full-fps RTSP connection to the camera and writes rolling `.ts` segments. Clip quality matches the camera stream. Requires an extra RTSP connection and an ffmpeg executable.

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
curl -u admin:Admin123 "http://localhost:3000/api/recording/config/1"
```

Create or update a per-camera recording config:

```bash
curl -u admin:Admin123 -X PUT -H "Content-Type: application/json" \
  -d '{"cameraId":1,"enabled":true,"preRollSec":30,"postRollSec":10,"storagePath":"./recordings","retentionDays":7,"segmentMinutes":15,"streamUrl":"","fallbackStreamUrl":""}' \
  "http://localhost:3000/api/recording/config"
```

The response includes a `recorderWarning` field that is non-empty when the recorder could not be hot-reloaded (e.g., no RTSP URI found), so the UI can surface it without treating the config save as a hard error.

Config changes are applied **immediately** without a restart via hot-reload.

List all recording configs:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/recording/config"
```

### Recorder status

Query the live state of all configured recorders:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/recording/status"
```

Each entry in the response array includes:

| Field             | Notes |
|-------------------|-------|
| `cameraId`        | Camera identifier. |
| `state`           | `streaming`, `stopped`, or `error`. |
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
curl -u admin:Admin123 "http://localhost:3000/api/recording/streams/1"
```

Set the camera live-view stream URI to a specific RTSP URL:

```bash
curl -u admin:Admin123 -X POST -H "Content-Type: application/json" \
  -d '{"rtspUrl":"rtsp://user:pass@192.168.1.10/stream1"}' \
  "http://localhost:3000/api/recording/streams/1/live"
```

The Recording tab exposes an **Auto-configure** button that reads ONVIF profiles and automatically sets the main stream for live view and the sub-stream for recording.

### Clips

List recorded clips (filterable by camera, alert, and time range):

```bash
curl -u admin:Admin123 "http://localhost:3000/api/recording/segments?cameraId=1&limit=50&offset=0"
```

Filter clips by alert ID:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/recording/segments?alertId=42"
```

Download a clip:

```bash
curl -u admin:Admin123 -o clip.mp4 \
  "http://localhost:3000/api/recording/segments/1/download"
```

Delete a clip (removes the DB row and the file on disk):

```bash
curl -u admin:Admin123 -X DELETE "http://localhost:3000/api/recording/segments/1"
```

The Recording tab in the browser UI shows the per-camera config form, the live recorder status panel, and lists clips with inline download and delete buttons.

## Vision API

All app-specific vision routes use the same local Basic Auth as ONVIF routes.

The AI page is organized by camera first. Select a saved camera, create or edit rules for that camera, and draw the detection polygon or crossing lines over the live preview. Live-view camera tiles show a visible indicator when recent AI alert events are raised for that camera.

List detection rules:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/vision/rules?limit=50&offset=0"
```

Create or update a rule:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Porch person after hours","detectionType":"person","zonePolygon":"[[0.1,0.1],[0.9,0.1],[0.9,0.8],[0.1,0.8]]","threshold":0.35,"minFrames":2,"cooldownSeconds":30,"soundEnabled":true,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

`zonePolygon` is JSON stored as normalized points from `0` to `1`, where `[0,0]` is the top-left of the video and `[1,1]` is the bottom-right. If no polygon is supplied, the reusable motion detector treats the whole frame as the zone.

Line crossing rules use the same YOLO object candidates as person, vehicle, and animal rules. `line_crossing` triggers when a tracked object crosses any configured line. `multi_line_crossing` triggers only when the same tracked object crosses the configured lines in sequence, up to five lines:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
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
curl -u admin:Admin123 "http://localhost:3000/api/vision/alerts?limit=50&offset=0"
```

Filter alerts to a specific camera and date range (unix timestamps):

```bash
curl -u admin:Admin123 \
  "http://localhost:3000/api/vision/alerts?cameraId=1&createdAfter=1749657600&createdBefore=1749743999&limit=20&offset=0"
```

The Alert Log panel in the browser UI defaults to today's date and pages 20 alerts at a time with Prev/Next buttons. Use the date picker to browse older days or clear the filter to see all dates.

Acknowledge an alert:

```bash
curl -u admin:Admin123 -X POST "http://localhost:3000/api/vision/alerts/1/ack"
```

The monitor captures JPEG frames from the saved camera RTSP or snapshot source, applies the configured detector, then applies threshold/min-frame/cooldown settings before persisting alert events. When `vision.detector.mode` is `external`, `hybrid`, or `persistent`, object candidates from the configured detector process are matched to rule types, zone polygons, thresholds, min-frame counts, line-crossing state, and cooldowns before alert persistence.

Each detection result carries the **frame capture timestamp** (`FrameCapturedAt`), which is passed through to the recording manager as the clip anchor. This means the pre-roll/post-roll window is always centred on when the subject was visible in the frame, not on when YOLO finished processing it. Without this, YOLO's inference latency (100–500 ms per frame) caused recordings to start after the subject had already left, producing empty clips when motion and person detection were both enabled.
