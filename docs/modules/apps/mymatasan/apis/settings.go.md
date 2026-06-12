# Module: apps/mymatasan/apis/settings.go

## Purpose

Registers runtime settings routes for standalone `mymatasan`.

## Routes

- `GET /api/settings/runtime`: return current decoder and live stream settings.
- `PUT /api/settings/runtime`: save decoder and live stream settings without restart.
- `POST /api/settings/runtime/auto-tune`: inspect saved camera RTSP metadata, local ffmpeg hardware acceleration capabilities, and detected GPU devices, then save recommended decoder settings.
- `GET /api/settings/runtime/gpu-devices`: detect and return available GPU/device options for ffmpeg hardware acceleration on the current platform. Used by the Settings UI to populate the GPU/device dropdown.
- `POST /api/settings/runtime/reset`: restore startup config defaults into the runtime settings row.
- `GET /api/settings/users`: list standalone local users.
- `POST /api/settings/users`: create a standalone local user.
- `PUT /api/settings/users/{id}`: update user profile, admin flag, and active flag.
- `POST /api/settings/users/{id}/password`: reset a local user's password.
- `DELETE /api/settings/users/{id}`: delete a local user.

## Notes

- Routes are mounted behind the app-level local Basic Auth middleware.
- Runtime settings are persisted in SQLite through `RuntimeSetting`.
- Decoder auto-tune runs GPU device detection as part of its environment scan and selects the best available hardware decoder and device automatically. It saves settings immediately and returns the applied settings plus observations explaining each decision.
- On Linux the auto-tune detects container environments (Docker, containerd, Kubernetes, LXC) and includes device-passthrough instructions in the observations when no GPU can be confirmed.
- `GET /api/settings/runtime/gpu-devices` returns DXGI-ordered adapter indices on Windows (matching Task Manager numbering), VAAPI render node paths and CUDA indices on Linux, and VideoToolbox display names on macOS.
- User management routes require an authenticated admin local user.
