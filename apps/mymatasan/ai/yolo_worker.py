#!/usr/bin/env python3
"""Persistent YOLO worker for MyMataSan.

Protocol:
  stdin  JSON lines: {"cameraId": 1, "format": "jpeg", "image": "<base64>"}
  stdout JSON lines: [{"label":"person","confidence":0.91,"box":{"x":0.1,"y":0.2,"w":0.3,"h":0.4}}]
"""

from __future__ import annotations

import base64
import contextlib
import json
import os
import sys
import tempfile
from pathlib import Path
from typing import Any


SCRIPT_DIR = Path(__file__).resolve().parent


def _stock_model_path() -> str:
    """The base model that ALWAYS runs (general/stock detection), in priority:

    1. MYMATASAN_YOLO_MODEL env (explicit override, file path).
    2. The stock-model pointer file (chosen in Settings -> AI) — its content is a
       model path or an ultralytics model name (downloaded on demand).
       MYMATASAN_STOCK_MODEL_FILE env or stock_model.txt next to this script.
    3. The bundled yolo11n.pt default.
    """
    explicit = os.environ.get("MYMATASAN_YOLO_MODEL", "").strip()
    if explicit and Path(explicit).is_file():
        return explicit
    pointer = os.environ.get("MYMATASAN_STOCK_MODEL_FILE", "").strip()
    pointer_path = Path(pointer) if pointer else (SCRIPT_DIR / "stock_model.txt")
    try:
        if pointer_path.is_file():
            chosen = pointer_path.read_text(encoding="utf-8").strip()
            if chosen:
                return chosen
    except OSError:
        pass
    return str(SCRIPT_DIR / "yolo11n.pt")


def _custom_model_path() -> str:
    """The optional custom model that runs ALONGSIDE the stock model, taken from
    the active-model pointer file (written by the training "activate" action) —
    MYMATASAN_ACTIVE_MODEL_FILE env or active_model.txt next to this script.
    Returns "" when no custom model is active.

    Both models run in parallel on each frame and their detections are merged, so
    activating a custom model ADDS its classes on top of stock detection rather
    than replacing it. Restarting this worker (the activate/deactivate action does
    that) reloads the pair.
    """
    pointer = os.environ.get("MYMATASAN_ACTIVE_MODEL_FILE", "").strip()
    pointer_path = Path(pointer) if pointer else (SCRIPT_DIR / "active_model.txt")
    try:
        if pointer_path.is_file():
            active = pointer_path.read_text(encoding="utf-8").strip()
            if active and Path(active).is_file():
                return active
    except OSError:
        pass
    return ""


def _lpr_model_path() -> str:
    """The optional license-plate (LPR/ALPR) detector, taken from the LPR pointer
    file (written by Settings -> AI -> License Plate Model) in priority:

    1. MYMATASAN_LPR_MODEL env (explicit override, file path).
    2. The LPR pointer file (MYMATASAN_LPR_MODEL_FILE env or lpr_model.txt next to
       this script) — its content is a local .pt path chosen/downloaded in the UI.

    Returns "" when no plate model is configured (LPR disabled). When set to a YOLO
    .pt trained to localize plates, the worker runs a second stage on frames that
    request it (request "lpr": true) — crop each plate at full resolution, OCR it,
    infer the enclosing vehicle's type/color, and emit a "license plate" candidate
    carrying {plate, ocrConfidence, vehicleType, color} in metadata.
    """
    explicit = os.environ.get("MYMATASAN_LPR_MODEL", "").strip()
    if explicit and Path(explicit).is_file():
        return explicit
    pointer = os.environ.get("MYMATASAN_LPR_MODEL_FILE", "").strip()
    pointer_path = Path(pointer) if pointer else (SCRIPT_DIR / "lpr_model.txt")
    try:
        if pointer_path.is_file():
            chosen = pointer_path.read_text(encoding="utf-8").strip()
            if chosen and Path(chosen).is_file():
                return chosen
    except OSError:
        pass
    return ""


def _anomaly_manifest_path() -> str:
    """The optional anomaly-model manifest (written by the Teach wizard's
    activate step): a JSON list of {cameraId, label, modelPath, roi, skillId}
    entries. Cameras listed here get an extra "spot anything unusual" pass on
    every frame — MYMATASAN_ANOMALY_FILE env or anomaly_models.json next to
    this script. Returns "" when absent (feature off)."""
    pointer = os.environ.get("MYMATASAN_ANOMALY_FILE", "").strip()
    path = Path(pointer) if pointer else (SCRIPT_DIR / "anomaly_models.json")
    return str(path) if path.is_file() else ""


