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

The `detectionType` field on a rule is the **mode** (`presence`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`, `lpr`); the **target classes** live in `ruleConfig.classes` and are resolved against the class registry at rule-load time. LPR rules instead carry their watchlist and match mode in `ruleConfig` (see `infra/vision/lpr.go`). The `alerts` list also accepts a `detectionType` query param for exact-match filtering.

### GET /api/vision/alerts query parameters

| Param           | Type  | Notes |
|-----------------|-------|-------|
| `limit`         | int   | Page size (default behaviour inherited from `readPaging`). |
| `offset`        | int   | Page offset. |
| `cameraId`      | int64 | Filter to a single camera. 0 means all cameras. |
| `createdAfter`  | int64 | Unix timestamp lower bound (inclusive). 0 means no lower bound. |
| `createdBefore` | int64 | Unix timestamp upper bound (exclusive). 0 means no upper bound. |

## Notes

- Route protection is provided by the app-level local Basic Auth middleware.
- The manual `POST /api/vision/alerts` path passes `frameCapturedAt = 0` to the recorder, which falls back to `time.Now()` as the clip anchor since no source frame is available.
- JSON request bodies are capped at 2 MiB.
- Request decoding rejects unknown JSON fields so frontend/API drift is caught early.
- Rule validation and alert validation are delegated to reusable `infra/vision` contracts.
- The Alert Log UI defaults to today's date range by computing `createdAfter`/`createdBefore` from midnight-to-midnight local time in the browser before sending the request.
