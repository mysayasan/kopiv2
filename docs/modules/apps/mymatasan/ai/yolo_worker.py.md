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

## Appearance search stage (`_appearance_embed`, W3-2)

- Gated per-request on `request["appearance"]` (set only when the sampling camera has
  `AppearanceEnabled` on — see `apps/mymatasan/services/vision_monitor.go.md`'s *Appearance
  capture gate* and `apps/mymatasan/services/appearance_search.go.md`).
- Runs on the **shared** resnet18 embedder, `_crop_backbone()` — renamed from
  `_anomaly_backbone()` because it now serves **two** stages: taught-anomaly scoring (compares
  a ROI against a memory bank of normal embeddings) and appearance search (embeds person/vehicle
  crops so "find more like this" has something to rank). The globals were renamed to match
  (`_CROP_BACKBONE`/`_CROP_PREP`/`_CROP_BACKBONE_DISABLED`, formerly `_ANOMALY_*`); behaviour is
  unchanged — same lazy-load-once, same CUDA placement, same disable-on-failure sentinel. Sharing
  is deliberate: it is already a dependency of the anomaly feature, so appearance search needs no
  additional model download, which matters because this product is deployed into networks with
  no egress.
- It is **not** a person re-identification network, and appearance search says so rather than
  implying otherwise (see `apps/mymatasan/services/appearance_search.go.md`'s *What this is and
  is not*): ImageNet features separate coarse appearance (clothing colour, shape, vehicle type)
  well and are markedly weaker at matching the same person across large changes in pose or
  lighting. Every vector is stamped with the model name that produced it (`APPEARANCE_MODEL =
  "resnet18-hsv-560"`) so a stronger model can replace this later without old
  vectors being silently compared against new ones.
- Enriches **existing** detections; never invents one. Eligible detections are filtered to
  `APPEARANCE_LABELS` (`person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `train`,
  `boat`), a confidence floor (`APPEARANCE_MIN_CONFIDENCE = 0.45`), and a minimum box-area
  fraction (`APPEARANCE_MIN_BOX_FRACTION = 0.015` — a distant figure a dozen pixels tall embeds
  to something that matches everything, which is worse than no result). The biggest-boxed
  survivors are kept up to `APPEARANCE_MAX_PER_FRAME` (8) per frame, so a crowd scene runs a
  bounded number of forward passes rather than one per person in frame.
- Crops are batched into **one** `torch.no_grad()` forward pass per frame (not one call per
  crop) concatenated with a two-band hue/lightness histogram, L2-normalised, and attached to each kept detection as `appearance: [float, ...]` (560-d: 512 shape + 48 colour)
  + `appearanceModel: "resnet18-hsv-560"`.

  The colour half is not an optimisation, it is the half that discriminates. Measured on the
  real model the embedding alone separated two crops of the same subject (0.9825) from a red
  figure against a blue one (0.9498) by just 0.033 — an ImageNet backbone is trained for
  CLASS invariance and answers "person" whatever they are wearing. Colour alone separates the
  same pair by 0.115. The weight between the two halves is held at 1.0 because the bench
  scene flatters colour and penalises shape; tuning it needs real footage.
- Runs **last**, on the merged detection list (after `_merge`, LPR, anomaly and face), inside the
  same `try` block that holds the temp JPEG open — after the earlier stages so a custom-model
  detection that replaced a stock one is the thing described, and inside the `try` so the file is
  guaranteed to still exist for it (moving the `unlink` out to keep the file alive would leak a
  JPEG per frame the moment any earlier stage raises, which on a camera failing repeatedly fills
  the temp directory).
- Any failure (missing torch/PIL, bad crop, OOM) is caught, logged to stderr, and returns zero
  embedded detections for that frame rather than failing the whole detection response — the same
  fail-open shape every other optional stage in this file uses.

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
