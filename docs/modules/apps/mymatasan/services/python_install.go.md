# Module: apps/mymatasan/services/python_install.go

## Purpose

In-app installer for a **self-contained AI Python runtime** — a standalone Python interpreter plus PyTorch (GPU or CPU build) and `ultralytics` — for hosts that have no usable system Python for the YOLO detector at all. Distinct from the Train-in-app "Install GPU support" flow (`services/training_runner.go`), which swaps an *existing* Python installation for a CUDA-capable one; `PythonInstaller` downloads a complete, isolated interpreter so `vision.detector.command` can be set up from nothing.

## Responsibilities

- `PythonInstaller` (`NewPythonInstaller(dataDir, configPath)`) — one install runs at a time; the UI polls status. `dataDir` is the writable state root (the runtime lands under `<dataDir>/pyruntime`); `configPath` is the app config file the resolved interpreter path is written back to.
- `PythonStatus(ctx)` — probes the installed interpreter (if any) for `torch`/`ultralytics`/CUDA availability via a short `python -c` script; returns `PythonRuntimeStatus{Found, Python, Torch, CUDA, Ultralytic}`.
- `StartInstall(ctx)` / `InstallStatus()` — background job pattern mirroring `services.FFmpegInstaller`: `StartInstall` errors immediately when unsupported for the OS/arch (`pythonDownloadURL`) or an install is already running; `InstallStatus()` returns `PythonInstallState{Running, Status, Log, Python, Supported}` for polling.
- `run(url, exeRel)` — the background pipeline:
  1. Fresh-extracts a pinned astral `python-build-standalone` `install_only` tarball (`pyStandaloneVersion`/`pyStandaloneRelease`, currently 3.12.7/20241016) into `<dataDir>/pyruntime/python` (removes any prior extract so a retry can't mix versions).
  2. `hasNvidiaGPU()` (via `nvidia-smi -L`) selects the PyTorch wheel index: `torchIndexCUDA` (cu128, covers Blackwell/RTX 50-series and older with a recent driver) when a GPU is present, else `torchIndexCPU`.
  3. Runs `pip install --upgrade pip`, then `pip install torch torchvision --index-url <index>`, then `pip install ultralytics`, streaming combined stdout/stderr into the live log.
  4. Verifies by importing `torch`/`ultralytics` in the new interpreter.
  5. `persistCommand(python)` — read-modify-write `configPath` as a generic `map[string]any`, setting `vision.detector.command` to the new interpreter path while preserving every other field.
- `pythonDownloadURL(goos, goarch)` — pure (no I/O, unit-testable) mapping of `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64` to an astral release triple and archive URL; any other OS/arch returns an error (`Supported: false`).
- `extractTarGz` — shared tar.gz extractor (path-traversal-checked; also reused by `services/update.go` for `.tar.gz` release archives) that recreates symlinks (the standalone Python archive ships `python3 -> python3.12`-style links).

## Notes

- Exposed by `apis/settings.go`: `GET /api/settings/vision/ai-runtime/status`, `POST /api/settings/vision/ai-runtime/install` (admin-write), `GET /api/settings/vision/ai-runtime/install/status`.
- The download can be large (200 MB – 2.5 GB for the CUDA torch build); the HTTP client has a 30-minute timeout.
- A restart is required after a successful install for the detector to pick up the new `vision.detector.command`.
- The stock `yolo11n.pt` model is bundled in every distribution channel (archives, `.deb`/`.rpm`, Windows installer — see `.goreleaser.yaml`, `packaging/stage-archive.sh`, `packaging/windows/mymatasan.iss`), so only the heavy Python/torch runtime is fetched by this installer, not the model weights.
