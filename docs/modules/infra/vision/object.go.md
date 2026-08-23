# Module: infra/vision/object.go

## Purpose

Maps object detector candidates to reusable vision detection rules.

## Responsibilities

- Define normalized object candidate and bounding-box shapes, including an optional appearance descriptor (`ObjectCandidate.Appearance []float32` + `AppearanceModel string`, W3-2 — see `vision.go.md`'s *WantAppearance frame gate*).
- Define the `ObjectDetector` interface for semantic detector backends.
- Apply rule class mapping, confidence thresholds, zone matching (single or multi-zone union via `boxCenterInAnyZone`), minimum frame count, and cooldown — the cooldown check routes through `cooldownActive` (`cooldown.go`), which seeds the in-process cooldown from the rule's persisted `LastTriggeredAt` on first sight, so cooldown survives a process restart instead of reading as zero.
- Convert matching candidates into reusable `Detection` results with bounding box and detector metadata JSON.
- Define `ObservationSink` (`Observe(cameraID, capturedAt, candidates)`) and `ObservationCapable` (`SetObservationSink`, `ObserveOnly`) — the seam the object metadata recorder (`apps/mymatasan/services.MetadataRecorder`) plugs into. `ObjectRuleDetector.Detect` forwards every candidate from the shared inference to the wired `observer` (not just rule matches) before evaluating rules, so a camera that already runs object rules pays no extra inference cost; `ObserveOnly` runs a detection pass purely to emit observations (no rule evaluation, no `Detection` results) for cameras with metadata recording on but no object rules to piggyback on.

## LPR detection branch

`Detect` has a four-way dispatch for rule types: crowd rules go to `crowdMatch`, LPR rules go to `lprMatch` (implemented in `lpr.go`), face rules go to `faceMatch` (implemented in `face.go`), and all other rules go to `bestCandidate`. When `lprMatch` succeeds, the plate string and vehicle attributes (`plate`, `ocrConfidence`, `vehicleType`, `color`, `watchlisted`) are promoted to top-level metadata so notification templates (`{{plate}}`, `{{vehicleType}}`, `{{color}}`) and filtering can reach them without parsing nested JSON. The alert label is set to the human-readable descriptor produced by `lprLabel` (e.g. `"Plate WXY1234 (white car)"`).

## Face detection branch

When `faceMatch` succeeds, the recognized identity (`personId`, `personName`, `faceConfidence`, `recognized`) is likewise promoted to top-level metadata — `personName` is empty for an unrecognized face — and the alert label is set via `faceLabel` (`"Alice (94%)"` or `"Unknown face"`). See `face.go.md` for the rule policy (known/include/unknown match modes).

## Notes

- `ObjectCandidate.Appearance`/`AppearanceModel` (W3-2) are populated by the persistent worker only when `Frame.WantAppearance` was set and only on labels worth describing (`person` and common vehicle labels); every other candidate carries a nil `Appearance` (`omitempty`). `ObjectRuleDetector.Detect` forwards these fields through to the observer untouched — this package never reads or ranks them itself, it is purely a carrier — so `apps/mymatasan/services.MetadataRecorder` can attach the peak-crop vector to the presence interval it writes (`services/metadata_recorder.go.md`) and `apps/mymatasan/services.AppearanceService` can later rank stored vectors (`services/appearance_search.go.md`).
- Candidate boxes are normalized from `0` to `1` and matched by box center against the rule's zone(s) via `boxCenterInAnyZone`. A rule's `ZonePolygon` can hold a single polygon or a list of polygons (multi-zone); a box counts if its center falls inside any one of them. Parsing (`parseZones`) and the underlying point-in-polygon test live in `motion.go` and are shared by every detector in this package (`bestCandidate` here, `crowdMatch` in `crowd.go`, `lprMatch` in `lpr.go`, `faceMatch` in `face.go`, `lineMatches` in `line_crossing.go`).
- Default class mappings cover `fire`, `smoke`, `person`, `vehicle`, `animal`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`, `lpr` (mapped to the `"license plate"` label by default), and `face` (mapped to the `"face"` label by default).
- `vehicle` maps common model labels such as `car`, `truck`, `bus`, `motorcycle`, and `bicycle`.
- `animal` maps common COCO animal labels such as `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`, plus custom-model labels such as `mouse` and `rat`.
- `intrusion` is treated as a rule type over person or vehicle objects unless the app routes it to motion fallback.
- `line_crossing` and `multi_line_crossing` rules use object centers, track matching, and line geometry from `line_crossing.go`.
- Rule `threshold` and detector `minObjectConfidence` are both applied; the effective minimum is the higher of the two values.
- If the backend implements `io.Closer`, this wrapper closes it during app shutdown.
