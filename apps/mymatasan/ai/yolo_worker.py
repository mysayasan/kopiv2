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


STOCK_MODEL_PATH = _stock_model_path()
CUSTOM_MODEL_PATH = _custom_model_path()
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
    """Merge stock + custom detections, dropping near-duplicate boxes of the SAME
    label (e.g. both models seeing the same person) so crowd counts aren't doubled.
    Different labels at the same spot (e.g. stock "person" + custom "papa") are
    both kept — that physical object is both."""
    combined = sorted(stock_dets + custom_dets, key=lambda d: d["confidence"], reverse=True)
    kept: list[dict[str, Any]] = []
    for det in combined:
        if any(k["label"] == det["label"] and _iou(k["box"], det["box"]) > 0.55 for k in kept):
            continue
        kept.append(det)
    return kept


def _detect(stock_model: Any, custom_model: Any, request: dict[str, Any]) -> list[dict[str, Any]]:
    camera_id = int(request.get("cameraId") or 0)
    image_b64 = str(request.get("image") or "")
    if not image_b64:
        raise ValueError("request image is required")

    image_bytes = base64.b64decode(image_b64)
    kwargs = _build_kwargs(request)
    tmp_path = ""
    try:
        with tempfile.NamedTemporaryFile(delete=False, suffix=".jpg") as tmp:
            tmp.write(image_bytes)
            tmp_path = tmp.name
        stock_dets = _run_model(stock_model, STOCK_MODEL_PATH, tmp_path, kwargs)
        custom_dets = _run_model(custom_model, CUSTOM_MODEL_PATH, tmp_path, kwargs) if custom_model is not None else []
    finally:
        if tmp_path:
            Path(tmp_path).unlink(missing_ok=True)

    detections = _merge(stock_dets, custom_dets) if custom_dets else stock_dets

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

    _HAS_CUDA = _check_cuda()
    device_label = "cuda" if _HAS_CUDA else "cpu"
    if DEVICE:
        device_label = DEVICE
    custom_label = CUSTOM_MODEL_PATH if custom_model is not None else "none"
    print(
        f"yolo_worker ready: stock={STOCK_MODEL_PATH} custom={custom_label} device={device_label} cuda={_HAS_CUDA}",
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
                _write(_detect(stock_model, custom_model, request))
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
