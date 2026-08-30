# Module: apps/mymatasan/services/face_models_install.go

## Purpose

`FaceModelsInstaller` probes and installs the face-recognition prerequisites from inside the app:
the two OpenCV model-zoo `.onnx` files and, when the detector's interpreter lacks it, the
`opencv-python` package that loads them.

It exists because the enrolment failure it repairs used to be a dead end. `pythonFaceEmbedder`
returns *"face models are not installed — run the face-recognition setup to download them"*, and the
setup it named was `ai/setup.ps1 -Faces`, a PowerShell script in the source tree. That is a true
sentence and a useless one for somebody standing in a browser trying to enrol a face. **An error
without a route to the fix is a half-built feature**, so the route is a button — the same shape as
the ffmpeg installer (`ffmpeg_install.go`) and the AI-runtime installer (`python_install.go`): a
status probe, a background job with a live log, and a poll endpoint.

## What it installs

| Model | Role | Licence | Size | Also used by |
|-------|------|---------|------|--------------|
| `face_detection_yunet_2023mar.onnx` | detection | MIT | ~230 KB | the evidence-export face blur, so it is often already present |
| `face_recognition_sface_2021dec.onnx` | recognition | Apache-2.0 | ~37 MB | enrolment and live matching only |

Only what is **missing** is fetched. The common real state is "detector present, recognizer
missing", which is why the status is per-file rather than a single ready/not-ready flag.

## Status (`GET /api/faces/models`)

`FaceModelsStatus` answers the whole question, not just the file one:

- `models[]` — each file's role, name, presence and size on disk.
- `worker` — whether `faces_worker.py` is in the install at all.
- `python` — the interpreter the detector actually runs on (resolved per call from
  `vision.detector.command` in the config FILE, so a Python installed after boot is the one probed),
  its opencv version, and whether that opencv exposes `FaceDetectorYN`/`FaceRecognizerSF`. **A host
  can have both models and still fail**, because the models are loaded by opencv — that is a
  different repair, and the status says which one is needed.
- `ready` — worker AND both models AND a usable opencv.

## Install (`POST /api/faces/models/install`, polled via `/install/status`)

1. `pip install opencv-python numpy` on the detector's interpreter, only when the probe says the
   face API is absent. Re-probed afterwards: an opencv that installs but is too old to expose the
   face classes is reported as such rather than treated as success.
2. Download each missing model to `<file>.part`, check it, and only then rename into place — a
   failed download never leaves a partial file that `faceFileExists` would report as installed.
3. Load both models with the real `cv2` calls the worker makes. A file of the right size is not
   proof it is the right file.

`StartInstall` checks the model directory is **writable before anything is downloaded**: 37 MB is a
long way to travel to learn the destination is read-only.

## checkFaceModelFile

Rejects the two ways this download succeeds and is still not a model, both of which arrive as a
valid HTTP 200:

- a **git-LFS pointer** — a ~130-byte text stub GitHub serves for LFS-tracked files through some URL
  forms. Named explicitly, because "too small" would send somebody to look at their network instead
  of the URL.
- a **truncated transfer**, caught by a floor size well under the real one (the check is not a
  version pin).

Unit-tested in `face_models_install_test.go` along with the per-file status and the
write-check-first rule.

## Notes

- Model files are `.gitignore`d (`apps/mymatasan/ai/*.onnx`) and fetched, never committed.
- The install needs outbound access to `github.com`. There is no bundled copy and no mirror; an
  air-gapped appliance uses `setup.ps1 -Faces` on a connected machine and a file copy.
- No restart is needed afterwards: `Embed` checks for the files per call, and the live worker
  reloads when the gallery is rebuilt on the next enrolment.
- Surfaced in the UI by `FaceModelsSetup` (People screen, and Settings › AI via
  `FaceModelsControl`), which renders nothing at all when `ready` is true.
