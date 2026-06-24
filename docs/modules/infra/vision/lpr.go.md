# Module: infra/vision/lpr.go

## Purpose

Implements automatic license-plate recognition (ALPR/LPR) rule matching, plate normalization, watchlist comparison, and label rendering for the reusable vision detection engine.

## Responsibilities

- Define the `lprConfig` rule config shape (`plates`, `matchMode`, `minOcrConfidence`, `plateLabel`) and parse it from the rule's `ruleConfig` JSON.
- Validate LPR rules via `validateLPRRule` (called from `ValidateDetectionRule`).
- Implement `lprMatch` on `ObjectRuleDetector`: iterate candidates, filter by plate label, zone membership, and OCR confidence floor, then apply the watchlist match mode (`any` / `include` / `exclude`). Return the highest-OCR-confidence qualifying plate as the representative, surface its confidence as the candidate confidence so downstream threshold/streak machinery and alerts reflect read quality.
- Implement `plateAttributes` to extract the plate string, OCR confidence, vehicle type, and color from the candidate's free-form metadata map.
- Implement `lprLabel` to produce a human-readable alert label (`"Plate WXY1234 (white car)"`, `"Plate WXY1234"`, or `"License plate detected"`).
- Implement `normalizePlate` / `normalizePlateList` to uppercase and strip separators/whitespace so reads and watchlist entries compare on alphanumerics only (`"WXY 1234" == "wxy-1234"`).
- Implement `plateInList` with exact-match plus Levenshtein-≤1 fuzzy matching to absorb common single-character OCR errors (O/0, I/1, B/8) without matching arbitrary plates.
- Provide `levenshtein` (standard edit distance) used internally by `plateInList`.

## Match modes

| Mode | Behaviour |
|---|---|
| `any` | Fire on every readable plate; default when no watchlist is configured. |
| `include` | Fire only when the OCR'd plate matches a watchlist entry (VIP/fleet/family). |
| `exclude` | Fire only when the plate is NOT in the watchlist (unknown vehicle at a gate). |

When `matchMode` is omitted, it defaults to `include` if `plates` is non-empty, otherwise `any`.

## Config defaults

| Key | Default | Notes |
|---|---|---|
| `minOcrConfidence` | `0.5` | OCR reads below this are treated as "no plate" — neither fires a rule nor false-matches a watchlist entry. |
| `plateLabel` | `"license plate"` | Raw model label the worker emits for a localized plate; override when a custom plate model uses a different class name. |
| `matchMode` | `"any"` or `"include"` | See above. |

## Notes

- `lprConfig` is parsed at `Detect` time (not startup) so rule changes take effect immediately.
- Plate candidates arrive with their metadata already set by the Python worker's LPR stage: `plate`, `ocrConfidence`, `vehicleType`, `color`.
- The `Watchlisted` flag in `plateMatch` is always set (true/false) so it is available for notification templates; it is meaningful only for `include`/`exclude` modes.
- This file has no dependency on MyMataSan entities or any app-specific code.
