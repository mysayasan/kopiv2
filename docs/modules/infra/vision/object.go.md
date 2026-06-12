# Module: infra/vision/object.go

## Purpose

Maps object detector candidates to reusable vision detection rules.

## Responsibilities

- Define normalized object candidate and bounding-box shapes.
- Define the `ObjectDetector` interface for semantic detector backends.
- Apply rule class mapping, confidence thresholds, polygon zone matching, minimum frame count, and cooldown.
- Convert matching candidates into reusable `Detection` results with bounding box and detector metadata JSON.

## Notes

- Candidate boxes are normalized from `0` to `1` and matched by box center against the rule polygon.
- Default class mappings cover `fire`, `smoke`, `person`, `vehicle`, `animal`, `intrusion`, `line_crossing`, and `multi_line_crossing`.
- `vehicle` maps common model labels such as `car`, `truck`, `bus`, `motorcycle`, and `bicycle`.
- `animal` maps common COCO animal labels such as `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`, plus custom-model labels such as `mouse` and `rat`.
- `intrusion` is treated as a rule type over person or vehicle objects unless the app routes it to motion fallback.
- `line_crossing` and `multi_line_crossing` rules use object centers, track matching, and line geometry from `line_crossing.go`.
- Rule `threshold` and detector `minObjectConfidence` are both applied; the effective minimum is the higher of the two values.
- If the backend implements `io.Closer`, this wrapper closes it during app shutdown.
