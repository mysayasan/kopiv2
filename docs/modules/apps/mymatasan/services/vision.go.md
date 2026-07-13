# Module: apps/mymatasan/services/vision.go

## Purpose

Persists MyMataSan AI detection rules and alert events using the reusable `infra/vision` contracts.

## Responsibilities

- List detection rules ordered by latest update.
- Normalize and validate rule requests before persistence.
- Preserve original creation audit fields and `LastTriggeredAt` when updating an existing rule.
- `MarkRuleTriggered(ctx, ruleId, at)`: persists `LastTriggeredAt` on the rule row so its cooldown survives a process restart — cooldown state is otherwise process-local and starts empty on every boot, so before this a crash-restart loop could turn a rule's cooldown into an alert/notification/ffmpeg storm (see `infra/vision/cooldown.go.md`). No-op when `ruleId`/`at` are non-positive, when the rule no longer exists, or when `at` would move the stored time backwards (a late/out-of-order sample must not shorten the cooldown).
- `DeleteRulesForCamera(ctx, cameraId)`: delete every detection rule belonging to one camera, in batches of 500. Part of the camera-delete cascade (`app.go`): an orphaned rule previously kept the vision monitor sampling a camera that no longer existed, failing every interval and writing a capture-failed diagnostic alert each time.
- List alert events with true DB-side filtering, sorting, and paging: `cameraId` and `status` are mandatory base constraints, and arbitrary extra filters/sorters supplied by the caller (in `[]sqldataenums.Filter`/`[]sqldataenums.Sorter` form) are appended so a client grid's column filters/sort drive the query directly. Defaults to `CreatedAt DESC` when no sort is supplied.
- Normalize and validate alert events before persistence.
- Mark alert events as acknowledged with local user and timestamp audit fields.
- Purge alert events older than a unix-seconds cutoff (`PurgeAlerts`) or older than N days (`PurgeAlertsOlderThanDays`), unlinking each row's snapshot image file. Operates in oldest-first batches of 500 so large backlogs do not build an unbounded result set. The `onlyDiagnostics` flag restricts deletion to diagnostic rows (no snapshot to unlink), leaving real detections untouched.
- `PurgeAlertsForCamera(ctx, cameraId)`: delete EVERY alert event for one camera regardless of age, oldest-first in batches of 500, unlinking each row's (deduped) snapshot file. Returns the count deleted. Powers the per-camera "Purge now" action alongside `recordingService.PurgeAllForCamera`.

## Notes

- Rule and alert validation remains app-neutral in `infra/vision`.
- The service maps reusable vision models into MyMataSan entities so later apps can reuse detector contracts without inheriting MyMataSan persistence.
- The monitor writes generated detections through this service, while the API can also create alerts for smoke tests and integration checks.
- `GetAlerts(ctx, limit, offset, cameraId, status, extraFilters, extraSorters)` builds `CameraId = ?` (when `cameraId > 0`) and a `status`-driven filter (`active`/`acknowledged`/`detections`/`diagnostic`), then appends `extraFilters`/`extraSorters` verbatim — these normally originate from the API's `sharedapis.ParseListQueryOptions` parse of the client grid's `filters`/`sorters` query params.
- `PurgeAlertsOlderThanDays(days=0, ...)` uses `time.Now()` as the cutoff, effectively purging everything up to the present.
