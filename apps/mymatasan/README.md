# mymatasan

`mymatasan` is the standalone camera and video intelligence app for `kopiv2`.

It is designed to run on small devices such as Raspberry Pi or Jetson-style micro computers. It discovers ONVIF cameras, persists camera records in a local SQLite database, and exposes live viewing through RTSP-backed streams. Browser live view uses WebRTC for H.264 camera tracks first, with MJPEG fallback retained for compatibility. It will later communicate with `myseliasan` through a strict device-control protocol.

## Current Scope

- Standalone DB-backed local Basic Auth with a first-run `admin` / `Admin123` seed.
- ONVIF discovery, manual probe, saved-device list, save, camera password change, PTZ move/stop, stream URI resolution, RTSP test, WebRTC live view, MJPEG fallback, and delete endpoints under `/api/onvif`.
- Camera-first AI detection rules and alert events under `/api/vision`, backed by reusable `infra/vision` rule, schedule, and motion-detection primitives.
- RTSP stream validation through the reusable `infra/rtsp` module.
- Shared RTSP-to-WebRTC sessions through the reusable `infra/stream` module.
- Shared public version and Swagger APIs from the shared app host.
- Runtime Decoder, Live Stream, and local user management settings backed by SQLite.
- Default cache provider is in-process memory.
- Default DB engine is SQLite at `apps/mymatasan/data/mymatasan.db`.
- MyIDSan JWT auth, SSO, RBAC, user, role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, and cache-service APIs are not mounted in `mymatasan`.
- App-specific OpenAPI descriptions for ONVIF, RTSP setup, live view, and vision endpoints.

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

```json
"decoder": {
  "mjpeg": {
    "ffmpegPath": "ffmpeg"
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

Use an absolute path when the service process cannot find ffmpeg from `PATH`, for example `C:\\ffmpeg\\bin\\ffmpeg.exe` on Windows or `/usr/bin/ffmpeg` on Linux.

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
  -d '{"decoder":{"mjpeg":{"ffmpegPath":"ffmpeg"}},"stream":{"webrtc":{"enabled":true,"iceServers":[]},"mjpegFallback":{"enabled":true}}}' \
  "http://localhost:3000/api/settings/runtime"
```

Resolve a saved device to an RTSP URI:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"username":"camera-user","password":"camera-password"}' \
  "http://localhost:3000/api/onvif/devices/1/stream-uri"
```

Probe the saved RTSP URI:

```bash
curl -u admin:Admin123 -X POST "http://localhost:3000/api/onvif/devices/1/rtsp-test"
```

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

## Vision API

All app-specific vision routes use the same local Basic Auth as ONVIF routes.

The AI page is organized by camera first. Select a saved camera, create or edit rules for that camera, and draw the detection polygon over the live preview. Live-view camera tiles show a visible indicator when recent AI alert events are raised for that camera.

List detection rules:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/vision/rules?limit=50&offset=0"
```

Create or update a rule:

```bash
curl -u admin:Admin123 -H "Content-Type: application/json" \
  -d '{"cameraId":1,"name":"Porch person after hours","detectionType":"person","zonePolygon":"[[0.1,0.1],[0.9,0.1],[0.9,0.8],[0.1,0.8]]","threshold":0.75,"minFrames":3,"cooldownSeconds":30,"soundEnabled":true,"isEnabled":true}' \
  "http://localhost:3000/api/vision/rules"
```

`zonePolygon` is JSON stored as normalized points from `0` to `1`, where `[0,0]` is the top-left of the video and `[1,1]` is the bottom-right. If no polygon is supplied, the reusable motion detector treats the whole frame as the zone.

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

List alert events:

```bash
curl -u admin:Admin123 "http://localhost:3000/api/vision/alerts?limit=50&offset=0"
```

Acknowledge an alert:

```bash
curl -u admin:Admin123 -X POST "http://localhost:3000/api/vision/alerts/1/ack"
```

The current detector is an MVP motion-based detector. It captures JPEG frames from the saved camera RTSP or snapshot source, compares consecutive frames inside the selected polygon, applies threshold/min-frame/cooldown settings, and persists alert events. The reusable `infra/vision` boundary is intentionally app-neutral so later fire, smoke, person, vehicle, or model-backed detectors can be plugged in without binding them to `mymatasan`.
