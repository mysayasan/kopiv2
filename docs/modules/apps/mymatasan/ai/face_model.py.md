# Module: apps/mymatasan/ai/face_model.py

## Purpose

The shared face detect+embed model: OpenCV's YuNet (detection) + SFace (128-d embedding) via
`cv2.FaceDetectorYN` / `cv2.FaceRecognizerSF`. Imported by **both** `faces_worker.py` (one-shot
enrolment) and `yolo_worker.py`'s live face stage, so a face is found, aligned and embedded
identically in both places — an enrolled faceprint and a live faceprint have to be comparable.

Alignment is mandatory rather than a nicety: SFace embeddings degrade badly on an unaligned crop,
and YuNet emits exactly the five landmarks SFace's aligner expects.

## `detect_embed(img, thorough=False)`

Returns, per face, `{vector, box (normalized), quality, aligned}`. `aligned` is the cropped,
aligned face — the caller turns it into the enrolment thumbnail.

## The two ladders

`detect_embed` runs one of two scale ladders, because **YuNet finds faces within a band of sizes
RELATIVE to its input frame** and both callers were feeding it inputs outside that band — at
opposite ends of it.

| stage | what it is handed | what broke |
|-------|-------------------|------------|
| enrolment | a photo somebody uploads | a passport photo was refused: *"no face found"* |
| live | a frame a camera sends | **somebody standing at the camera was not detected at all** |

The live failure was the more serious of the two: no candidate reaches the rule, so no alert is
written, no clip, no notification, and the roster never says "last seen". The feature looked
switched on and did nothing.

## The live ladder (default)

Measured against the shipped model on a 1920×1080 frame — the resolution a face rule forces:

| face height | % of frame | native (before) | 640-long copy | either |
|---|---|---|---|---|
| 80–100px | 7–9% | ✔ | ✘ | ✔ |
| 150–220px | 14–20% | ✔ | ✔ | ✔ |
| 320px | 30% | **✘** | ✔ | ✔ |
| 450px | 42% | **✘** | ✔ | ✔ |
| 650px | 60% | **✘** | ✔ | ✔ |

Native alone covers 7–20%: a face across the room. Everything closer — the entire point of a door
camera, and the first thing anybody tests — was invisible.

So the live path detects on a **640-long copy first, then at native size**, stopping at the first
rung that finds a face, with the rows mapped back to original coordinates either way. Cheap rung
first is deliberate: on this machine the 640 rung is 13ms against 88ms native, so **the case that was
broken is now the fastest one** (a frame with somebody at the camera: 25ms, was 88ms and found
nothing), and an empty frame — which pays both rungs — costs about 138ms against 88ms. That extra
cost lands only on cameras with a face rule, once per sampling interval.

## The enrolment ladder (`thorough=True`)

The upload side of the same band problem. Measured against the shipped model on a plain-background
portrait, at native size:

| framing | faces found |
|---------|-------------|
| 640×480, head 35% of height (camera-like) | 1 |
| 413×531 (a 35×45mm passport photo at 300dpi), head 75% | **0** |
| 1200×1600, head 60% | **0** |
| 2448×3264, head 85% | **0** |

So enrolling a passport photo failed with *"no face found in the image — use a clear, front-facing
photo"*: a message that blames the photo for a property of the detector's input, and sends the
operator to find a better photo, which cannot help.

Two transforms fix the two ends, and neither is guessable in advance:

- **Downscale** to a longest side of 640 — a 3000px-wide photo puts the face above the anchor range.
- **Pad** with a margin — a head filling the frame leaves no context, and padding makes the face
  relatively smaller.

`thorough=True` tries them as a ladder — pad fractions `0.0, 0.35, 0.75, 1.5`, each followed by the
downscale — and stops at the first rung that finds a face. Padding is a **constant** fill of the
median edge colour, not `BORDER_REPLICATE`: replicating the edge of a photo whose subject touches the
border smears a face into the new margin, and a smeared half-face reported as a second face would
trade *"no face found"* for *"more than one face in the image"*.

Detection runs on the transformed copy; `_map_back` returns the rows in **original** image
coordinates (sizes divide by the actual per-axis scale, positions also lose the padding offset), so
`alignCrop` runs on the full-resolution original rather than a 640px thumbnail. The mapping uses the
scale that was really applied after integer rounding — not the requested one — or every landmark
lands slightly off.

**The live stage uses its own, shorter ladder** rather than this one: padding is what a tight
*crop* needs, and paying for up to four detections on every empty frame — the common case, once per
sampling interval on every face camera — would not be worth the one framing it adds there.

## Benched

`tools/fleetbench/bench_face_enrol_framing.py` covers **both** stages — five upload framings
(camera-style, passport photo, phone portrait, full-size photo, tight head-and-shoulders) and seven
camera framings from "far away" to "face fills the frame" — asserting that each yields **exactly
one** face (the Go side refuses anything else) and that each detected box lands on the subject,
because a mis-mapped box would still report one face and then embed the wrong pixels: a bad
enrolment poisons every future match, and a bad live crop names the wrong person. It uses the drawn
face from the W3-6b redaction bench, so no photograph of a real person is committed to the
repository. Everything it prints is ASCII, so a cp1252 Windows console cannot kill the run with its
own output.

## Notes

- `cosine(a, b)` is the similarity the worker's gallery match uses (SFace "same person" ≈ ≥0.36).
- A head cropped so tightly that the chin or crown is cut off still fails, which is why
  `ErrNoFace` now asks for "the whole head visible".