def _face_gallery_path() -> str:
    """The enrolled face gallery (written by FaceGalleryService): JSON
    {model, persons:[{id,name,embeddings:[[float]]}]}. Cameras with a face rule
    match live faces against it. MYMATASAN_FACES_FILE env or faces_gallery.json
    next to this script. Returns "" when absent (feature off)."""
    pointer = os.environ.get("MYMATASAN_FACES_FILE", "").strip()
    path = Path(pointer) if pointer else (SCRIPT_DIR / "faces_gallery.json")
    return str(path) if path.is_file() else ""


def _face_model_files() -> tuple[str, str]:
    """The YuNet detector + SFace recognizer .onnx paths (env, else next to this
    script). Returns ("","") when either is missing (feature off)."""
    yunet = os.environ.get("MYMATASAN_FACE_YUNET", "").strip() or str(SCRIPT_DIR / "face_detection_yunet_2023mar.onnx")
    sface = os.environ.get("MYMATASAN_FACE_SFACE", "").strip() or str(SCRIPT_DIR / "face_recognition_sface_2021dec.onnx")
    if Path(yunet).is_file() and Path(sface).is_file():
        return yunet, sface
    return "", ""


STOCK_MODEL_PATH = _stock_model_path()
CUSTOM_MODEL_PATH = _custom_model_path()
LPR_MODEL_PATH = _lpr_model_path()
ANOMALY_MANIFEST_PATH = _anomaly_manifest_path()
FACE_GALLERY_PATH = _face_gallery_path()
FACE_YUNET_PATH, FACE_SFACE_PATH = _face_model_files()
# The minimum cosine similarity to treat a live face as a recognized enrolled person. SFace's own
# "same" floor is ~0.36; we default higher for an NVR because naming the WRONG person is worse than
# naming nobody. The Go rule applies a second, per-rule floor on top of this.
FACE_MATCH_MIN_COS = float(os.environ.get("MYMATASAN_FACE_MIN_COS", "0.40"))
# OCR backend confidence is read per-character/line; this floors what we emit so a
# blurry partial read is reported as "" (no plate) rather than a wrong string.
LPR_OCR_MIN_CONF = float(os.environ.get("MYMATASAN_LPR_OCR_MIN_CONF", "0.3"))
CONFIDENCE = float(os.environ.get("MYMATASAN_YOLO_CONF", "0.25"))
DEVICE = os.environ.get("MYMATASAN_YOLO_DEVICE", "").strip()
IMGSZ_RAW = os.environ.get("MYMATASAN_YOLO_IMGSZ", "").strip()
IMGSZ = int(IMGSZ_RAW) if IMGSZ_RAW else None
IOU_RAW = os.environ.get("MYMATASAN_YOLO_IOU", "").strip()
IOU = float(IOU_RAW) if IOU_RAW else None
# Temporary diagnostic: when set, log every frame's raw detections to stderr so
# we can see what YOLO actually returns (label + confidence) before any
# threshold/zone/streak gating in Go. Set MYMATASAN_YOLO_DEBUG=1 to enable.
DEBUG = os.environ.get("MYMATASAN_YOLO_DEBUG", "").strip() not in ("", "0", "false", "False")

# Whether CUDA is available on this host — detected once after model load.
# False on Raspberry Pi, True on Jetson/desktop GPU.
_HAS_CUDA: bool = False


def _check_cuda() -> bool:
    try:
        import torch
        return torch.cuda.is_available()
    except Exception:
        return False


def _load_model(path: str) -> Any:
    from ultralytics import YOLO

    with contextlib.redirect_stdout(sys.stderr):
        return YOLO(path)


def _label(names: Any, cls_id: int) -> str:
    if isinstance(names, dict):
        return str(names.get(cls_id, cls_id)).lower()
    if isinstance(names, list) and 0 <= cls_id < len(names):
        return str(names[cls_id]).lower()
    return str(cls_id)


