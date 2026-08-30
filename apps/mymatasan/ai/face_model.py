"""Shared face detect+embed model for mymatasan face recognition.

Uses OpenCV's built-in YuNet (detection) + SFace (128-d embedding) via cv2.FaceDetectorYN /
cv2.FaceRecognizerSF. Both model files carry permissive licenses (YuNet MIT, SFace Apache-2.0) and
run on the opencv-python already used by the LPR path — no new heavy dependency (no insightface, no
onnxruntime, no torch needed for faces).

This module is imported by BOTH faces_worker.py (one-shot enrollment: embed a photo) and
yolo_worker.py's live face stage (recognize faces against the enrolled gallery), so the model is
loaded and aligned identically in both places. Alignment is mandatory: SFace embeddings degrade
badly on an unaligned crop, and YuNet conveniently emits exactly the 5 landmarks SFace's aligner
expects.
"""

import cv2
import numpy as np


class FaceModel:
    def __init__(self, yunet_path, sface_path, score_threshold=0.7, nms_threshold=0.3):
        # input_size is reset per-frame via setInputSize(); the constructor value is a placeholder.
        self.detector = cv2.FaceDetectorYN.create(
            yunet_path, "", (320, 320), score_threshold, nms_threshold, 5000
        )
        self.recognizer = cv2.FaceRecognizerSF.create(sface_path, "")

    # ENROLMENT LADDER. YuNet is an anchor-based detector: it finds faces within a band of sizes
    # RELATIVE TO ITS INPUT FRAME, and a photo somebody uploads sits outside that band at both ends.
    #
    # Measured against the shipped model, on a plain-background portrait:
    #   1200x1600 with the face at 35% of the height -> NOT FOUND at native size
    #   640x480   with the face at 60% of the height -> NOT FOUND
    #   413x531 (a 35x45mm passport photo at 300dpi), face at 75% -> NOT FOUND
    # A live camera frame never looks like that — it is ~640px wide with a small, distant face,
    # which is exactly the band YuNet wants, which is why recognition worked while enrolling a
    # passport photo returned "no face found in the image" and looked like the photo's fault.
    #
    # Two transforms fix the two ends: DOWNSCALE a big image (a 3000px-wide photo puts the face far
    # above the anchor range), and PAD a tight crop (a face filling the frame leaves the detector no
    # context; adding a margin makes it relatively smaller). Neither is guessable in advance, so the
    # ladder tries them in order and stops at the first rung that finds a face.
    #
    # Detection runs on the transformed copy; the coordinates are then mapped BACK and the crop is
    # aligned from the ORIGINAL image, so the faceprint is computed at full resolution rather than
    # from a 640px thumbnail.
    _ENROL_PADS = (0.0, 0.35, 0.75, 1.5)
    _ENROL_LONGEST = 640

    # LIVE LADDER. The same anchor-band problem, measured on the other side of it: on a 1920x1080
    # frame, detecting at NATIVE size finds a face only while it is roughly 7-20% of the frame
    # height. A person standing at the camera — the thing anybody tests first, and the whole point of
    # a door camera — is 30-70%, and was NOT DETECTED. No candidate reaches the rule, so no alert is
    # written and the roster never says "last seen": the feature looked switched on and did nothing.
    #
    # Detecting on a 640-long copy covers 14-74%, and the two together cover 7-74%. The cheap rung
    # runs FIRST and the native pass only when it finds nothing, which makes the case that was
    # broken the fastest one rather than the most expensive (measured on this machine, 1920x1080:
    # 13ms for the 640 rung, 88ms native — a frame with somebody at the camera now costs 13ms
    # instead of 88ms, and an empty frame costs 101ms instead of 88ms).
    _LIVE_LONGEST = (640, 0)  # 0 = native size

    def detect_embed(self, img, thorough=False):
        """Detect every face in a BGR image and return, per face:
            {vector: [128 float], box: [x,y,w,h] normalized, quality: score, aligned: ndarray}
        The aligned crop is returned so a caller can make a thumbnail; it is not serialized here.

        thorough=True runs the enrolment ladder above. It is OFF for the live stage on purpose:
        live frames are already in YuNet's comfortable range, and an empty frame — the common case,
        many times a second — would otherwise cost four detections instead of one.
        """
        if img is None or img.size == 0:
            return []
        h, w = img.shape[:2]
        if thorough:
            faces = self._detect_thorough(img)
        else:
            faces = self._detect_live(img)
        if faces is None:
            return []
        out = []
        for row in faces:
            x, y, fw, fh = float(row[0]), float(row[1]), float(row[2]), float(row[3])
            score = float(row[-1])
            aligned = self.recognizer.alignCrop(img, row)
            feat = self.recognizer.feature(aligned)  # shape (1, 128), float32
            vec = np.asarray(feat).flatten().astype(np.float32)
            out.append({
                "vector": vec.tolist(),
                "box": [max(0.0, x / w), max(0.0, y / h), fw / w, fh / h],
                "quality": score,
                "aligned": aligned,
            })
        return out


    def _detect_live(self, img):
        """The live ladder: a cheap downscaled pass, then the native one. Rows come back in ORIGINAL
        image coordinates either way, so the caller cannot tell which rung found the face."""
        for longest in self._LIVE_LONGEST:
            work, sx, sy = (img, 1.0, 1.0) if not longest else self._fit(img, longest)
            wh, ww = work.shape[:2]
            # YuNet silently misbehaves if the input size does not match the actual frame.
            self.detector.setInputSize((ww, wh))
            _, faces = self.detector.detect(work)
            if faces is None or len(faces) == 0:
                continue
            if sx == 1.0 and sy == 1.0:
                return faces
            return self._map_back(faces, sx, sy, 0, 0)
        return None

    def _detect_thorough(self, img):
        """Try the enrolment ladder, returning YuNet rows in ORIGINAL image coordinates."""
        import cv2 as _cv2  # local alias keeps the module import list unchanged

        h, w = img.shape[:2]
        for pad in self._ENROL_PADS:
            work, px, py = self._pad(img, pad)
            work, sx, sy = self._fit(work, self._ENROL_LONGEST)
            wh, ww = work.shape[:2]
            self.detector.setInputSize((ww, wh))
            _, faces = self.detector.detect(work)
            if faces is None or len(faces) == 0:
                continue
            return self._map_back(faces, sx, sy, px, py)
        return None

    @staticmethod
    def _pad(img, frac):
        """Add a margin of `frac` of each dimension, filled with the median EDGE colour.

        Constant fill, not BORDER_REPLICATE: replicating the edge of a photo whose subject touches
        the border smears a face across the new margin, and a smeared half-face that the detector
        reports as a SECOND face turns "no face found" into "more than one face in the image" —
        trading one wrong refusal for another.
        """
        import cv2 as _cv2

        if frac <= 0:
            return img, 0, 0
        h, w = img.shape[:2]
        px, py = int(w * frac), int(h * frac)
        edge = np.concatenate([img[0, :, :], img[-1, :, :], img[:, 0, :], img[:, -1, :]])
        fill = [int(v) for v in np.median(edge, axis=0)]
        return _cv2.copyMakeBorder(img, py, py, px, px, _cv2.BORDER_CONSTANT, value=fill), px, py

    @staticmethod
    def _fit(img, longest):
        """Downscale so the longest side is at most `longest`. Returns the actual per-axis scales,
        which are NOT identical to the requested one after integer rounding — and the mapping back
        has to use what was really applied, or every landmark lands slightly off."""
        import cv2 as _cv2

        h, w = img.shape[:2]
        if max(h, w) <= longest:
            return img, 1.0, 1.0
        s = longest / float(max(h, w))
        nw, nh = max(1, int(round(w * s))), max(1, int(round(h * s)))
        out = _cv2.resize(img, (nw, nh), interpolation=_cv2.INTER_AREA)
        return out, nw / float(w), nh / float(h)

    @staticmethod
    def _map_back(faces, sx, sy, px, py):
        """Map YuNet rows [x, y, w, h, 5 landmark xy pairs..., score] from the transformed copy back
        to the original image. Sizes divide by the scale; positions also lose the padding offset."""
        mapped = np.array(faces, dtype=np.float32, copy=True)
        for row in mapped:
            row[0] = row[0] / sx - px
            row[1] = row[1] / sy - py
            row[2] = row[2] / sx
            row[3] = row[3] / sy
            for i in range(4, 14, 2):
                row[i] = row[i] / sx - px
                row[i + 1] = row[i + 1] / sy - py
        return mapped


def cosine(a, b):
    """Cosine similarity of two 1-D float vectors (higher = more similar; SFace 'same' >= ~0.36)."""
    a = np.asarray(a, dtype=np.float32)
    b = np.asarray(b, dtype=np.float32)
    na = np.linalg.norm(a)
    nb = np.linalg.norm(b)
    if na == 0 or nb == 0:
        return 0.0
    return float(np.dot(a, b) / (na * nb))
