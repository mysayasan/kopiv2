# Module: infra/vision/object.go

## Purpose

Maps object detector candidates to reusable vision detection rules.

## Responsibilities

- Define normalized object candidate and bounding-box shapes.
- Define the `ObjectDetector` interface for semantic detector backends.
- Apply rule class mapping, confidence thresholds, zone matching (single or multi-zone union via `boxCenterInAnyZone`), minimum frame count, and cooldown.
- Convert matching candidates into reusable `Detection` results with bounding box and detector metadata JSON.
- Define `ObservationSink` (`Observe(cameraID, capturedAt, candidates)`) and `ObservationCapable` (`SetObservationSink`, `ObserveOnly`) — the seam the object metadata recorder (`apps/mymatasan/services.MetadataRecorder`) plugs into. `ObjectRuleDetector.Detect` forwards every candidate from the shared inference to the wired `observer` (not just rule matches) before evaluating rules, so a camera that already runs object rules pays no extra inference cost; `ObserveOnly` runs a detection pass purely to emit observations (no rule evaluation, no `Detection` results) for cameras with metadata recording on but no object rules to piggyback on.

## LPR detection branch

`Detect` has a three-way dispatch for rule types: crowd rules go to `crowdMatch`, LPR rules go to `lprMatch` (implemented in `lpr.go`), and all other rules go to `bestCandidate`. When `lprMatch` succeeds, the plate string and vehicle attributes (`plate`, `ocrConfidence`, `vehicleType`, `color`, `watchlisted`) are promoted to top-level metadata so notification templates (`{{plate}}`, `{{vehicleType}}`, `{{color}}`) and filtering can reach them without parsing nested JSON. The alert label is set to the human-readable descriptor produced by `lprLabel` (e.g. `"Plate WXY1234 (white car)"`).

## Notes

- Candidate boxes are normalized from `0` to `1` and matched by box center against the rule's zone(s) via `boxCenterInAnyZone`. A rule's `ZonePolygon` can hold a single polygon or a list of polygons (multi-zone); a box counts if its center falls inside any one of them. Parsing (`parseZones`) and the underlying point-in-polygon test live in `motion.go` and are shared by every detector in this package (`bestCandidate` here, `crowdMatch` in `crowd.go`, `lprMatch` in `lpr.go`, `lineMatches` in `line_crossing.go`).
- Default class mappings cover `fire`, `smoke`, `person`, `vehicle`, `animal`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`, and `lpr` (mapped to the `"license plate"` label by default).
- `vehicle` maps common model labels such as `car`, `truck`, `bus`, `motorcycle`, and `bicycle`.
- `animal` maps common COCO animal labels such as `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`, plus custom-model labels such as `mouse` and `rat`.
- `intrusion` is treated as a rule type over person or vehicle objects unless the app routes it to motion fallback.
- `line_crossing` and `multi_line_crossing` rules use object centers, track matching, and line geometry from `line_crossing.go`.
- Rule `threshold` and detector `minObjectConfidence` are both applied; the effective minimum is the higher of the two values.
- If the backend implements `io.Closer`, this wrapper closes it during app shutdown.
