#!/usr/bin/env python3
"""One-shot YOLO trainer for MyMataSan.

Trains a detection model on an exported dataset (data.yaml) and streams
machine-readable progress to stdout as JSON lines, e.g.:

  {"event":"start","epochs":50,"device":"cpu"}
  {"event":"progress","epoch":1,"epochs":50}
  {"event":"completed","weights":".../best.pt","metrics":{"map50":0.81,"map5095":0.6}}
  {"event":"error","message":"..."}

ultralytics' own console output is redirected to stderr so stdout carries only
these JSON lines for the Go runner to parse.
"""

from __future__ import annotations

import argparse
import contextlib
import json
import sys
from pathlib import Path


# Capture the real stdout before ultralytics noise is redirected to stderr.
_REAL_STDOUT = sys.stdout


def emit(obj) -> None:
    print(json.dumps(obj, separators=(",", ":")), file=_REAL_STDOUT, flush=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", required=True)
    parser.add_argument("--base", default="yolo11n.pt")
    parser.add_argument("--epochs", type=int, default=50)
    parser.add_argument("--imgsz", type=int, default=640)
    parser.add_argument("--project", required=True)
    parser.add_argument("--name", default="run")
    args = parser.parse_args()

    try:
        from ultralytics import YOLO
    except Exception as exc:  # noqa: BLE001
        emit({"event": "error", "message": f"ultralytics not available: {exc}"})
        return 1

    try:
        import torch

        device = 0 if torch.cuda.is_available() else "cpu"
    except Exception:  # noqa: BLE001
        device = "cpu"

    emit({"event": "start", "epochs": args.epochs, "device": str(device)})

    try:
        with contextlib.redirect_stdout(sys.stderr):
            model = YOLO(args.base)

            def on_train_epoch_end(trainer) -> None:
                try:
                    epoch = int(getattr(trainer, "epoch", 0)) + 1
                    total = int(getattr(trainer, "epochs", args.epochs))
                    emit({"event": "progress", "epoch": epoch, "epochs": total})
                except Exception:  # noqa: BLE001
                    pass

            model.add_callback("on_train_epoch_end", on_train_epoch_end)

            results = model.train(
                data=args.data,
                epochs=args.epochs,
                imgsz=args.imgsz,
                project=args.project,
                name=args.name,
                exist_ok=True,
                verbose=False,
                device=device,
            )

        # Prefer ultralytics' own record of where best.pt was saved (handles run
        # dir incrementing / absolute resolution); fall back to the expected path.
        weights = ""
        try:
            trainer = getattr(model, "trainer", None)
            best = getattr(trainer, "best", None) if trainer is not None else None
            if best:
                weights = str(best)
        except Exception:  # noqa: BLE001
            weights = ""
        if not weights:
            weights = str(Path(args.project) / args.name / "weights" / "best.pt")
        metrics = {}
        try:
            rd = getattr(results, "results_dict", {}) or {}
            metrics = {
                "map50": float(rd.get("metrics/mAP50(B)", 0.0)),
                "map5095": float(rd.get("metrics/mAP50-95(B)", 0.0)),
            }
        except Exception:  # noqa: BLE001
            metrics = {}

        emit({"event": "completed", "weights": str(weights), "metrics": metrics})
        return 0
    except Exception as exc:  # noqa: BLE001
        emit({"event": "error", "message": str(exc)})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
