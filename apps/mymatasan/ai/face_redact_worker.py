"""Face redaction worker for mymatasan evidence exports (W3-6b).

TWO SEPARATE STEPS, run as two invocations, and the split is deliberate:

    python face_redact_worker.py --detect <in.mp4> --yunet <yunet.onnx> --out <detections.json>
    python face_redact_worker.py --render <in.mp4> --cover <cover.json>

Between them, the GO side decides what actually gets covered — it widens each detection by a
margin and holds it across the frames either side (see services/face_redactor.go). That
arithmetic is the difference between a face being obscured and a face being visible for three
frames in the middle of a clip nobody will scrub through, so it lives where it can be
unit-tested and mutation-checked rather than in a worker nobody runs in CI.

This file therefore does only the two things that genuinely need OpenCV: find faces, and put
rectangles on frames.

The renderer writes ONE JSON header line to stdout and then raw BGR frames, so the caller can
start ffmpeg with the right size and rate without needing ffprobe (which the appliance's
ffmpeg install does not guarantee). Encoding, audio and the privacy-zone burn-in all stay on
the Go side, in one ffmpeg pass.

Coordinates are NORMALISED 0..1 everywhere, the same convention privacy zones use, so nothing
downstream has to know what resolution the camera happened to be recording at.
"""

import argparse
import json
import sys

import cv2


def _open(path):
    cap = cv2.VideoCapture(path)
    if not cap.isOpened():
        return None, None
    w = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
    h = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
    fps = float(cap.get(cv2.CAP_PROP_FPS) or 0.0)
    # A file whose header lies about its rate is common enough that a zero here must not
    # become a division later; the caller is told what we used.
    if not (fps > 0.1):
        fps = 15.0
    return cap, {"width": w, "height": h, "fps": fps}


def detect(args):
    cap, info = _open(args.detect)
    if cap is None:
        return {"error": "could not open the footage for face detection"}
    if info["width"] <= 0 or info["height"] <= 0:
        cap.release()
        return {"error": "the footage reports no frame size"}

    try:
        det = cv2.FaceDetectorYN.create(
            args.yunet, "", (info["width"], info["height"]), args.score, 0.3, 5000
        )
    except Exception as e:  # noqa: BLE001 - a missing/corrupt model is data, not a crash
        cap.release()
        return {"error": "face detector could not be loaded: %s" % e}

    w, h = float(info["width"]), float(info["height"])
    detections = []
    frames = 0
    failed = 0
    while True:
        ok, frame = cap.read()
        if not ok:
            break
        # setInputSize per frame: YuNet silently misbehaves when the declared size does not
        # match the actual frame, and a mid-file resolution change is possible in a
        # concatenated export.
        fh, fw = frame.shape[:2]
        boxes = []
        try:
            det.setInputSize((fw, fh))
            _, faces = det.detect(frame)
            if faces is not None:
                for row in faces:
                    boxes.append([
                        float(row[0]) / fw, float(row[1]) / fh,
                        float(row[2]) / fw, float(row[3]) / fh,
                        float(row[-1]),
                    ])
        except Exception:  # noqa: BLE001
            # A frame the detector could not scan is NOT a frame with no faces. It is
            # counted and reported, and the Go side refuses to call the result a face
            # redaction when any frame went unscanned — a partial scan that looks complete
            # is worse than no scan at all.
            failed += 1
        if boxes:
            detections.append({"f": frames, "b": boxes})
        frames += 1
    cap.release()

    return {
        "width": info["width"], "height": info["height"], "fps": info["fps"],
        "frames": frames, "failedFrames": failed, "detections": detections,
    }


def render(args):
    cover = {}
    with open(args.cover, "r", encoding="utf-8") as fh:
        doc = json.load(fh)
    for key, boxes in (doc.get("frames") or {}).items():
        cover[int(key)] = boxes

    cap, info = _open(args.render)
    if cap is None:
        return {"error": "could not open the footage for redaction"}

    out = sys.stdout.buffer
    # The header the caller needs before it can start an encoder. Written and FLUSHED before
    # a single frame, because the caller blocks on it.
    sys.stdout.write(json.dumps({
        "width": info["width"], "height": info["height"], "fps": info["fps"],
    }) + "\n")
    sys.stdout.flush()

    written = 0
    covered = 0
    while True:
        ok, frame = cap.read()
        if not ok:
            break
        fh_, fw_ = frame.shape[:2]
        for box in cover.get(written, ()):  # noqa: B007
            x = int(round(box[0] * fw_))
            y = int(round(box[1] * fh_))
            bw = int(round(box[2] * fw_))
            bh = int(round(box[3] * fh_))
            x2 = max(0, min(fw_, x + bw))
            y2 = max(0, min(fh_, y + bh))
            x = max(0, min(fw_, x))
            y = max(0, min(fh_, y))
            if x2 > x and y2 > y:
                # SOLID FILL, not blur — the same decision, for the same reason, as the
                # privacy-zone redaction this rides alongside: a blur invites the argument
                # that something could be recovered, and on a small region it sometimes can.
                frame[y:y2, x:x2] = 0
                covered += 1
        out.write(frame.tobytes())
        written += 1
    cap.release()
    out.flush()
    return {"frames": written, "boxesDrawn": covered}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--detect", help="footage to scan for faces")
    ap.add_argument("--render", help="footage to redact and stream as rawvideo")
    ap.add_argument("--yunet", help="path to the YuNet detector .onnx")
    ap.add_argument("--cover", help="JSON of per-frame boxes to fill (render mode)")
    ap.add_argument("--out", help="where to write the detection JSON (detect mode)")
    ap.add_argument("--score", type=float, default=0.5, help="detector confidence threshold")
    args = ap.parse_args()

    if args.detect:
        if not args.yunet:
            print(json.dumps({"error": "--yunet is required"}))
            return
        result = detect(args)
        if args.out and "error" not in result:
            with open(args.out, "w", encoding="utf-8") as fh:
                json.dump(result, fh)
            # The summary goes to stdout; the bulk goes to the file, so a long clip does not
            # have to survive a pipe.
            print(json.dumps({
                "frames": result["frames"], "failedFrames": result["failedFrames"],
                "detectedFrames": len(result["detections"]),
                "width": result["width"], "height": result["height"], "fps": result["fps"],
            }))
            return
        print(json.dumps(result))
        return

    if args.render:
        if not args.cover:
            print(json.dumps({"error": "--cover is required"}))
            return
        result = render(args)
        if "error" in result:
            # In render mode stdout is the video stream, so an error that happens before the
            # header can go there; after the header it would corrupt the stream. Either way
            # the caller also sees a non-zero frame count mismatch and the exit status.
            sys.stderr.write(json.dumps(result) + "\n")
            sys.exit(1)
        sys.stderr.write(json.dumps(result) + "\n")
        return

    print(json.dumps({"error": "one of --detect or --render is required"}))


if __name__ == "__main__":
    try:
        main()
    except Exception as e:  # noqa: BLE001
        sys.stderr.write(json.dumps({"error": str(e)}) + "\n")
        # Non-zero: an export must never treat a crashed redaction as a completed one.
        sys.exit(1)
