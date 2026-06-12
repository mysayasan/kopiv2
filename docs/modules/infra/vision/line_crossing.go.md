# Module: infra/vision/line_crossing.go

## Purpose

Adds object-backed line crossing and ordered multi-line crossing rule behavior for reusable vision detectors.

## Responsibilities

- Parse and validate line-crossing `ruleConfig` JSON.
- Track object candidates across frames by nearest normalized box center.
- Detect finite line-segment crossings from previous center to current center.
- Trigger `line_crossing` rules when an allowed object crosses a configured line.
- Trigger `multi_line_crossing` rules only when the same tracked object crosses configured lines in array order.
- Attach track ID, object label, line ID, line index, and line count metadata to detections.

## Notes

- `ruleConfig.lines` supports at most five ordered line entries.
- Each line uses normalized `[x,y]` points from `0` to `1`.
- `direction` accepts `both`, `forward`, or `reverse` based on the configured line point order.
- `classes` filters model labels such as `person`, `car`, or `truck`; empty values use the detector class map. Setting `classes` to `["*"]` enables a wildcard that matches **any** YOLO label — the line fires for any detected object regardless of class. The UI exposes this as an **Anything** toggle in the Object Classes panel.
- `maxSecondsBetweenLines`, `maxTrackDistance`, and `trackTtlSeconds` tune sequence timing and track matching.
