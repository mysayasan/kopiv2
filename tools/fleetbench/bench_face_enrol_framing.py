"""Bench: does the face detector find a face in the framings people actually produce?

Two stages, one root cause, and both were broken at opposite ends of it:

  ENROLMENT — the photo somebody uploads. A passport photo was refused with "no face found".
  LIVE      — the frame a camera sends. A person STANDING AT THE CAMERA was not detected, so no
              alert was written and the roster never said "last seen". The feature looked switched
              on and did nothing, which is the worst way for a security feature to fail.


WHY THIS EXISTS. YuNet is anchor-based: it finds faces within a band of sizes RELATIVE to its input
frame, and both stages fed it inputs outside that band. Measured against the shipped model:

  a 413x531 passport photo, head at 75%      -> NOT FOUND at native size
  a 1920x1080 frame, face 30-74% of height   -> NOT FOUND at native size (somebody at the camera)

Neither failure was about image QUALITY, and the enrolment message ("use a clear, front-facing
photo") actively sent people to look for a better photo, which cannot help.

WHAT IS MEASURED. Each framing below is run through the SAME FaceModel both workers use. The
assertion is not "the ladders are better on average": it is that every one of these framings — each
an ordinary thing for this product to be handed — yields exactly ONE face, because the Go side
refuses anything else, and that the detected box lands on the subject.

The subject is the drawn face the W3-6b redaction bench already uses, so this bench needs no
photograph of a real person to run in CI, and no biometric data is committed to the repository.

    python tools/fleetbench/bench_face_enrol_framing.py

Exits 0 when every case passes, 1 otherwise, and skips (0) when the models are not installed.
"""

import os
import sys

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
AI = os.path.join(REPO, "apps", "mymatasan", "ai")
sys.path.insert(0, AI)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

YUNET = os.path.join(AI, "face_detection_yunet_2023mar.onnx")
SFACE = os.path.join(AI, "face_recognition_sface_2021dec.onnx")

# (label, width, height, head height as a fraction of the image height)
ENROL_CASES = [
    ("camera-style frame      ", 640, 480, 0.35),
    ("passport photo 35x45mm  ", 413, 531, 0.75),
    ("phone portrait          ", 1200, 1600, 0.60),
    ("full-size photo         ", 2448, 3264, 0.85),
    ("tight head-and-shoulders", 600, 800, 0.90),
]

# What a camera sends, at the resolution a face rule forces. The last three are the ones that were
# silently undetectable: somebody walking up to the camera and standing in front of it.
LIVE_CASES = [
    ("far away  (face 7%)     ", 1920, 1080, 0.074),
    ("across the room (14%)   ", 1920, 1080, 0.139),
    ("a few steps away (20%)  ", 1920, 1080, 0.204),
    ("approaching (30%)       ", 1920, 1080, 0.296),
    ("at the camera (42%)     ", 1920, 1080, 0.417),
    ("face fills it (60%)     ", 1920, 1080, 0.602),
    ("720p, at the camera     ", 1280, 720, 0.45),
]


def main():
    if not (os.path.exists(YUNET) and os.path.exists(SFACE)):
        print("SKIP: face models are not installed (run the setup from People or Settings > AI)")
        return 0

    import cv2  # noqa: F401  (imported for the error message when opencv is missing)
    import numpy as np
    from bench_w36b_faceredact import draw_face
    from face_model import FaceModel

    model = FaceModel(YUNET, SFACE)

    def photo(w, h, frac):
        img = np.full((h, w, 3), 226, np.uint8)
        draw_face(img, w // 2, int(h * 0.48), int(h * frac))
        return img

    failures = []

    def run(title, cases, thorough):
        print("\n%s" % title)
        print("  %-26s %-12s %-22s" % ("framing", "one pass", "with the ladder"))
        for label, w, h, frac in cases:
            img = photo(w, h, frac)
            # One pass at native size: what the code did before either ladder existed.
            hh, ww = img.shape[:2]
            model.detector.setInputSize((ww, hh))
            _, raw = model.detector.detect(img)
            before = 0 if raw is None else len(raw)

            found = model.detect_embed(img, thorough=thorough)
            ok = len(found) == 1
            if not ok:
                failures.append("%s: found %d faces, want exactly 1" % (label.strip(), len(found)))
            elif True:
                # The crop matters as much as the count: a mis-mapped box would still report one
                # face and then embed the WRONG PIXELS — a bad enrolment poisons every future match,
                # and a bad live crop names the wrong person. The subject is centred in every case,
                # so its detected box must be too.
                x, y, bw, bh = found[0]["box"]
                cx, cy = x + bw / 2, y + bh / 2
                if not (0.35 < cx < 0.65 and 0.25 < cy < 0.75):
                    failures.append("%s: box centre (%.2f, %.2f) is not on the subject"
                                    % (label.strip(), cx, cy))
            print("  %-26s %-12s %-22s %s" % (
                label.strip(),
                "%d found" % before,
                "%d found (q=%.2f)" % (len(found), found[0]["quality"]) if found else "0 found",
                "ok" if ok else "FAIL",
            ))

    run("ENROLMENT - photos people upload:", ENROL_CASES, thorough=True)
    run("LIVE - frames a camera sends:", LIVE_CASES, thorough=False)

    if failures:
        print("\nFAILED:")
        for f in failures:
            print("  -", f)
        return 1
    total = len(ENROL_CASES) + len(LIVE_CASES)
    print("\nAll %d framings yield exactly one face, and every box lands on the subject." % total)
    return 0


if __name__ == "__main__":
    sys.exit(main())
