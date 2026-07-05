#!/usr/bin/env python3
"""Holdout evaluator for MyMataSan Teach.

Runs a trained model over a directory of images (the export's val split) and
streams per-image predictions to stdout as JSON lines:

  {"event":"start","count":12}
  {"event":"prediction","image":"41","detections":[{"label":"broken widget","confidence":0.83}]}
  {"event":"completed"}
  {"event":"error","message":"..."}

ultralytics' own console output goes to stderr so stdout carries only these
JSON lines for the Go runner to parse (same protocol style as train_worker).
"""

from __future__ import annotations

import argparse
import contextlib
import json
import sys
from pathlib import Path


_REAL_STDOUT = sys.stdout


def emit(obj) -> None:
    print(json.dumps(obj, separators=(",", ":")), file=_REAL_STDOUT, flush=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--weights", required=True)
    parser.add_argument("--images", required=True)
    parser.add_argument("--conf", type=float, default=0.15)
    args = parser.parse_args()

    try:
        from ultralytics import YOLO
    except Exception as exc:  # noqa: BLE001
        emit({"event": "error", "message": f"ultralytics not available: {exc}"})
        return 1

    try:
        with contextlib.redirect_stdout(sys.stderr):
            model = YOLO(args.weights)
        names = getattr(model, "names", {})

        def label_for(cls_id: int) -> str:
            if isinstance(names, dict):
                return str(names.get(cls_id, cls_id)).lower()
            if isinstance(names, list) and 0 <= cls_id < len(names):
                return str(names[cls_id]).lower()
            return str(cls_id)

        images = sorted(
            f for f in Path(args.images).iterdir()
            if f.suffix.lower() in (".jpg", ".jpeg", ".png")
        )
        emit({"event": "start", "count": len(images)})
        for image in images:
            with contextlib.redirect_stdout(sys.stderr):
                results = model.predict(str(image), conf=args.conf, verbose=False)
            detections = []
            for result in results:
                boxes = getattr(result, "boxes", None)
                if boxes is None:
                    continue
                for box in boxes:
                    detections.append({
                        "label": label_for(int(box.cls[0].item())),
                        "confidence": float(box.conf[0].item()),
                    })
            emit({"event": "prediction", "image": image.stem, "detections": detections})
        emit({"event": "completed"})
        return 0
    except Exception as exc:  # noqa: BLE001
        emit({"event": "error", "message": str(exc)})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
