# Module: infra/vision/motion.go

## Purpose

Provides the dependency-free reusable detector implementation: motion detection inside configured rule polygons.

## Responsibilities

- Decode JPEG frames into grayscale pixel buffers.
- Keep previous-frame state per camera.
- Parse the rule's `ZonePolygon` field into one or more zones (`parseZones`) and fall back to a single full-frame zone when the value is missing or invalid.
- Compare consecutive frames using a configurable pixel delta and stride.
- Compute the changed-pixel ratio inside the rule's zone(s), unioned via `pointInAnyZone`.
- Apply rule threshold, minimum frame count, and cooldown before returning detections.
- Emit detector metadata that includes the motion source and changed-frame ratio.

## Multi-zone support

`ZonePolygon` accepts either a single polygon `[[x,y],...]` (legacy, unchanged) or a list of polygons `[[[x,y],...],...]` (multi-zone). `parseZones` tries the multi-polygon shape first and falls back to `parseZone` (single-polygon) when that fails, so existing single-zone rules keep working with no migration. Every polygon in the set participates as a union: a pixel/point counts if it falls inside **any** zone (`pointInAnyZone`). `toPoints`/`fullFrameZone` are shared helpers used by both `parseZone` and `parseZones`. This module owns the parsing helpers (`parseZone`, `parseZones`, `toPoints`, `fullFrameZone`, `pointInPolygon`, `pointInAnyZone`) that every other detector (`object.go`, `crowd.go`, `lpr.go`, `line_crossing.go`) also imports.

## Notes

- The detector is intentionally simple and local-device friendly for the MVP.
- It is safe for concurrent use; camera state is protected by a mutex.
- It implements the shared `Detector` interface, so it can run as the whole detector in `motion` mode or as the intrusion fallback in `hybrid` mode.
