# Module: apps/myseliasan/apis/sites.go

## Purpose

Exposes the indoor-map half of the fleet map: site + floor-plan management, and node/camera
placements on a floor plan. A `Site` is also the **digital-twin building** on the geographic map —
it carries its own `Lat`/`Lon`/`MapPlaced` (mirroring `ManagedNode`'s, see `apis/nodes.go.md`) and
an `Icon` glyph, so a building — not the appliance that happens to record its cameras — anchors
cameras geographically. The offline basemap tile archive is in `apis/basemap.go.md`.

## Endpoints

All routes require a myseliasan session (`auth.Middleware` + `session.Middleware`).

| Method | Path | Notes |
|---|---|---|
| GET | `/api/sites` | List all sites (`Ordinal` then `Id` ascending). |
| POST | `/api/sites` | Create a site. Body: `{name, description, icon}`. `name` is required; `icon` is an operator-chosen emoji glyph shown on the geo-map building marker (empty = the default building glyph, rendered client-side). |
| GET | `/api/sites/overview` | Compact per-building rollup for the geographic map: `[]SiteOverview` (`site`, `nodeIds` — every node with a camera physically placed in this building, `cameraKeys` — `"<nodeId>::<cameraId>"` per camera, `cameras`, `floors`). The marker takes the *worst* status among `nodeIds` and attributes unread notifications **per camera** (`cameraKeys`), not per whole node, since one node can record cameras in several buildings. Registered before the `/{id}` routes (literal path) so it is never captured as an id. |
| PUT | `/api/sites/{id}` | Update a site's `{name, description, icon, ordinal}`. |
| DELETE | `/api/sites/{id}` | Delete a site and cascade-delete its floor plans (and their encrypted images + placements — see `DeleteFloor` below). |
| PUT | `/api/sites/{id}/position` | Set a building's geographic coordinates from an operator dragging its marker: `{lat, lon, placed}` — the exact counterpart of `PUT /api/nodes/{id}/position`. `placed` defaults to `true` when omitted (the common drag); an explicit `false` unplaces it (coordinates then unchecked). Otherwise `lat`/`lon` are validated to `[-90,90]`/`[-180,180]`. Returns the updated `Site`; 400 on an unknown site, out-of-range coordinates, or bad body. |
| GET | `/api/sites/{id}/floors` | List a site's floor plans (`Ordinal` then `Id` ascending). |
| POST | `/api/sites/{id}/floors` | Upload a floor-plan image. **Multipart form**: field `file` (the image), optional form `name` (falls back to the uploaded filename, then `"Floor plan"`), optional form `design` (drawn-plan vector JSON when saved from the in-app floor editor rather than an uploaded photo; empty otherwise). Capped at 24 MiB (`maxPlanUploadBytes`). Only `image/png`, `image/jpeg`, `image/gif` are accepted — content type is sniffed (`detectPlanType`) when the declared `Content-Type` isn't one of the three, so a wrong/missing header doesn't silently pass through as something else. Returns 400 on an unknown site, an unreadable image, or a disallowed type. |
| POST | `/api/sites/{id}/areas` | Create an **area** — a floor with no uploaded plan, a blank white canvas generated server-side. **JSON body**: `{name, ordinal, width, height}`; `name` is required, `width`/`height` are optional (service defaults/caps them). This is the building wizard's "this building has these areas" step (one call per area) and the "add an area" button in the building editor — both create JSON in one request rather than the frontend rasterising and uploading a blank PNG through the multipart `/floors` route above. Returns 400 on an unknown site or missing `name`. |
| GET | `/api/sites/{id}/floorplans` | A building's floor plans, each paired with **every** placement on it regardless of which node's camera it is (`[]NodeFloorplan`, `services.SiteFloorplans`) — the multi-node building drill-down the geo map's building marker opens into, distinct from the node-scoped `nodeFloorplans` below. |
| GET | `/api/floors/{id}` | Get one floor plan's metadata. |
| PUT | `/api/floors/{id}` | Update `{name, ordinal}`. |
| PUT | `/api/floors/{id}/model` | Rewrite a floor's **3D layout**: `{grid, scale, wallHeight, elevation}` JSON body. `grid` is the painted wall/floor cell model the in-app editor draws (and extrudes in its 3D tab); `scale` is metres-per-image-pixel, `wallHeight`/`elevation` are metres. Leaves the plan image and camera placements untouched — a distinct endpoint from the multipart image routes and the name/ordinal `updateFloor`, autosaved (debounced) by the floor editor as the operator draws. Returns `404` on an unknown floor. |
| DELETE | `/api/floors/{id}` | Delete a floor plan: removes its placements first (so no orphaned pins survive), then its encrypted image file(s) (rendered + background, if any), then the row. |
| GET | `/api/floors/{id}/image` | The **decrypted, rendered** plan image (background + any drawn shapes composited). Cookie-authed like every other route (so a plain `<img src="/api/floors/{id}/image">` works — same-origin, CSP `img-src 'self'` — while a link without a session does not). Sets `Cache-Control: private, max-age=300` and `X-Content-Type-Options: nosniff`. |
| POST | `/api/floors/{id}/image` | **Replace** an existing floor's image + design — used when the operator uploads a real plan (scan/CAD export) over a blank area's generated canvas, or re-saves a drawn/annotated plan. Same multipart shape as the upload above (`file`, optional `name`, optional `design`). Returns `404` on an unknown floor, `400` on an unreadable/disallowed image. |
| DELETE | `/api/floors/{id}/image` | **Clear** the plan picture, restoring the blank canvas the area started as (`services.ClearFloorImage`) — the inverse of the upload/replace routes above, and the only way to un-upload a plan short of `DELETE /api/floors/{id}` itself. The 3D model (`Grid`/`Scale`/`WallHeight`/`Elevation`) and every camera placement survive; only the picture, `Design`, and the pristine background are cleared. The building editor's **Remove plan** button calls this. Returns `404` on an unknown floor. |
| GET | `/api/floors/{id}/background` | The **decrypted, pristine background** image (the original uploaded photo, before any in-app drawing) — what the floor editor loads as its canvas background so re-editing never draws on an already-composited render. `404` when the plan has no background (a generated blank area, or one drawn from scratch). `Cache-Control: no-store` (unlike `/image`, this is only fetched while actively editing). |
| GET | `/api/floors/{id}/placements` | List a floor's node/camera markers. |
| POST | `/api/floors/{id}/placements` | Add a marker. Body: `{nodeId, cameraId, lastKnownName, x, y}`. `nodeId` is required; `cameraId` empty means the marker represents the node itself. A camera marker (non-empty `cameraId`) gets a default `70`° coverage arc on drop; a node/sensor marker gets none. Returns 400 on an unknown floor or missing `nodeId`. |
| PUT | `/api/placements/{id}` | Update a marker: `{x, y, heading, fov, mountHeight, pitch}`, every field an optional pointer — a drag sends only `x`/`y` (repositioning without touching the camera's aim), the FOV editor sends only `heading`/`fov` (re-aiming without moving the marker), and the 3D editor sends only `mountHeight`/`pitch` (metres above the floor / downward tilt in degrees, for the 3D coverage cone) without touching any other axis. |
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
- A `FloorPlan`'s image is either **uploaded** (`POST /api/sites/{id}/floors`, a photo/scan) or a
  **generated blank canvas** (`POST /api/sites/{id}/areas` — the building wizard's "areas" step
  and the building editor's "add an area" button both go through this, never the old
  client-rasterised-PNG-then-upload path). Either way the walls/rooms are then **drawn** in-app
  over it (the `floor_editor.js` canvas: Select/Wall/Room/Round/Door/Stairs/Erase tools, grid-lock,
  undo/redo, multi-select, rotate/flip, zoom/pan — one surface with a 2D⇄3D toggle, replacing the
  old standalone `floor_designer.js`/`indoor_map.js` pair). A drawn plan's vector shapes round-trip
  through `design` on upload/replace so it can be reopened and re-edited, not just re-rendered as
  a flat image; an uploaded photo can also be annotated later, at which point its original is
  preserved as the background (see `services/sites.go.md`'s `ReplaceFloorImage`), and
  `DELETE /api/floors/{id}/image` (**Remove plan** in the building editor) clears the picture back
  to a blank canvas without touching the walls/placements underneath (`ClearFloorImage`).
  Separately, the wall/room layout an operator paints (not the image) round-trips through
  `Grid`/`Scale`/`WallHeight`/`Elevation` via `PUT /api/floors/{id}/model` — this is what the 3D
  tab extrudes; `Grid` additionally carries `stairs[]`/`doors[]` (straight-flight stairs and wall
  openings, both authored in the same 2D canvas and extruded in 3D) alongside the wall
  `segments[]`.
- Camera markers on a floor render a **coverage wedge** (SVG/canvas arc, `Heading`/`Fov` on
  `NodePlacement`) in both the editor (`building_editor_dialog.js`'s `FloorEditor`, i.e.
  `floor_editor.js`) and the read-only plan viewer (`node_floor_view.js`'s `BuildingFloorView`),
  so an operator can see at a glance which part of a room a camera actually watches, not just
  where it is mounted. The same placement additionally carries `MountHeight`/`Pitch` for the 3D
  view's coverage cone (see `entities/node_placement.go.md`).
- **Digital-twin buildings — the map is buildings-only**: a `Site` doubles as the geo-map's
  building marker (its own `Lat`/`Lon`/`MapPlaced` + `Icon`), and `ManagedNode` separately carries
  a `SiteId` — the building the *appliance itself* resides in, set via
  `PUT /api/nodes/{id}/building` (`apis/nodes.go.md`). The frontend (`fleet_map.js`) never draws a
  node its own pin, assigned or not — a node with no `SiteId` yet is listed in the rail's
  "Appliances" section to be assigned rather than placed standalone on the map (the old
  building-less node pin, drag-to-place, and Nodes-layer toggle are gone); a node's *cameras* are
  separately anchored by their floor-plan placements, which is what makes the building — not the
  node — the map's true anchor for "where is this camera". `ManagedNode.Lat`/`Lon`/`MapPlaced` and
  `PUT /api/nodes/{id}/position` still exist at the API layer (unchanged) but are no longer driven
  by any reachable UI action. See `entities/managed_node.go.md`.
