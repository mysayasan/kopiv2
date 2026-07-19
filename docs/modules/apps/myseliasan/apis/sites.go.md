# Module: apps/myseliasan/apis/sites.go

## Purpose

Exposes the indoor-map half of the fleet map: site + floor-plan management, and node/camera
placements on a floor plan. The geographic half (`Lat`/`Lon`/`MapPlaced` on `ManagedNode`) is in
`apis/nodes.go.md`; the offline basemap tile archive is in `apis/basemap.go.md`.

## Endpoints

All routes require a myseliasan session (`auth.Middleware` + `session.Middleware`).

| Method | Path | Notes |
|---|---|---|
| GET | `/api/sites` | List all sites (`Ordinal` then `Id` ascending). |
| POST | `/api/sites` | Create a site. Body: `{name, description}`. `name` is required. |
| PUT | `/api/sites/{id}` | Update a site's `{name, description, ordinal}`. |
| DELETE | `/api/sites/{id}` | Delete a site and cascade-delete its floor plans (and their encrypted images + placements — see `DeleteFloor` below). |
| GET | `/api/sites/{id}/floors` | List a site's floor plans (`Ordinal` then `Id` ascending). |
| POST | `/api/sites/{id}/floors` | Upload a floor-plan image. **Multipart form**: field `file` (the image), optional form `name` (falls back to the uploaded filename, then `"Floor plan"`), optional form `design` (drawn-plan vector JSON when saved from the in-app floor designer rather than an uploaded photo; empty otherwise). Capped at 24 MiB (`maxPlanUploadBytes`). Only `image/png`, `image/jpeg`, `image/gif` are accepted — content type is sniffed (`detectPlanType`) when the declared `Content-Type` isn't one of the three, so a wrong/missing header doesn't silently pass through as something else. Returns 400 on an unknown site, an unreadable image, or a disallowed type. |
| GET | `/api/floors/{id}` | Get one floor plan's metadata. |
| PUT | `/api/floors/{id}` | Update `{name, ordinal}`. |
| DELETE | `/api/floors/{id}` | Delete a floor plan: removes its placements first (so no orphaned pins survive), then its encrypted image file(s) (rendered + background, if any), then the row. |
| GET | `/api/floors/{id}/image` | The **decrypted, rendered** plan image (background + any drawn shapes composited). Cookie-authed like every other route (so a plain `<img src="/api/floors/{id}/image">` works — same-origin, CSP `img-src 'self'` — while a link without a session does not). Sets `Cache-Control: private, max-age=300` and `X-Content-Type-Options: nosniff`. |
| POST | `/api/floors/{id}/image` | **Replace** an existing floor's image + design — used when the in-app floor designer re-saves a drawn/annotated plan. Same multipart shape as the upload above (`file`, optional `name`, optional `design`). Returns `404` on an unknown floor, `400` on an unreadable/disallowed image. |
| GET | `/api/floors/{id}/background` | The **decrypted, pristine background** image (the original uploaded photo, before any in-app drawing) — what the floor designer loads as its canvas background so re-editing never draws on an already-composited render. `404` when the plan has no background (drawn from a blank canvas). `Cache-Control: no-store` (unlike `/image`, this is only fetched while actively editing). |
| GET | `/api/floors/{id}/placements` | List a floor's node/camera markers. |
| POST | `/api/floors/{id}/placements` | Add a marker. Body: `{nodeId, cameraId, lastKnownName, x, y}`. `nodeId` is required; `cameraId` empty means the marker represents the node itself. A camera marker (non-empty `cameraId`) gets a default `70`° coverage arc on drop; a node/sensor marker gets none. Returns 400 on an unknown floor or missing `nodeId`. |
| PUT | `/api/placements/{id}` | Update a marker: `{x, y, heading, fov}`, every field an optional pointer — a drag sends only `x`/`y` (repositioning without touching the camera's aim), the FOV editor sends only `heading`/`fov` (re-aiming without moving the marker). |
| DELETE | `/api/placements/{id}` | Remove a marker. |
| GET | `/api/node-floorplan/{nodeId}` | The floor plan(s) that carry a node's placements, each paired with that node's markers on it (`[]NodeFloorplan`, most-populated plan first). Drives the geo-map "Locate on plan" drill-down (`fleet_map.js`) — clicking a camera event opens the right floor with that camera focused, without the operator having to know which site/floor it's on. Its own top-level prefix (not nested under `/nodes` or `/floors`) so it never collides with either. |

## Notes

- `actorID(r)` / `pathID(r)` are small shared helpers: the former pulls the caller's user id off
  `JwtCustomClaims` for `CreatedBy`/`UpdatedBy` stamping, the latter parses and validates the
  `{id}` path variable (rejects non-positive/unparseable ids with a `false` ok).
- Floor-plan image bytes are **encrypted at rest** — see `services/sites.go.md`. Everything else
  here (site/floor/placement metadata) is plain rows, matching the pattern used elsewhere in this
  app for non-secret operational data.
- Placements are myseliasan's own record, not fetched from the node — that's the whole point:
  they survive the node going offline (`entities/node_placement.go.md`).
- A `FloorPlan` can be **drawn** in-app (the `floor_designer.js` canvas: rooms/walls/text/pen,
  grid-lock, undo/redo, multi-select, rotate/flip, zoom/pan) instead of only uploaded as a photo.
  A drawn plan's vector shapes round-trip through `design` on upload/replace so it can be
  reopened and re-edited, not just re-rendered as a flat image; an uploaded photo can also be
  annotated later, at which point its original is preserved as the background (see
  `services/sites.go.md`'s `ReplaceFloorImage`).
- Camera markers on a floor render a **coverage wedge** (SVG arc, `Heading`/`Fov` on
  `NodePlacement`) in both the editor (`indoor_map.js`) and the read-only plan viewer
  (`node_floor_view.js`), so an operator can see at a glance which part of a room a camera
  actually watches, not just where it is mounted.
