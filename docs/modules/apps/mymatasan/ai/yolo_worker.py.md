# Module: apps/mymatasan/ai/yolo_worker.py

## Purpose

Runs Ultralytics YOLO as a persistent MyMataSan detector worker.

Companion script `train_worker.py` is a one-shot trainer used by the in-app
training feature; it runs `ultralytics` `.train()` on an exported dataset and
streams JSON progress to stdout (see the Custom Model Training API).

Companion script `faces_worker.py` is a one-shot **enrollment** worker (embed one
photo, print JSON, exit) — this file's `_faces_detect` is the **live** counterpart
that runs on every sampled frame from a camera with an active face rule. Both
share `face_model.py` (YuNet detect + SFace embed via `cv2.FaceDetectorYN`/
`cv2.FaceRecognizerSF`) so an enrolled faceprint and a live faceprint are
directly comparable. See `apps/mymatasan/services/face_gallery.go.md` and
`apps/mymatasan/services/face_embedder.go.md` for the Go side; neither
`face_model.py` nor `faces_worker.py` has its own module doc (same convention as
`anomaly_worker.py`/`train_worker.py`/`eval_worker.py` — only this file, the
always-running worker, is individually documented).

## Responsibilities

- Load the configured YOLO model once at process startup.
- Read newline-delimited JSON frame requests from stdin.
- Decode base64 JPEG bytes into a temporary image file for YOLO inference.
- Convert YOLO results into normalized object candidates for Go.
- Write one compact JSON response per request to stdout.

## Face recognition stage (`_faces_detect`)

- Gated per-request on `request["face"]` (set only when the sampling camera has an active
  `detectionType: "face"` rule — see `apps/mymatasan/services/vision_monitor.go.md`'s *Face capture
  path*) **and** the YuNet model file being present; when either is false the stage is skipped with
  zero overhead.
- `_get_face_model()` lazily builds the shared `FaceModel` (`face_model.py`) once and caches a
  `False` sentinel on failure (missing opencv, missing/corrupt `.onnx`) so a broken install doesn't
  retry model-load on every frame — it just runs with faces disabled.
- `_get_face_gallery()` hot-reloads `faces_gallery.json` (path from `MYMATASAN_FACES_FILE`, written
  by `FaceGalleryService.rebuildGallery`) by `mtime`, and pre-normalizes every enrolled person's
  embedding matrix to unit vectors so a live match is one dot product per person = cosine similarity.
- For each face YuNet finds in the frame: embed with SFace, unit-normalize, take the best cosine
  similarity across every enrolled person's embeddings, and emit one `"face"` candidate — recognized
  (`personId`/`personName` set) when the best score is `>= MYMATASAN_FACE_MIN_COS` (default `0.40`,
  SFace's own "same" floor is ~0.36; this NVR defaults higher because naming the wrong person is
  worse than naming nobody), otherwise unknown (`personId: 0`, `personName: ""`). The Go rule
  (`infra/vision/face.go`) applies a **second**, per-rule `minConfidence` floor on top of this one.
- Reuses the **same** captured frame already decoded for stock/LPR/anomaly detection — enabling faces
  on a camera adds only the detect+embed compute, no extra frame grab.
- Any failure (bad gallery JSON, opencv error) is caught, logged to stderr, and returns no face
  candidates for that frame rather than failing the whole detection response.

## Notes

- Install Python dependencies from `apps/mymatasan/ai/requirements-yolo.txt`. Face recognition is a
  separate optional dependency set (`requirements-face.txt` — `opencv-python`+`numpy`, downloads the
  two `.onnx` models). **Windows only for now**: `setup.ps1 -Faces` installs deps and downloads the
  models; `setup.sh` (Linux/macOS/Pi) has no `--faces` equivalent yet, so those hosts need the pip
  install and the two `opencv_zoo` model downloads done by hand until that's added. Dormant either
  way until both `.onnx` files are present and at least one person is enrolled via `/api/faces`.
- Default model is `yolo11n.pt`, which detects COCO classes such as `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`.
- Fire, smoke, mouse, rat, and other non-COCO labels require a YOLO model trained for those classes; set `MYMATASAN_YOLO_MODEL` to that model path, or train/import and **activate** a custom model from the Training tab.
- **Parallel stock + custom models, custom takes priority.** The worker always loads a **stock** model (`MYMATASAN_YOLO_MODEL` env, else bundled `yolo11n.pt`) and, when a custom model is active, **also** loads it from the active-model pointer file (`MYMATASAN_ACTIVE_MODEL_FILE`, default `active_model.txt` next to this script). Both run on every frame and their detections are merged by `_merge()`: custom (taught) detections are kept first (same-label dupes collapsed at IoU > 0.55); a stock detection is then dropped if a kept custom detection overlaps its box at IoU > 0.55, **regardless of label** — e.g. stock `dog` + custom `cat` on the same animal collapses to just `cat`, one Object Search row instead of two. Stock detections the custom model does not overlap are kept untouched, so activating a skill never blinds stock classes elsewhere in the frame. Output stays confidence-sorted. Because `_merge()` output feeds the whole detection pipeline, this precedence also applies to **AI alert-rule evaluation**: a taught skill overriding a stock label means a rule targeting the stock class won't fire on that object (e.g. a stock "dog" rule won't fire on an object the taught model calls "cat"). Activate/deactivate restarts the worker to reload the pair; only one custom model is active at a time. A custom-model load failure does not take down stock detection.
- Optional environment variables: `MYMATASAN_YOLO_CONF`, `MYMATASAN_YOLO_DEVICE`, and `MYMATASAN_YOLO_IMGSZ`.
- CCTV and IR frames often produce useful detections below `0.75`; MyMataSan semantic-rule UI defaults start at `threshold: 0.35` and `minFrames: 2`.
- stdout is reserved for protocol JSON; model logs are redirected to stderr.
