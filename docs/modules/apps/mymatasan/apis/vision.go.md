# Module: apps/mymatasan/apis/vision.go

## Purpose

Registers AI detection rule and alert routes for standalone `mymatasan`.

## Routes

| Method   | Path                              | Description |
|----------|-----------------------------------|-------------|
| `GET`    | `/api/vision/rules`               | List saved detection rules with `limit`/`offset` paging. |
| `POST`   | `/api/vision/rules`               | Create or update a detection rule. |
| `DELETE` | `/api/vision/rules/{id}`          | Delete a detection rule. |
| `GET`    | `/api/vision/alerts`              | List detection alert events with server-side filtering and paging (see query params below). |
| `POST`   | `/api/vision/alerts`              | Create an alert event; also triggers clip extraction when a recorder is configured. |
| `POST`   | `/api/vision/alerts/purge`        | Purge alert events older than `days` (int query param; 0 = all up to now). `onlyDiagnostics=true` limits deletion to diagnostic rows only. Returns `{"deleted": N}`. |
| `POST`   | `/api/vision/alerts/{id}/ack`     | Mark an alert as acknowledged by the current local user. |
| `GET`    | `/api/vision/alerts/{id}/snapshot`| Serve the alert's stored snapshot image (`?annotated=1` draws the detection box). |
| `GET`    | `/api/vision/classes`             | List detection class registry entries (built-in, trained, and groups). |
| `POST`   | `/api/vision/classes`             | Create or update a registry class/group. |
| `DELETE` | `/api/vision/classes/{id}`        | Delete a custom class/group (built-ins cannot be deleted). |

The `detectionType` field on a rule is the **mode** (`presence`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`, `lpr`); the **target classes** live in `ruleConfig.classes` and are resolved against the class registry at rule-load time. LPR rules instead carry their watchlist and match mode in `ruleConfig` (see `infra/vision/lpr.go`).

### GET /api/vision/alerts query parameters

| Param      | Type   | Notes |
|------------|--------|-------|
| `limit`    | int    | Page size (default behaviour inherited from `readPaging`). |
| `offset`   | int    | Page offset. |
| `cameraId` | int64  | Filter to a single camera. 0 means all cameras. |
| `status`   | string | `active` (unacknowledged real detections), `acknowledged`, `detections` (all real detections, ack or not), or `diagnostic`. Empty applies no status filter. |
| `filters`  | JSON   | DataTable-format `[]{fieldName, compare, value}` array, validated against `entities.AlertEvent` fields via `sharedapis.ParseListQueryOptions`. Drives true DB-side `WHERE` clauses — e.g. a `createdAt` date-range filter, `ruleId`, `confidence`, or `label`. |
| `sorters`  | JSON   | DataTable-format `[]{fieldName, sort}` array. Defaults to `CreatedAt DESC` when omitted. |

The legacy `createdAfter`/`createdBefore`/`ruleId`/`detectionType` query params were removed in favor of the generic `filters`/`sorters` pair — the frontend's Alert Log grid (`@shared/DataTable` in server mode) now emits column filters/sort directly in this shape, so paging always runs over the true filtered set rather than a client-side slice.

## Notes

- Route protection is provided by the app-level local Basic Auth middleware.
- The manual `POST /api/vision/alerts` path passes `frameCapturedAt = 0` to the recorder, which falls back to `time.Now()` as the clip anchor since no source frame is available.
- JSON request bodies are capped at 2 MiB.
- Request decoding rejects unknown JSON fields so frontend/API drift is caught early.
- Rule validation and alert validation are delegated to reusable `infra/vision` contracts.
- The Alert Log UI's default view is the latest detections (no filter, `CreatedAt DESC`); a "Today" button seeds a `createdAt` daterange filter for midnight-to-midnight local time instead of pre-computing epoch bounds client-side.
