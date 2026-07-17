"""One-shot face ENROLLMENT worker for mymatasan.

Invoked by the Go side once per enrollment photo/frame:

    python faces_worker.py --embed <image.jpg> --yunet <yunet.onnx> --sface <sface.onnx>

It detects the faces in the image, embeds each, and prints a single JSON line:

    {"faces": [{"vector": [128 floats], "box": [x,y,w,h], "quality": 0.98, "thumb": "<b64 jpeg>"}]}

or {"error": "..."}. The Go gallery service refuses an image that is not exactly one clear, large
face — a bad enrollment silently poisons every future match — so this worker just reports what it
finds and lets Go decide. This mirrors anomaly_worker.py's one-shot, JSON-on-stdout pattern.
"""

import argparse
import base64
import json
import sys

import cv2

from face_model import FaceModel


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--embed", required=True, help="path to the image to enroll")
    ap.add_argument("--yunet", required=True, help="path to the YuNet detector .onnx")
    ap.add_argument("--sface", required=True, help="path to the SFace recognizer .onnx")
    args = ap.parse_args()

    img = cv2.imread(args.embed)
    if img is None:
        print(json.dumps({"error": "could not read the image"}))
        return

    try:
        model = FaceModel(args.yunet, args.sface)
    except Exception as e:  # noqa: BLE001 - report model-load problems as data, not a crash
        print(json.dumps({"error": f"face model load failed: {e}"}))
        return

    faces = model.detect_embed(img)
    out = []
    for f in faces:
        thumb = ""
        try:
            ok, buf = cv2.imencode(".jpg", f["aligned"])
            if ok:
                thumb = base64.b64encode(buf.tobytes()).decode("ascii")
        except Exception:  # noqa: BLE001
            thumb = ""
        out.append({
            "vector": f["vector"],
            "box": f["box"],
            "quality": f["quality"],
            "thumb": thumb,
        })
    print(json.dumps({"faces": out}))


if __name__ == "__main__":
    try:
        main()
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"error": str(e)}))
        sys.exit(0)
