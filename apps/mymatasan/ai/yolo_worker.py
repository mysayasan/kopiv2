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
MODEL_PATH = os.environ.get("MYMATASAN_YOLO_MODEL", str(SCRIPT_DIR / "yolo11n.pt"))
CONFIDENCE = float(os.environ.get("MYMATASAN_YOLO_CONF", "0.25"))
DEVICE = os.environ.get("MYMATASAN_YOLO_DEVICE", "").strip()
IMGSZ_RAW = os.environ.get("MYMATASAN_YOLO_IMGSZ", "").strip()
IMGSZ = int(IMGSZ_RAW) if IMGSZ_RAW else None
IOU_RAW = os.environ.get("MYMATASAN_YOLO_IOU", "").strip()
IOU = float(IOU_RAW) if IOU_RAW else None

# Whether CUDA is available on this host — detected once after model load.
# False on Raspberry Pi, True on Jetson/desktop GPU.
_HAS_CUDA: bool = False

# Per-camera saved tracker objects: camera_id -> tracker
_camera_trackers: dict[int, Any] = {}


def _check_cuda() -> bool:
    try:
        import torch
        return torch.cuda.is_available()
    except Exception:
        return False


def _load_model() -> Any:
    from ultralytics import YOLO

    with contextlib.redirect_stdout(sys.stderr):
        return YOLO(MODEL_PATH)


def _label(names: Any, cls_id: int) -> str:
    if isinstance(names, dict):
        return str(names.get(cls_id, cls_id)).lower()
    if isinstance(names, list) and 0 <= cls_id < len(names):
        return str(names[cls_id]).lower()
    return str(cls_id)


def _restore_tracker(model: Any, camera_id: int) -> None:
    """Swap in this camera's saved ByteTrack state before calling model.track()."""
    predictor = getattr(model, "predictor", None)
    if predictor is None:
        return
    saved = _camera_trackers.get(camera_id)
    if saved is not None:
        trackers = getattr(predictor, "trackers", None)
        if trackers is not None:
            if len(trackers) > 0:
                trackers[0] = saved
            else:
                trackers.append(saved)
    else:
        # New camera — reset so ByteTrack initialises fresh rather than
        # inheriting the previous camera's tracker state.
        if hasattr(predictor, "trackers"):
            predictor.trackers = []


def _save_tracker(model: Any, camera_id: int) -> None:
    """Persist the updated ByteTrack state after model.track()."""
    predictor = getattr(model, "predictor", None)
    if predictor is None:
        return
    trackers = getattr(predictor, "trackers", None)
    if trackers and len(trackers) > 0:
        _camera_trackers[camera_id] = trackers[0]


def _detect(model: Any, request: dict[str, Any]) -> list[dict[str, Any]]:
    camera_id = int(request.get("cameraId") or 0)
    image_b64 = str(request.get("image") or "")
    if not image_b64:
        raise ValueError("request image is required")

    image_bytes = base64.b64decode(image_b64)
    tmp_path = ""
    try:
        with tempfile.NamedTemporaryFile(delete=False, suffix=".jpg") as tmp:
            tmp.write(image_bytes)
            tmp_path = tmp.name

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

        kwargs: dict[str, Any] = {
            "conf": eff_conf,
            "verbose": False,
        }
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

        with contextlib.redirect_stdout(sys.stderr):
            try:
                _restore_tracker(model, camera_id)
                results = model.track(
                    tmp_path,
                    tracker="bytetrack.yaml",
                    persist=True,
                    **kwargs,
                )
                _save_tracker(model, camera_id)
            except Exception as exc:
                # Fall back to plain predict if ByteTrack is unavailable or fails.
                print(f"bytetrack failed, falling back to predict: {exc}", file=sys.stderr, flush=True)
                results = model.predict(tmp_path, **kwargs)
    finally:
        if tmp_path:
            Path(tmp_path).unlink(missing_ok=True)

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

            track_id: int | None = None
            if box.id is not None:
                try:
                    track_id = int(box.id[0].item())
                except (IndexError, TypeError, ValueError):
                    track_id = None

            metadata: dict[str, Any] = {
                "model": MODEL_PATH,
                "classId": cls_id,
            }
            if track_id is not None:
                metadata["trackId"] = track_id

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


def _write(payload: Any) -> None:
    print(json.dumps(payload, separators=(",", ":")), flush=True)


def main() -> int:
    global _HAS_CUDA
    try:
        model = _load_model()
    except Exception as exc:
        print(f"failed to load YOLO model: {exc}", file=sys.stderr, flush=True)
        return 1

    _HAS_CUDA = _check_cuda()
    device_label = "cuda" if _HAS_CUDA else "cpu"
    if DEVICE:
        device_label = DEVICE
    print(f"yolo_worker ready: model={MODEL_PATH} device={device_label} cuda={_HAS_CUDA}", file=sys.stderr, flush=True)

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            request = json.loads(line)
            _write(_detect(model, request))
        except Exception as exc:
            _write({"error": str(exc)})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
