# MyMataSan — System Requirements

This document lists what is needed to **run** MyMataSan, organised as three tiers:

- **Minimal** — the smallest viable host. The app runs and does its core job (a few cameras, light/motion AI, software decode). Expect to keep camera counts low and AI intervals relaxed.
- **Optimal** — the recommended target for a smooth multi-camera appliance with real-time AI and continuous recording.
- **Maximum** — a high-end host for many cameras, GPU-accelerated detection, and **in-app model training**.

Capacity is workload-driven, so treat the camera numbers below as **rough guidance (±50%)**. The app ships a **Capacity estimator** (`GET /api/capacity`) and a **calibration** benchmark (`POST /api/capacity/calibrate`, also in the first-run wizard) that compute the real answer for *your* host — always trust those over the table. See [TECHNICAL_SPEC.md](TECHNICAL_SPEC.md) and [HOWTO.md](HOWTO.md) for the function details referenced here.

---

## 1. Platform & operating system

- **OS:** Windows 10/11, Linux (glibc; Debian/Ubuntu/RaspiOS, etc.), or macOS 12+.
- **Architecture:** x86-64 (amd64) or ARM64 (aarch64) — designed to run on small devices such as **Raspberry Pi 4/5** or Jetson-style micro-computers, up to GPU workstations/servers.
- **Privileges:** a normal user account is enough to run. A few optional features want elevation: hardware decode device passthrough, and the secure-wipe TRIM step (Windows `Optimize-Volume`, Linux `fstrim`) which needs admin/root (failures are downgraded to warnings).
- The server binary is **self-contained Go** — no runtime/VM to install. **Go 1.26+** is only needed if you build from source.

---

## 2. Software dependencies

| Dependency | Needed for | Required? |
|---|---|---|
| **MyMataSan binary** | everything | Always |
| **ffmpeg** | MJPEG fallback live view, RTSP recording mode, AI RTSP/siphon frame capture, non-G.711 live-view audio transcode | **Conditionally required** — see note |
| **Database** (SQLite / PostgreSQL / MariaDB) | all persistence (cameras, users, rules, settings, alerts) | Always (SQLite is built-in, zero-setup) |
| **Cache** (in-memory / Redis) | endpoint/RBAC/rate-limit caching | In-memory by default; Redis optional |
| **Python 3.9–3.13 + `ultralytics`** | YOLO object detection (person/vehicle/animal/fire/smoke, custom models) | Optional — without it, only **native motion** detection runs |
| **NVIDIA GPU + CUDA build of PyTorch** | fast/real-time AI on many cameras, and in-app training | Optional (recommended at scale) |