def _build_kwargs(request: dict[str, Any]) -> dict[str, Any]:
    # Per-request overrides take priority over env-var defaults.
    req_conf = request.get("inferConf")
    req_iou = request.get("inferIou")
    req_augment = request.get("inferAugment")
    req_imgsz = request.get("inferImgsz")
    req_half = request.get("inferHalf")
    req_max_det = request.get("inferMaxDet")

    eff_conf = float(req_conf) if req_conf else CONFIDENCE
    eff_iou = float(req_iou) if req_iou else IOU
    eff_imgsz = int(req_imgsz) if req_imgsz else IMGSZ
    eff_augment = bool(req_augment)
    # half-precision is only supported on CUDA; silently ignore on CPU (Raspberry Pi etc.)
    eff_half = bool(req_half) and _HAS_CUDA
    eff_max_det = int(req_max_det) if req_max_det else None

    kwargs: dict[str, Any] = {"conf": eff_conf, "verbose": False}
    if DEVICE:
        kwargs["device"] = DEVICE
    if eff_iou is not None:
        kwargs["iou"] = eff_iou
    if eff_imgsz:
        kwargs["imgsz"] = eff_imgsz
    if eff_augment:
        kwargs["augment"] = True
    if eff_half:
        kwargs["half"] = True
    if eff_max_det:
        kwargs["max_det"] = eff_max_det
    return kwargs


def _run_model(model: Any, model_path: str, tmp_path: str, kwargs: dict[str, Any]) -> list[dict[str, Any]]:
    # Plain predict (no ByteTrack) — see the long-standing note: detection rules
    # don't use track IDs and line crossing does its own centroid tracking.
    with contextlib.redirect_stdout(sys.stderr):
        results = model.predict(tmp_path, **kwargs)

    detections: list[dict[str, Any]] = []
    names = getattr(model, "names", {})
    for result in results:
        boxes = getattr(result, "boxes", None)
        if boxes is None:
            continue
        height, width = result.orig_shape[:2]
        if not width or not height:
            continue
        for box in boxes:
            cls_id = int(box.cls[0].item())
            confidence = float(box.conf[0].item())
            x1, y1, x2, y2 = [float(v) for v in box.xyxy[0].tolist()]
            metadata: dict[str, Any] = {"model": model_path, "classId": cls_id}
            detections.append(
                {
                    "label": _label(names, cls_id),
                    "confidence": max(0.0, min(1.0, confidence)),
                    "box": {
                        "x": max(0.0, min(1.0, x1 / width)),
                        "y": max(0.0, min(1.0, y1 / height)),
                        "w": max(0.0, min(1.0, (x2 - x1) / width)),
                        "h": max(0.0, min(1.0, (y2 - y1) / height)),
                    },
                    "metadata": metadata,
                }
            )
    return detections


def _iou(a: dict[str, float], b: dict[str, float]) -> float:
    ax2, ay2 = a["x"] + a["w"], a["y"] + a["h"]
    bx2, by2 = b["x"] + b["w"], b["y"] + b["h"]
    ix1, iy1 = max(a["x"], b["x"]), max(a["y"], b["y"])
    ix2, iy2 = min(ax2, bx2), min(ay2, by2)
    iw, ih = max(0.0, ix2 - ix1), max(0.0, iy2 - iy1)
    inter = iw * ih
    union = a["w"] * a["h"] + b["w"] * b["h"] - inter
    return inter / union if union > 0 else 0.0


