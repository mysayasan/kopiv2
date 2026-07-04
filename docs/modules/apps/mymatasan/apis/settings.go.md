# Module: apps/mymatasan/apis/settings.go

## Purpose

Registers runtime settings routes for standalone `mymatasan`.

## Routes

- `GET /api/settings/runtime`: return current decoder and live stream settings.
- `PUT /api/settings/runtime`: save decoder and live stream settings without restart.
- `POST /api/settings/runtime/auto-tune`: inspect saved camera RTSP metadata, local ffmpeg hardware acceleration capabilities, and detected GPU devices, then save recommended decoder settings.
- `GET /api/settings/runtime/gpu-devices`: detect and return available GPU/device options for ffmpeg hardware acceleration on the current platform. Used by the Settings UI to populate the GPU/device dropdown.
- `POST /api/settings/runtime/reset`: restore startup config defaults into the runtime settings row.
- `GET /api/settings/decoder/status`: report whether a usable ffmpeg is available (`found`, `path`, `version`). Powers the FFmpeg-path status icon/tooltip and "Check" button in Settings, and the setup wizard's video-engine check.
- `POST /api/settings/decoder/ffmpeg/install`: start the background ffmpeg download/install job and return its initial state. Admin-only (a write).
- `GET /api/settings/decoder/ffmpeg/install/status`: poll the ffmpeg install job (`running`, `status`, `log`, `path`, `supported`).
- `GET /api/settings/vision/ai-runtime/status`: report whether the self-contained AI Python runtime (Python + torch + ultralytics) is installed, its path, and whether CUDA is available (`services.PythonInstaller.PythonStatus`).
- `POST /api/settings/vision/ai-runtime/install`: start the background download + pip install of the AI runtime (a self-contained Python plus a GPU or CPU PyTorch build and ultralytics), persisting the resolved interpreter into `vision.detector.command` on success. Admin-only (a write).
- `GET /api/settings/vision/ai-runtime/install/status`: poll the AI-runtime install job (`running`, `status`, `log`, `python`, `supported`).
- `GET /api/settings/fs/browse`: server-side directory picker used to choose the ffmpeg binary. Returns one directory level (`path`, `parent`, `separator`, `entries[]` of `{name, path, dir}`) for the `path` query param. Admin-only and read-only — names only, never file contents. Browsing is confined to a whitelist of roots (see `services/filesystem_browse.go`); an empty/out-of-whitelist `path` lists the allowed roots.
- `GET /api/settings/notification`: return current notification settings (destinations, retention, and legacy singleton fields).
- `PUT /api/settings/notification`: save the full notification settings blob.
- `PUT /api/settings/notification/destination`: upsert a single delivery destination (create when the body has no `id`, otherwise replace the destination with that `id`) without touching other destinations or the retention section. Returns `{destination, settings}`.
- `DELETE /api/settings/notification/destination/{id}`: remove one delivery destination by id; a no-op if the id is unknown.
- `PUT /api/settings/notification/retention`: save only the retention section, leaving destinations and legacy singleton fields untouched.
- `POST /api/settings/notification/test`: send a test notification through configured destinations.
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
- The ffmpeg installer routes share the background `services.FFmpegInstaller` with the first-run setup wizard; on success the installer persists the resolved path into runtime settings. The Settings UI offers a "Restart now" button afterwards so the new path takes effect everywhere. `app.go` constructs it with `binDir = deps.DataDir/bin` (an absolute, writable path), not a CWD-relative `bin/`, so it installs correctly under a packaged Windows service (CWD `C:\Windows\System32`).
- The AI-runtime installer routes are backed by `services.PythonInstaller` (constructed in `app.go` from `deps.DataDir`/`deps.ConfigPath`) and are `nil`-guarded (`ErrInternalServerError`) the same way the ffmpeg installer is if unavailable. It is a separate, self-contained runtime from the Train-in-app "Install GPU support" flow (`services/training_runner.go`), which instead upgrades the Python the detector already uses.
- `GET /api/settings/fs/browse` whitelisted roots are: the app working directory and its `bin/`, the user home, OS-specific common install locations, plus any extra paths from the `decoder.browseRoots` config array (passed to `NewSettingsApi`). User management and the filesystem-browse route require an authenticated admin local user.