**ffmpeg note:** Browser live view uses **WebRTC directly from RTSP H.264** and needs *no* ffmpeg for that primary path. ffmpeg becomes required once you use MJPEG fallback, RTSP-mode recording, AI RTSP/siphon capture, or audio transcode for non-G.711 cameras. The app can **install ffmpeg for you** (Settings → Runtime → Decoder → *Download ffmpeg*, or the first-run wizard's System check) on common platforms, or you can point it at an existing binary.

**Database:** SQLite (default, file-based, ideal for an appliance/Pi — nothing to install), PostgreSQL, or MariaDB. The choice is the `db.engine` config; a server engine is only worth it for larger/multi-node deployments.

**AI runtime:** the YOLO worker needs **CPython 3.9–3.13** (PyTorch publishes **no CUDA wheels for 3.14** — see [HOWTO](HOWTO.md)). `ultralytics>=8.3` pulls in `torch` and OpenCV. The bundled `apps/mymatasan/ai/setup.ps1` / `setup.sh` (and the in-app **Install GPU support** button) auto-detect an NVIDIA GPU and install the matching CUDA or CPU build. Without any AI runtime the app still starts and runs **native motion** detection (dependency-free) when `useMotionFallback` is on.

---

## 3. Hardware tiers

> Camera counts assume the defaults the estimator uses: **2 s** detection interval, **640 px** sampled frames, ~**4 Mbps** H.264 per camera for recording, `siphon`/`auto` capture (one continuous decode per camera). Faster intervals, larger frames, or `standalone` capture lower the counts. Recording is a **rolling buffer** — a small disk shortens achievable *retention* rather than capping camera count.

### Minimal

- **CPU:** 4 cores (e.g. Raspberry Pi 4/5, low-end x86).
- **RAM:** 2–4 GB.
- **Storage:** 16 GB for the app/OS + whatever you want for recordings (a few hours/day of one or two cameras fits on a 32–64 GB card; expect short retention).
- **GPU:** none (CPU software decode + motion or light YOLO-nano).
- **Realistic load:** ~1–4 cameras. Live view + recording for a couple of cameras; AI either **native motion** or YOLO-nano at a relaxed interval (3–5 s). Keep `hwaccel: none`, low ffmpeg `threads`.
- **DB/cache:** SQLite + in-memory cache.

### Optimal (recommended)

- **CPU:** 6–8+ modern cores (mini-PC/NUC, Ryzen/Core i5–i7, Apple Silicon).
- **RAM:** 8–16 GB.
- **Storage:** SSD for the app/DB; a dedicated HDD/SSD or volume sized for your retention target (see §6). NVMe recommended if recording many cameras.
- **GPU:** optional but recommended — an entry/mid NVIDIA card (or integrated VAAPI/QSV/D3D11VA for *decode*) lifts camera count and keeps AI real-time.
- **Realistic load:** ~6–16 cameras with continuous recording and YOLO detection at the 2 s default. With a discrete GPU for decode + inference, the headline limiter usually moves from CPU decode to disk/retention.
- **DB/cache:** SQLite is fine; PostgreSQL/MariaDB + Redis if you want a server-grade backend.

### Maximum

- **CPU:** 12+ cores / server class.
- **RAM:** 32 GB+.
- **Storage:** NVMe for DB + active recording, plus large HDD array for retention; size by §6.
- **GPU:** discrete **NVIDIA** GPU with **CUDA 12.4+** (Blackwell / RTX 50-series needs **CUDA 12.8+**). Required for **in-app training** (the CUDA build of PyTorch) and for the highest concurrent-camera AI throughput.
- **Realistic load:** many cameras (20+; GPU NVDEC decode sessions become the practical ceiling, modelled at ~32/GPU), GPU-accelerated detection, custom-model training, and long retention. Run a server engine (PostgreSQL/MariaDB) + Redis.

---

## 4. Requirements by function

- **Browser live view (primary):** an H.264 RTSP track on the camera + a modern browser (WebRTC). No ffmpeg needed. **MJPEG fallback** (for H.265/non-H.264 cameras or WebRTC disabled) **needs ffmpeg**.
- **Live-view audio:** G.711 (PCMA/PCMU) plays natively; non-G.711 (e.g. AAC) is transcoded to Opus, which **needs ffmpeg**.
- **Recording (NVR):** continuous + event clips. `tick`/`siphon` clip modes reuse decoded frames cheaply; **`rtsp` clip mode needs ffmpeg** and a spare camera connection. Needs disk sized for retention (§6).
- **AI detection:** native **motion** is dependency-free. **Object detection** (person, vehicle, animal, fire/smoke, custom) needs the **Python + ultralytics** runtime; a **CUDA GPU** makes it real-time at scale.
- **Custom model training (in-app):** needs an **NVIDIA GPU + the CUDA build of PyTorch** (CPU training is possible but slow). See [HOWTO](HOWTO.md) and the GPU setup notes.
- **Encryption at rest** (default on): no extra hardware; AES-256-GCM benefits from **AES-NI** (standard on modern x86; present on ARMv8). See the `security` config.
- **Notifications:** Webhook/Telegram/MQTT delivery needs outbound network reachability to the destination (and a broker for MQTT).
- **Factory reset / secure wipe:** TRIM-based secure erase wants admin/root; otherwise it degrades to best-effort. Crypto-erase (key destroy) works regardless.

---

## 5. Network & cameras

- **Cameras:** ONVIF-discoverable IP cameras exposing **RTSP** (H.264 preferred for WebRTC; H.265/MJPEG supported via fallback). Most need credentials.
- **LAN:** cameras and host on the same reachable network; enough bandwidth for the RTSP streams (≈ the sum of per-camera bitrates, typically 1–8 Mbps each).
- **Ports:** the app serves HTTP/HTTPS on the configured `server` ports (TLS optional). Health/readiness on `/health`, `/ready`.
- **Internet:** only needed for optional one-time downloads — base YOLO model weights on first use, the ffmpeg installer, and AI dependency installs. The running appliance does not require internet.

---

## 6. Storage sizing for recordings

Continuous recording disk use is roughly:

```
GB/day per camera ≈ bitrate_Mbps × 10.8        (Mbps × 86400 s ÷ 8 ÷ 1000)
total ≈ Σ(per-camera GB/day) × retention_days
```

Example: 8 cameras @ 4 Mbps for 7 days ≈ `8 × (4 × 10.8) × 7` ≈ **2.4 TB**. Footage auto-purges at the retention limit, so a smaller disk simply yields **shorter achievable retention** (the capacity estimator reports this, with a ~1-day minimum floor) rather than failing. Machine-health disk mitigation can purge early and pause recording before a disk fills.

---

## 7. Quick checklist

1. Pick a host from the tier table (or just install and run the **Capacity estimator** / first-run **wizard** to size it).
2. Ensure **ffmpeg** is available if you need MJPEG/recording-RTSP/siphon/audio — or let the in-app installer fetch it.
3. Decide the **database** (SQLite for an appliance; PostgreSQL/MariaDB for servers) and **cache** (in-memory vs Redis).
4. For object detection, install the **Python 3.9–3.13 + ultralytics** runtime (GPU build if you have an NVIDIA card); otherwise rely on motion.
5. Size **storage** for your camera count × bitrate × retention (§6).
6. Confirm **`/health`** and **`/ready`** are green (Settings → Version & Health) once running.
