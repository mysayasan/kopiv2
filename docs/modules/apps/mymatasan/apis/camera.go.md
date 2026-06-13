# Module: apps/mymatasan/apis/camera.go

## Purpose

Exposes HTTP endpoints for camera (ONVIF device) management in the MyMataSan app.

## Endpoints

| Method | Path                    | Handler          | Notes |
|--------|-------------------------|------------------|-------|
| GET    | `/api/cameras`          | `list`           | Returns all saved cameras with details. |
| POST   | `/api/cameras`          | `save`           | Creates a new camera entry. |
| GET    | `/api/cameras/{id}`     | `getById`        | Returns one camera by ID. |
| PUT    | `/api/cameras/{id}`     | `updateDetails`  | Updates `name` and `description` only; preserves all other camera fields. |
| DELETE | `/api/cameras/{id}`     | `delete`         | Deletes a saved camera. |

## updateDetails

Accepts `{ "name": string, "description": string }`.
Loads the existing camera record first to preserve all other fields (RTSP URI, credentials, ONVIF config, etc.), then replaces only `name` and `description` before calling `Save`.
Returns a `"succeed"` envelope on success.

## Notes

- The route group is registered under `/api/cameras` with Basic Auth applied to all camera routes.
- `PUT /api/cameras/{id}` was added to fix a bug where "Save Details" in the Camera tab silently reported success without persisting data (the previous implementation called `POST /api/cameras` which had no POST handler, causing the request to be silently dropped by the router).
