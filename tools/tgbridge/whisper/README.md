# Local Whisper server for tgbridge voice→task

Runs [faster-whisper-server](https://github.com/fedirz/faster-whisper-server) in
Docker, exposing an **OpenAI-compatible** `/v1/audio/transcriptions` endpoint.
tgbridge points its STT config at it, so voice notes are transcribed locally —
accurate *and* private (nothing leaves your PC).

Two ways to run it: **`docker run`** (no Compose needed — use this if `docker
compose` isn't available) or **Docker Compose**.

## Prerequisites
- Docker installed and running (`docker --version`).
- GPU only: NVIDIA driver (CUDA 12.x) + NVIDIA Container Toolkit / WSL2 GPU.

### GPU preflight (before the GPU command)
Confirm Docker can see your card:
```
docker run --rm --gpus all nvidia/cuda:12.4.0-base-ubuntu22.04 nvidia-smi
```
- **GPU table shows (RTX 5080 listed)** → use the GPU command below.
- **`could not select device driver "nvidia"`** → GPU isn't wired into Docker:
  update the NVIDIA driver, enable WSL2 integration in Docker Desktop, install the
  NVIDIA Container Toolkit, restart Docker, re-run this. Or just use the CPU command.

---

## Run with `docker run` (no Compose)

**GPU (recommended for large-v3):**
```
docker run -d --name whisper-server --gpus all -p 8000:8000 -v whisper-models:/root/.cache/huggingface --restart unless-stopped fedirz/faster-whisper-server:latest-cuda
```

**CPU (any machine):**
```
docker run -d --name whisper-server -p 8000:8000 -v whisper-models:/root/.cache/huggingface --restart unless-stopped fedirz/faster-whisper-server:latest-cpu
```

Manage it:
```
docker logs -f whisper-server      # watch logs / model download
docker stop whisper-server         # stop
docker start whisper-server        # start again
docker rm -f whisper-server        # remove (before switching CPU<->GPU image)
```

> Switching between GPU and CPU images? Remove the old container first:
> `docker rm -f whisper-server`, then run the other command.

---

## Or run with Docker Compose (if you have it)

| File | Runtime | Command |
|---|---|---|
| `docker-compose.yml` | GPU | `docker compose up -d` |
| `docker-compose.cpu.yml` | CPU | `docker compose -f docker-compose.cpu.yml up -d` |

Stop with `docker compose down`. (No `docker compose` command? Update/reinstall
Docker Desktop — Compose v2 is bundled — or just use `docker run` above.)

---

## Point tgbridge at it
`tools/tgbridge/config.json` (already set):
```json
"stt_api_url": "http://127.0.0.1:8000/v1/audio/transcriptions",
"stt_model":   "Systran/faster-whisper-large-v3",
"stt_api_key": ""
```
Then in Telegram: `/voicemode` → task, and send a voice note.

## Choosing a model (set in config, not here)
The server loads whatever model each request names, so switch quality via
`stt_model`:

| stt_model | Runtime | Notes |
|---|---|---|
| `Systran/faster-whisper-large-v3` | GPU | best accuracy (recommended on your GPU) |
| `Systran/faster-whisper-medium`   | GPU/CPU | good balance |
| `Systran/faster-whisper-small`    | CPU | fast, low RAM (good CPU default) |

English-only variants (e.g. `...-small.en`) are slightly better if you only speak English.

## Verify
```
curl http://127.0.0.1:8000/v1/models
```
First transcription downloads the model (cached in the `whisper-models` volume) —
slow once, fast after. If the server is down, tgbridge automatically falls back
to the built-in Windows recognizer.
