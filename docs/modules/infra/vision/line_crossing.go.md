# Module: infra/vision/line_crossing.go

## Purpose

Adds object-backed line crossing and ordered multi-line crossing rule behavior for reusable vision detectors.

## Responsibilities

- Parse and validate line-crossing `ruleConfig` JSON.
- Track object candidates across frames by nearest normalized box center.
- Detect finite line-segment crossings from previous center to current center, with a perpendicular dead-band for hysteresis (see Notes).
- Trigger `line_crossing` rules when an allowed object crosses a configured line.
- Trigger `multi_line_crossing` rules only when the same tracked object crosses configured lines in array order.
- Attach track ID, object label, line ID, line index, and line count metadata to detections.

## Notes

- `ruleConfig.lines` supports at most five ordered line entries.
- Each line uses normalized `[x,y]` points from `0` to `1`.
- `direction` accepts `both`, `forward`, or `reverse` based on the configured line point order.
- **Direction hysteresis (`lineCrossingBand`, ~2% of frame)**: each `lineTrack` keeps a confirmed side (`lineTrack.sides`, keyed by line ID) per line, via `evaluateLineCrossing`/`lineSideOf`. A track's centre must move farther than `lineCrossingBand` past the line (perpendicular distance) before its side is re-confirmed as "crossed" — movement that merely jitters within the band is ignored. This was added because sub-pixel wobble right at the line previously flipped the raw `signedArea` sign on every frame, firing a crossing in **both** directions and defeating the one-way `direction` (`forward`/`reverse`) filter. A crossing is only reported when (a) the side actually flips past the band and (b) the straight path between the last confirmed centre and the current centre intersects the finite line segment (`segmentsIntersect`), which also correctly handles a slow crosser stepping through the band over several frames. `lineDirectionAllows(direction, side)` then applies the rule's `direction` filter to the confirmed crossing side. The older `crossedLine`/`directionMatches` per-frame check is retained only for the motion-centroid fallback detector (no per-object tracking, so no hysteresis state to keep).
- `classes` filters model labels such as `person`, `car`, or `truck`; empty values use the detector class map. Setting `classes` to `["*"]` enables a wildcard that matches **any** YOLO label — the line fires for any detected object regardless of class. The UI exposes this as an **Anything** toggle in the Object Classes panel.
- `maxSecondsBetweenLines`, `maxTrackDistance`, and `trackTtlSeconds` tune sequence timing and track matching.
- `lineMatches` gates candidates by the rule's `ZonePolygon` zone(s) (`parseZones` + `pointInAnyZone`, shared with every other detector in this package) before line-crossing geometry is evaluated, so a rule's line only fires for candidates whose box center is inside one of its zones.
- `ruleCooldownElapsed(state, rule DetectionRule, now, cooldown)` takes the whole rule (not just its ID) and delegates to `cooldownActive` (`cooldown.go`), so a rule's persisted `LastTriggeredAt` seeds the in-process cooldown map the first time this process sees it — a restart no longer resets every rule's cooldown to zero.

## The tracker moved out (W3-4)

`lineTrack` now embeds `trackCore` and the matcher lives in `infra/vision/track.go.md`,
shared with the dwell rules. Field access is unchanged (embedded fields are promoted); the
matching semantics are identical — ByteTrack id first, nearest centre within
`maxTrackDistance` second, one candidate per track per pass.
