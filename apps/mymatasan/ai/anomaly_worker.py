#!/usr/bin/env python3
"""Anomaly ("spot anything unusual") fitter for MyMataSan Teach.

Learns what NORMAL looks like from good-only samples: every training image is
embedded with a pretrained torchvision backbone (resnet18 — already installed as
an ultralytics dependency), the embeddings become a memory bank, and calibration
images (held-back normal moments) set the alert threshold at mean + 3*sigma of
their nearest-neighbor distances. The live yolo_worker scores frames against the
saved bank the same way and emits a synthetic detection when a frame's distance
exceeds the threshold.

Protocol (JSON lines on stdout, ultralytics/torch noise on stderr):
  {"event":"start","train":34,"calib":8}
  {"event":"progress","done":10,"total":42}
  {"event":"score","image":"41","score":0.062}
  {"event":"completed","threshold":0.081,"bankSize":34}
  {"event":"error","message":"..."}
"""

from __future__ import annotations

import argparse
import contextlib
import json
import sys
from pathlib import Path

_REAL_STDOUT = sys.stdout
MAX_BANK = 800
KNN = 3


def emit(obj) -> None:
    print(json.dumps(obj, separators=(",", ":")), file=_REAL_STDOUT, flush=True)


def list_images(folder: str):
    return sorted(
        f for f in Path(folder).iterdir()
        if f.suffix.lower() in (".jpg", ".jpeg", ".png")
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--train", required=True, help="directory of normal images to learn from")
    parser.add_argument("--calib", required=True, help="held-back normal images that set the threshold")
    parser.add_argument("--out", required=True, help="output model file (.pt)")
    args = parser.parse_args()

    try:
        import torch
        import torchvision
        from torchvision import transforms
        from PIL import Image
    except Exception as exc:  # noqa: BLE001
        emit({"event": "error", "message": f"torch/torchvision not available: {exc}"})
        return 1

    try:
        device = "cuda" if torch.cuda.is_available() else "cpu"
        with contextlib.redirect_stdout(sys.stderr):
            backbone = torchvision.models.resnet18(weights=torchvision.models.ResNet18_Weights.IMAGENET1K_V1)
        backbone.fc = torch.nn.Identity()
        backbone.eval().to(device)

        prep = transforms.Compose([
            transforms.Resize((224, 224)),
            transforms.ToTensor(),
            transforms.Normalize(mean=[0.485, 0.456, 0.406], std=[0.229, 0.224, 0.225]),
        ])

        @torch.no_grad()
        def embed(path: Path):
            image = Image.open(path).convert("RGB")
            tensor = prep(image).unsqueeze(0).to(device)
            feat = backbone(tensor).squeeze(0)
            return torch.nn.functional.normalize(feat, dim=0).cpu()

        train_images = list_images(args.train)
        calib_images = list_images(args.calib)
        if len(train_images) < 3:
            emit({"event": "error", "message": "not enough normal samples to learn from"})
            return 1
        emit({"event": "start", "train": len(train_images), "calib": len(calib_images)})

        total = len(train_images) + len(calib_images)
        done = 0
        bank_list = []
        for image in train_images:
            bank_list.append(embed(image))
            done += 1
            if done % 10 == 0:
                emit({"event": "progress", "done": done, "total": total})
        bank = torch.stack(bank_list)
        if bank.shape[0] > MAX_BANK:
            keep = torch.randperm(bank.shape[0])[:MAX_BANK]
            bank = bank[keep]

        # Distance = 1 - mean cosine similarity of the K nearest bank entries.
        def score_of(feat) -> float:
            sims = bank @ feat
            k = min(KNN, sims.shape[0])
            top = torch.topk(sims, k).values
            return float(1.0 - top.mean().item())

        scores = []
        for image in calib_images:
            s = score_of(embed(image))
            scores.append(s)
            emit({"event": "score", "image": image.stem, "score": round(s, 6)})
            done += 1
            if done % 10 == 0:
                emit({"event": "progress", "done": done, "total": total})

        if scores:
            t = torch.tensor(scores)
            threshold = float(t.mean().item() + 3.0 * t.std(unbiased=False).item())
            # Never sit the threshold below the worst calibration sample — that
            # would guarantee false alarms on scenes we KNOW are normal.
            threshold = max(threshold, float(t.max().item()) * 1.05)
        else:
            threshold = 0.15  # cautious default when the dataset is tiny
        threshold = max(threshold, 1e-4)

        with contextlib.redirect_stdout(sys.stderr):
            torch.save({"version": 1, "bank": bank, "threshold": threshold, "knn": KNN}, args.out)
        emit({"event": "completed", "threshold": round(threshold, 6), "bankSize": int(bank.shape[0])})
        return 0
    except Exception as exc:  # noqa: BLE001
        emit({"event": "error", "message": str(exc)})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