def _merge(stock_dets: list[dict[str, Any]], custom_dets: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Merge stock + custom detections, with the taught (custom) model taking
    PRIORITY where it fires.

    A custom model is a deliberate, per-deployment refinement, so where it detects an
    object it should own that object's label: a stock box it overlaps is dropped even
    when the labels differ (e.g. stock "dog" + custom "cat" on the same animal → just
    "cat"). That saves a duplicate presence row in Object Search and avoids the
    confusing "it's both" result, while still collapsing same-label near-duplicates so
    crowd counts aren't doubled. Stock detections the custom model does NOT cover are
    kept untouched, so activating a skill never blinds the stock classes elsewhere in
    the frame."""
    kept: list[dict[str, Any]] = []
    # Custom detections first (highest confidence first), collapsing same-label dupes.
    for det in sorted(custom_dets, key=lambda d: d["confidence"], reverse=True):
        if any(k["label"] == det["label"] and _iou(k["box"], det["box"]) > 0.55 for k in kept):
            continue
        kept.append(det)
    custom_kept = list(kept)
    # Then stock: a custom detection wins any box it overlaps (ANY label), so suppress
    # stock boxes a taught detection already owns; then the usual same-label dedup.
    for det in sorted(stock_dets, key=lambda d: d["confidence"], reverse=True):
        if any(_iou(c["box"], det["box"]) > 0.55 for c in custom_kept):
            continue
        if any(k["label"] == det["label"] and _iou(k["box"], det["box"]) > 0.55 for k in kept):
            continue
        kept.append(det)
    # Restore the confidence-desc ordering the rest of the pipeline expects.
    kept.sort(key=lambda d: d["confidence"], reverse=True)
    return kept


# --- Anomaly ("spot anything unusual") stage ------------------------------------
#
# Taught anomaly skills score each frame against a memory bank of NORMAL
# embeddings (built by anomaly_worker.py). Everything here is lazy and guarded:
# a missing/broken model or torchvision disables that entry without touching
# stock/custom detection. Fires a synthetic detection labeled with the skill's
# class when the frame's distance exceeds the calibrated threshold, so the Go
# rule/alert pipeline needs no special casing.

_ANOMALY_ENTRIES: list[dict[str, Any]] | None = None
_ANOMALY_BACKBONE: Any = None
_ANOMALY_PREP: Any = None
_ANOMALY_DISABLED = False


def _anomaly_entries() -> list[dict[str, Any]]:
    global _ANOMALY_ENTRIES
    if _ANOMALY_ENTRIES is not None:
        return _ANOMALY_ENTRIES
    _ANOMALY_ENTRIES = []
    if not ANOMALY_MANIFEST_PATH:
        return _ANOMALY_ENTRIES
    try:
        raw = json.loads(Path(ANOMALY_MANIFEST_PATH).read_text(encoding="utf-8"))
        if isinstance(raw, list):
            _ANOMALY_ENTRIES = [e for e in raw if isinstance(e, dict) and e.get("modelPath")]
    except Exception as exc:  # noqa: BLE001
        print(f"anomaly: manifest unreadable, disabled: {exc}", file=sys.stderr, flush=True)
    return _ANOMALY_ENTRIES


def _anomaly_backbone():
    """Shared resnet18 embedder, loaded once on first use."""
    global _ANOMALY_BACKBONE, _ANOMALY_PREP, _ANOMALY_DISABLED
    if _ANOMALY_DISABLED:
        return None, None
    if _ANOMALY_BACKBONE is not None:
        return _ANOMALY_BACKBONE, _ANOMALY_PREP
    try:
        import torch
        import torchvision
        from torchvision import transforms

        with contextlib.redirect_stdout(sys.stderr):
            backbone = torchvision.models.resnet18(weights=torchvision.models.ResNet18_Weights.IMAGENET1K_V1)
        backbone.fc = torch.nn.Identity()
        backbone.eval()
        if _HAS_CUDA:
            backbone = backbone.to("cuda")
        _ANOMALY_BACKBONE = backbone
        _ANOMALY_PREP = transforms.Compose([
            transforms.Resize((224, 224)),
            transforms.ToTensor(),
            transforms.Normalize(mean=[0.485, 0.456, 0.406], std=[0.229, 0.224, 0.225]),
        ])
    except Exception as exc:  # noqa: BLE001
        print(f"anomaly: backbone unavailable, disabled: {exc}", file=sys.stderr, flush=True)
        _ANOMALY_DISABLED = True
        return None, None
    return _ANOMALY_BACKBONE, _ANOMALY_PREP


def _anomaly_model(entry: dict[str, Any]) -> dict[str, Any] | None:
    """Lazily load one entry's memory bank; cache (or a failure marker) on it."""
    if "_state" in entry:
        return entry["_state"]  # may be None after a failed load
    entry["_state"] = None
    try:
        import torch

        blob = torch.load(entry["modelPath"], map_location="cpu", weights_only=False)
        bank = blob["bank"]
        if _HAS_CUDA:
            bank = bank.to("cuda")
        entry["_state"] = {"bank": bank, "threshold": float(blob["threshold"]), "knn": int(blob.get("knn", 3))}
    except Exception as exc:  # noqa: BLE001
        print(f"anomaly: model load failed ({entry.get('modelPath')}): {exc}", file=sys.stderr, flush=True)
    return entry["_state"]


def _anomaly_detect(camera_id: int, tmp_path: str) -> list[dict[str, Any]]:
    entries = [e for e in _anomaly_entries() if int(e.get("cameraId") or 0) == camera_id]
    if not entries:
        return []
    backbone, prep = _anomaly_backbone()
    if backbone is None:
        return []
    detections: list[dict[str, Any]] = []
    try:
        import torch
        from PIL import Image

        image = Image.open(tmp_path).convert("RGB")
        width, height = image.size
        for entry in entries:
            state = _anomaly_model(entry)
            if state is None:
                continue
            roi = entry.get("roi") or [0.0, 0.0, 1.0, 1.0]
            x, y, w, h = [float(v) for v in roi[:4]]
            if w <= 0 or h <= 0:
                x, y, w, h = 0.0, 0.0, 1.0, 1.0
            crop = image.crop((int(x * width), int(y * height), int((x + w) * width), int((y + h) * height)))
            with torch.no_grad():
                tensor = prep(crop).unsqueeze(0)
                if _HAS_CUDA:
                    tensor = tensor.to("cuda")
                feat = torch.nn.functional.normalize(backbone(tensor).squeeze(0), dim=0)
                sims = state["bank"] @ feat
                k = min(state["knn"], sims.shape[0])
                score = float(1.0 - torch.topk(sims, k).values.mean().item())
            threshold = state["threshold"]
            if score <= threshold:
                continue
            # Map "how far past the threshold" into 0.5..0.99 so the auto-created
            # rule's 0.5 confidence floor lines up with "at the threshold".
            margin = max(threshold * 0.5, 1e-6)
            confidence = 0.5 + 0.49 * min(1.0, (score - threshold) / margin)
            detections.append({
                "label": str(entry.get("label") or "unusual"),
                "confidence": confidence,
                "box": {"x": x, "y": y, "w": w, "h": h},
                "metadata": {"model": "anomaly", "score": round(score, 6), "threshold": round(threshold, 6)},
            })
    except Exception as exc:  # noqa: BLE001
        print(f"anomaly: scoring failed: {exc}", file=sys.stderr, flush=True)
        return []
    return detections


# --- Face recognition stage (YuNet detect + SFace embed + gallery cosine match) ---------------

_face_model_cache: Any = None
_face_gallery_cache: dict[str, Any] = {"mtime": 0.0, "persons": []}


def _get_face_model():
    """Lazily build the shared YuNet+SFace model (face_model.FaceModel); cache it (or a None marker
    on failure) so a missing opencv/model does not crash every frame."""
    global _face_model_cache
    if _face_model_cache is not None:
        return _face_model_cache if _face_model_cache is not False else None
    if not FACE_YUNET_PATH or not FACE_SFACE_PATH:
        _face_model_cache = False
        return None
    try:
        from face_model import FaceModel

        _face_model_cache = FaceModel(FACE_YUNET_PATH, FACE_SFACE_PATH)
    except Exception as exc:  # noqa: BLE001
        print(f"faces: model load failed: {exc}", file=sys.stderr, flush=True)
        _face_model_cache = False
        return None
    return _face_model_cache


def _get_face_gallery():
    """Load (and hot-reload on file change) the enrolled gallery as a list of {id, name, mat}, where
    mat is a unit-normalized (N,128) matrix so one dot product per person IS a cosine similarity."""
    global _face_gallery_cache
    if not FACE_GALLERY_PATH:
        return []
    try:
        mtime = Path(FACE_GALLERY_PATH).stat().st_mtime
    except OSError:
        return []
    if mtime == _face_gallery_cache["mtime"]:
        return _face_gallery_cache["persons"]
    try:
        import numpy as np

        with open(FACE_GALLERY_PATH, "r", encoding="utf-8") as f:
            blob = json.load(f)
        persons = []
        for p in blob.get("persons", []):
            embs = p.get("embeddings") or []
            if not embs:
                continue
            mat = np.asarray(embs, dtype=np.float32)
            norms = np.linalg.norm(mat, axis=1, keepdims=True)
            norms[norms == 0] = 1.0
            persons.append({"id": int(p.get("id") or 0), "name": str(p.get("name") or ""), "mat": mat / norms})
        _face_gallery_cache = {"mtime": mtime, "persons": persons}
    except Exception as exc:  # noqa: BLE001
        print(f"faces: gallery load failed: {exc}", file=sys.stderr, flush=True)
    return _face_gallery_cache["persons"]


def _faces_detect(tmp_path: str) -> list[dict[str, Any]]:
    """Detect every face in the frame, embed it, and match against the enrolled gallery. Emits a
    'face' candidate for EACH face — recognized (personName set) or not (unknown) — so a rule can
    fire on known people, a chosen set, or strangers. Matching (in Go) applies the rule's policy."""
    model = _get_face_model()
    if model is None:
        return []
    gallery = _get_face_gallery()
    detections: list[dict[str, Any]] = []
    try:
        import cv2
        import numpy as np

        img = cv2.imread(tmp_path)
        if img is None:
            return []
        for f in model.detect_embed(img):
            vec = np.asarray(f["vector"], dtype=np.float32)
            n = np.linalg.norm(vec)
            if n == 0:
                continue
            vec = vec / n
            best_name, best_id, best_cos = "", 0, 0.0
            for p in gallery:
                sims = p["mat"] @ vec
                c = float(sims.max()) if sims.size else 0.0
                if c > best_cos:
                    best_cos, best_name, best_id = c, p["name"], p["id"]
            recognized = best_cos >= FACE_MATCH_MIN_COS
            x, y, w, h = f["box"]
            detections.append({
                "label": "face",
                "confidence": float(f["quality"]),
                "box": {"x": float(x), "y": float(y), "w": float(w), "h": float(h)},
                "metadata": {
                    "model": "face",
                    "personId": best_id if recognized else 0,
                    "personName": best_name if recognized else "",
                    "confidence": round(best_cos, 4),
                },
            })
    except Exception as exc:  # noqa: BLE001
        print(f"faces: scoring failed: {exc}", file=sys.stderr, flush=True)
        return []
    return detections


# --- License-plate (LPR) stage -------------------------------------------------
#
# Everything below is lazy and guarded: if the plate model or OCR backend is
# missing or errors, the LPR stage disables itself and stock/custom detection are
# unaffected. Plate detection localizes; OCR reads; color/vehicle-type enrich.

_OCR_READER: Any = None
_OCR_DISABLED = False
_VEHICLE_LABELS = {"car", "truck", "bus", "motorcycle", "motorbike", "van"}


def _ocr_reader() -> Any:
    """Lazily build an OCR reader, preferring fast_plate_ocr, then easyocr.
    Returns None (and disables further attempts) if neither is importable."""
    global _OCR_READER, _OCR_DISABLED
    if _OCR_DISABLED:
        return None
    if _OCR_READER is not None:
        return _OCR_READER
    # easyocr is the most common general backend; the recognizer is created once
    # and reused. GPU is used automatically when torch reports CUDA.
    try:
        import easyocr  # type: ignore

        _OCR_READER = easyocr.Reader(["en"], gpu=_HAS_CUDA, verbose=False)
        return _OCR_READER
    except Exception as exc:  # pragma: no cover - depends on host deps
        print(f"lpr: OCR backend unavailable, plate text disabled: {exc}", file=sys.stderr, flush=True)
        _OCR_DISABLED = True
        return None


def _ocr_plate(crop: Any) -> tuple[str, float]:
    """OCR a cropped plate image (numpy BGR). Returns (text, confidence) with text
    upper-cased and stripped to alphanumerics; ("", 0.0) when unreadable."""
    reader = _ocr_reader()
    if reader is None or crop is None or crop.size == 0:
        return "", 0.0
    try:
        results = reader.readtext(crop)
    except Exception:
        return "", 0.0
    best_text, best_conf = "", 0.0
    for item in results:
        # easyocr returns (bbox, text, confidence)
        text = "".join(ch for ch in str(item[1]).upper() if ch.isalnum())
        conf = float(item[2]) if len(item) > 2 else 0.0
        if text and conf >= best_conf:
            best_text, best_conf = text, conf
    if best_conf < LPR_OCR_MIN_CONF:
        return "", 0.0
    return best_text, best_conf


# Coarse HSV ranges → color names. Plate/vehicle color only needs to be roughly
# right ("a white car"), so a small lookup beats a model here.
def _vehicle_color(crop: Any) -> str:
    if crop is None or crop.size == 0:
        return ""
    try:
        import cv2  # type: ignore
        import numpy as np  # type: ignore
    except Exception:
        return ""
    try:
        hsv = cv2.cvtColor(crop, cv2.COLOR_BGR2HSV)
        h = float(np.median(hsv[:, :, 0]))
        s = float(np.median(hsv[:, :, 1]))
        v = float(np.median(hsv[:, :, 2]))
    except Exception:
        return ""
    if v < 50:
        return "black"
    if s < 40:
        return "white" if v > 170 else "gray"
    if h < 10 or h >= 160:
        return "red"
    if h < 25:
        return "orange"
    if h < 35:
        return "yellow"
    if h < 85:
        return "green"
    if h < 130:
        return "blue"
    return "purple"


def _contains(outer: dict[str, float], inner: dict[str, float]) -> bool:
    """Whether the inner box's centre falls inside the outer box (used to find the
    vehicle that a plate belongs to)."""
    cx = inner["x"] + inner["w"] / 2
    cy = inner["y"] + inner["h"] / 2
    return outer["x"] <= cx <= outer["x"] + outer["w"] and outer["y"] <= cy <= outer["y"] + outer["h"]


def _associate_vehicle(plate_box: dict[str, float], stock_dets: list[dict[str, Any]]) -> str:
    """Find the vehicle type for a plate by locating the smallest stock vehicle
    detection whose box contains the plate. Smallest = tightest enclosing vehicle."""
    best_label, best_area = "", 2.0  # areas are normalized (<=1), so 2.0 = "none yet"
    for det in stock_dets:
        if det["label"] not in _VEHICLE_LABELS:
            continue
        if not _contains(det["box"], plate_box):
            continue
        area = det["box"]["w"] * det["box"]["h"]
        if area < best_area:
            best_label, best_area = det["label"], area
    return best_label


def _lpr_detect(
    plate_model: Any, tmp_path: str, kwargs: dict[str, Any], stock_dets: list[dict[str, Any]]
) -> list[dict[str, Any]]:
    """Run plate localization + OCR + enrichment. Returns "license plate" candidates."""
    try:
        import cv2  # type: ignore
    except Exception:
        return []
    image = cv2.imread(tmp_path)
    if image is None:
        return []
    img_h, img_w = image.shape[:2]
    if not img_w or not img_h:
        return []

    # Localize plates (the plate model has its own classes; we treat every box as a
    # plate region and let OCR decide readability).
    plate_boxes = _run_model(plate_model, LPR_MODEL_PATH, tmp_path, kwargs)
    out: list[dict[str, Any]] = []
    for det in plate_boxes:
        box = det["box"]
        x1 = int(box["x"] * img_w)
        y1 = int(box["y"] * img_h)
        x2 = int((box["x"] + box["w"]) * img_w)
        y2 = int((box["y"] + box["h"]) * img_h)
        x1, y1 = max(0, x1), max(0, y1)
        x2, y2 = min(img_w, x2), min(img_h, y2)
        if x2 <= x1 or y2 <= y1:
            continue
        plate_crop = image[y1:y2, x1:x2]
        text, conf = _ocr_plate(plate_crop)
        if not text:
            continue
        vehicle_type = _associate_vehicle(box, stock_dets)
        color = ""
        # Color the vehicle, not the plate: find the enclosing vehicle box to sample.
        for vdet in stock_dets:
            if vdet["label"] in _VEHICLE_LABELS and _contains(vdet["box"], box):
                vx1 = max(0, int(vdet["box"]["x"] * img_w))
                vy1 = max(0, int(vdet["box"]["y"] * img_h))
                vx2 = min(img_w, int((vdet["box"]["x"] + vdet["box"]["w"]) * img_w))
                vy2 = min(img_h, int((vdet["box"]["y"] + vdet["box"]["h"]) * img_h))
                if vx2 > vx1 and vy2 > vy1:
                    color = _vehicle_color(image[vy1:vy2, vx1:vx2])
                break
        out.append(
            {
                "label": "license plate",
                "confidence": det["confidence"],
                "box": box,
                "metadata": {
                    "model": LPR_MODEL_PATH,
                    "plate": text,
                    "ocrConfidence": conf,
                    "vehicleType": vehicle_type,
                    "color": color,
                },
            }
        )
    return out


def _detect(stock_model: Any, custom_model: Any, plate_model: Any, request: dict[str, Any]) -> list[dict[str, Any]]:
    camera_id = int(request.get("cameraId") or 0)
    image_b64 = str(request.get("image") or "")
    if not image_b64:
        raise ValueError("request image is required")

    image_bytes = base64.b64decode(image_b64)
    kwargs = _build_kwargs(request)
    want_lpr = bool(request.get("lpr")) and plate_model is not None
    want_face = bool(request.get("face")) and bool(FACE_GALLERY_PATH) and bool(FACE_YUNET_PATH)
    tmp_path = ""
    try:
        with tempfile.NamedTemporaryFile(delete=False, suffix=".jpg") as tmp:
            tmp.write(image_bytes)
            tmp_path = tmp.name
        stock_dets = _run_model(stock_model, STOCK_MODEL_PATH, tmp_path, kwargs)
        custom_dets = _run_model(custom_model, CUSTOM_MODEL_PATH, tmp_path, kwargs) if custom_model is not None else []
        # The plate stage reuses the SAME captured frame (no second grab) and the
        # stock detections (for vehicle type/color), so enabling LPR adds only OCR
        # compute, not another decode — see the per-camera gating note.
        lpr_dets = _lpr_detect(plate_model, tmp_path, kwargs, stock_dets) if want_lpr else []
        anomaly_dets = _anomaly_detect(camera_id, tmp_path)
        # The face stage reuses the SAME captured frame (no second grab) — enabling it adds only the
        # detect+embed compute, and only on cameras whose rule set requested it (want_face gate).
        face_dets = _faces_detect(tmp_path) if want_face else []
    finally:
        if tmp_path:
            Path(tmp_path).unlink(missing_ok=True)

    detections = _merge(stock_dets, custom_dets) if custom_dets else list(stock_dets)
    detections += lpr_dets
    detections += anomaly_dets
    detections += face_dets

    if DEBUG:
        if detections:
            summary = ", ".join(
                f"{d['label']}:{d['confidence']:.2f}"
                f"@({d['box']['x']+d['box']['w']/2:.2f},{d['box']['y']+d['box']['h']/2:.2f})"
                for d in detections
            )
        else:
            summary = "none"
        print(f"yolo-debug cam{camera_id}: {summary}", file=sys.stderr, flush=True)

    return detections


def _write(payload: Any) -> None:
    print(json.dumps(payload, separators=(",", ":")), flush=True)


def main() -> int:
    global _HAS_CUDA
    try:
        stock_model = _load_model(STOCK_MODEL_PATH)
    except Exception as exc:
        print(f"failed to load stock YOLO model: {exc}", file=sys.stderr, flush=True)
        return 1

    # The custom model is optional and runs alongside stock. A failure to load it
    # must not take down stock detection.
    custom_model = None
    if CUSTOM_MODEL_PATH:
        try:
            custom_model = _load_model(CUSTOM_MODEL_PATH)
        except Exception as exc:
            print(f"failed to load custom YOLO model ({CUSTOM_MODEL_PATH}): {exc}", file=sys.stderr, flush=True)
            custom_model = None

    # The plate model is optional and only used on frames that request "lpr":true.
    # Loading it must not take down stock detection.
    plate_model = None
    if LPR_MODEL_PATH:
        if Path(LPR_MODEL_PATH).is_file():
            try:
                plate_model = _load_model(LPR_MODEL_PATH)
            except Exception as exc:
                print(f"failed to load LPR model ({LPR_MODEL_PATH}): {exc}", file=sys.stderr, flush=True)
                plate_model = None
        else:
            print(f"lpr: MYMATASAN_LPR_MODEL set but not a file: {LPR_MODEL_PATH}", file=sys.stderr, flush=True)

    _HAS_CUDA = _check_cuda()
    device_label = "cuda" if _HAS_CUDA else "cpu"
    if DEVICE:
        device_label = DEVICE
    custom_label = CUSTOM_MODEL_PATH if custom_model is not None else "none"
    lpr_label = LPR_MODEL_PATH if plate_model is not None else "none"
    print(
        f"yolo_worker ready: stock={STOCK_MODEL_PATH} custom={custom_label} lpr={lpr_label} device={device_label} cuda={_HAS_CUDA}",
        file=sys.stderr,
        flush=True,
    )

    try:
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            try:
                request = json.loads(line)
                _write(_detect(stock_model, custom_model, plate_model, request))
            except Exception as exc:
                _write({"error": str(exc)})
    except KeyboardInterrupt:
        # Ctrl+C in the terminal sends SIGINT to the whole process group, hitting
        # this worker too. Exit quietly rather than dumping a traceback — the Go
        # parent shuts us down by closing stdin anyway.
        pass
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        # SIGINT before/outside the read loop (e.g. during model load) — exit with
        # the conventional 130 instead of an unhandled-exception traceback.
        raise SystemExit(130)
