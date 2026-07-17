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

    def detect_embed(self, img):
        """Detect every face in a BGR image and return, per face:
            {vector: [128 float], box: [x,y,w,h] normalized, quality: score, aligned: ndarray}
        The aligned crop is returned so a caller can make a thumbnail; it is not serialized here.
        """
        if img is None or img.size == 0:
            return []
        h, w = img.shape[:2]
        # YuNet silently misbehaves if the input size does not match the actual frame.
        self.detector.setInputSize((w, h))
        _, faces = self.detector.detect(img)
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


def cosine(a, b):
    """Cosine similarity of two 1-D float vectors (higher = more similar; SFace 'same' >= ~0.36)."""
    a = np.asarray(a, dtype=np.float32)
    b = np.asarray(b, dtype=np.float32)
    na = np.linalg.norm(a)
    nb = np.linalg.norm(b)
    if na == 0 or nb == 0:
        return 0.0
    return float(np.dot(a, b) / (na * nb))
