# Module: apps/mymatasan/services/vision.go

## Purpose

Persists MyMataSan AI detection rules and alert events using the reusable `infra/vision` contracts.

## Responsibilities

- List detection rules ordered by latest update.
- Normalize and validate rule requests before persistence.
- Preserve original creation audit fields and `LastTriggeredAt` when updating an existing rule.
- List alert events ordered newest first, with optional server-side filters for camera ID and created-at unix timestamp range (`createdAfter`, `createdBefore`).
- Normalize and validate alert events before persistence.
- Mark alert events as acknowledged with local user and timestamp audit fields.

## Notes

- Rule and alert validation remains app-neutral in `infra/vision`.
- The service maps reusable vision models into MyMataSan entities so later apps can reuse detector contracts without inheriting MyMataSan persistence.
- The monitor writes generated detections through this service, while the API can also create alerts for smoke tests and integration checks.
- `GetAlerts` applies up to three independent DB filters: `CameraId = ?`, `CreatedAt >= ?`, `CreatedAt < ?`. Zero values for `cameraId`, `createdAfter`, and `createdBefore` are treated as "no filter" for that dimension.
