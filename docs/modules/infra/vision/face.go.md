# Module: infra/vision/face.go

## Purpose

Implements face-recognition rule matching, match-mode policy, and alert label rendering for the
reusable vision detection engine. Face recognition is the two-stage detect-then-recognize sibling of
LPR: the **worker** localizes each face, embeds it, and matches it against the global enrolled
gallery, emitting a candidate labelled `"face"` whose metadata carries the recognized
`personId`/`personName` (empty = unknown) and a match confidence. This file is the Go half — it
applies the **rule's policy**: which faces are worth an alert. (Compare `lpr.go`, where the OCR runs
in the worker and the watchlist match runs here.)

## Responsibilities

- Define the `faceConfig` rule config shape (`people`, `matchMode`, `minConfidence`) and parse it
  from the rule's `ruleConfig` JSON (`parseFaceConfig`), at `Detect` time so rule changes take effect
  immediately.
- Validate face rules via `validateFaceRule` (called from `ValidateDetectionRule`).
- Implement `(*ObjectRuleDetector) faceMatch` — iterate candidates, filter to the `"face"` label, zone
  membership (`boxCenterInAnyZone`), and the rule's match mode, then return the highest-confidence
  qualifying face as the representative candidate.
- Implement `faceAttributes` to pull `personId`/`personName`/`confidence` out of a candidate's
  worker-emitted metadata.
- Implement `faceLabel` to produce a human-readable alert label: `"Alice (94%)"` for a recognized
  person, `"Unknown face"` for an unrecognized one, `"Face detected"` when no match info is present.
- Implement `normalizeName`/`normalizeNameList`/`nameInList` so rule `people` entries and
  worker-emitted names compare consistently (lowercased, single-spaced).

## Match modes

| Mode | Behaviour |
|---|---|
| `known` | Fire on **any** recognized (enrolled) person — "tell me when someone we know is here". Default when no `people` list is configured. |
| `include` | Fire only when the recognized person's name is in the rule's `people` list (VIP/watchlist alert). |
| `unknown` | Fire only on **un**recognized faces — stranger detection. Deliberately names nobody; reports "an unknown face appeared". |

When `matchMode` is omitted, it defaults to `include` if `people` is non-empty, otherwise `known`.

## Config defaults

| Key | Default | Notes |
|---|---|---|
| `minConfidence` | `0.6` (`defaultMinFaceConfidence`) | The minimum cosine-match confidence to treat a face as recognized. Below it a face is treated as unknown rather than risk naming the wrong person — the dangerous failure mode for this feature. |
| `matchMode` | `"known"` or `"include"` | See above. |

`validateFaceRule` rejects an unrecognized `matchMode`, an `include` rule with an empty `people`
list, and a `minConfidence` outside `[0, 1]`.

## Notes

- `defaultFaceLabel = "face"` is the raw label the worker's face stage emits for every detected face
  (recognized or not) — see `apps/mymatasan/ai/yolo_worker.py`'s `_faces_detect`.
- The `Recognized` flag on `faceMatchInfo` is always set (true/false), meaningful for both filtering
  (`known`/`unknown` modes) and for the alert label.
- `object.go`'s `Detect` promotes `personId`/`personName`/`faceConfidence`/`recognized` to top-level
  alert metadata (so notification templates can reach them without parsing nested JSON) and sets the
  alert label via `faceLabel` — mirroring how `lpr.go`'s plate attributes are promoted.
- This file has no dependency on MyMataSan entities or any app-specific code; it only reads the
  metadata shape the Python worker already attaches to a `"face"` candidate.
- **Face recognition is biometric data.** The rule-matching logic here is dormant unless (1) an admin
  has enrolled at least one person via `/api/faces` and (2) a camera has an active `detectionType:
  "face"` rule — see `apps/mymatasan/services/face_gallery.go.md` for the enrollment/consent side.

## The confidence floor

`defaultMinFaceConfidence` is **0.40**, and it must not be raised above the worker's own naming
floor (`MYMATASAN_FACE_MIN_COS`, also 0.40; SFace's documented same-identity point is ~0.36).

It was 0.60, which made ONE decision — "is this the enrolled person?" — get made twice against
different numbers. Every genuine match in the 0.40–0.60 band was named by the worker and then
discarded here. Measured against the shipped model, on one subject enrolled from a passport photo
and seen on a 1080p camera, 2 of 7 ordinary views fell in that band (backlit at the camera, 0.50;
backlit across the room, 0.59). On a `known` rule they produced no alert at all; on an `unknown`
rule they were worse than nothing, reporting an **enrolled person as a stranger**.

`face_test.go` pins the relationship so raising it again fails the build rather than the site. A
site that wants to be stricter sets `minConfidence` per rule, which the camera's AI rules editor
exposes — the right place for a judgement about one doorway.

## UI note

All three match modes (`known` / `include` / `unknown`) have worked here since face rules shipped,
but nothing in the UI could choose between them: the People screen hardcoded `known` and the rules
editor did not know the type existed. Both now expose the choice — per camera on the People page,
and in full (mode + watchlist + minConfidence) in the camera's AI rules editor.
