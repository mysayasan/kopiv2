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
| `POST`   | `/api/vision/alerts/{id}/ack`     | Mark an alert as acknowledged by the current local user. |

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
