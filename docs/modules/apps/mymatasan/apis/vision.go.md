# Module: apps/mymatasan/apis/vision.go

## Purpose

Registers AI detection rule and alert routes for standalone `mymatasan`.

## Routes

- `GET /api/vision/rules`: list saved detection rules.
- `POST /api/vision/rules`: create or update a detection rule with camera ID, detection type, zone polygon, per-rule schedule policy, threshold, minimum frames, cooldown, sound setting, and enabled state.
- `DELETE /api/vision/rules/{id}`: delete a detection rule.
- `GET /api/vision/alerts`: list detection alert events.
- `POST /api/vision/alerts`: create an alert event. The monitor uses the same service path internally after detector results are normalized.
- `POST /api/vision/alerts/{id}/ack`: mark an alert as acknowledged by the current local user.

## Notes

- Route protection is provided by the app-level local Basic Auth middleware.
- JSON request bodies are capped at 2 MiB.
- Request decoding rejects unknown JSON fields so frontend/API drift is caught early.
- Rule validation and alert validation are delegated to reusable `infra/vision` contracts.
