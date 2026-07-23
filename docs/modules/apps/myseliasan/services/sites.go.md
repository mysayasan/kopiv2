# Module: apps/myseliasan/services/sites.go

## Purpose

Implements `ISiteService`, the backing service for `apis/sites.go`: sites, their uploaded
floor-plan images, and node/camera placements on those plans — the indoor half of the fleet map.

## Constructor

`NewSiteService(db, cipher, planDir)`:

| Param | Meaning |
|---|---|
| `db` | `dbsql.IDbCrud`, used to build the three generic repos (`Site`, `FloorPlan`, `NodePlacement`). |
| `cipher` | `*atrest.Cipher`; may be `nil` (at-rest encryption disabled). When set, floor-plan image bytes are AES-256-GCM encrypted before being written to disk — the same fleet cipher `app.go` resolves for the CA key/PSK (`services/secret_store.go.md`). |
| `planDir` | Absolute directory encrypted plan images are written to. `app.go` resolves this to `<dataDir>/floorplans`. |

## Responsibilities

- **Sites** — `ListSites`/`CreateSite`/`UpdateSite`/`DeleteSite`. `DeleteSite` lists the site's
  floors and calls `DeleteFloor` on each before deleting the site row itself, so a deleted site
  never leaves orphaned floor-plan files or placements behind. `CreateSite`/`UpdateSite` now also
  take an `icon` (the building glyph shown on the geo-map marker).
  - `UpdateSitePosition(ctx, id, lat, lon, placed, by)` — sets `Site.Lat`/`Lon`/`MapPlaced` from an
    operator dragging the building's marker on the geographic map, mirroring
    `INodeRegistry.UpdatePosition`. Returns `ErrSiteUnknown` for an unknown site.
  - `SiteOverview(ctx)` — rolls up **every** site for the geo map in one call: for each site, walks
    its floors' placements to collect the distinct owning `nodeIds` (for marker health — the worst
    status among them) and `cameraKeys` (`"<nodeId>::<cameraId>"`, for **per-camera** notification
    attribution, since one node can record cameras across several buildings and summing the whole
    node's alerts onto one building would over-count), plus `cameras`/`floors` counts. Cheap enough
    to compute for a whole (small) fleet on every request; no caching.
  - `SiteFloorplans(ctx, siteID)` — like `NodeFloorplans` below, but scoped to a **building**
    rather than a node: every floor of the site, each paired with **all** of its placements
    regardless of which node owns the camera. This is the multi-node building drill-down
    (`BuildingFloorView` in `node_floor_view.js`) — clicking a building marker shows every camera
    physically inside it, whichever node happens to record each one.
- **Floors** — `ListFloors`/`GetFloor`/`UpdateFloor`/`DeleteFloor`, plus:
  - `AddFloor(ctx, siteID, name, img, contentType, design, by)` — the **uploaded-image** path;
    thin wrapper over `addFloorBytes` (below) with `keepBg = (design == "")`, i.e. a plain
    uploaded photo keeps a pristine background copy, a drawn plan does not.
  - `AddBlankFloor(ctx, siteID, name, ordinal, width, height, by)` — the **generated-canvas**
    path used by `POST /api/sites/{id}/areas`: renders a white `image.NewRGBA` (via `image/draw`),
    PNG-encodes it, and stores it through `addFloorBytes` with `keepBg = false` (no original photo
    worth doubling on disk for a blank canvas). `width`/`height` default to
    `defaultBlankPlanW`/`H` (1600×1000 — the same size the old client-side rasteriser used, so
    areas made either way share a coordinate space) when `<= 0` or `> maxBlankPlanPx` (8000, an
    allocation-size guard: an RGBA buffer is 4 bytes/pixel). A non-zero `ordinal` is applied with
    a follow-up `UpdateById` after the row is created (the id is needed first to name the file).
  - `addFloorBytes(ctx, siteID, name, img, contentType, design, keepBg, by)` — the shared
    store-a-plan path both of the above now funnel through: decodes just the image header
    (`image.DecodeConfig`) to capture pixel `Width`/`Height` (these become the OL pixel-projection
    extent the frontend renders the plan in), creates the DB row first (to get an id to name the
    file), writes the image to `<planDir>/floor-<id>.img` — encrypted via `cipher.EncryptBytes`
    when a cipher is configured, plaintext otherwise — then stamps `ImagePath` on the row.
    `keepBg` additionally copies the same bytes to `<planDir>/floor-<id>.bg.img` and stamps
    `BgPath`, so the floor editor has a pristine original to draw on if the plan is later
    annotated in-app (an uploaded photo gets this; a generated blank canvas does not, since there
    is no "original" worth a second copy). Any failure after the DB row is created rolls back by
    deleting that row (and any file already written), so a failed upload/generate never leaves a
    half-created floor plan.
  - `ReplaceFloorImage(ctx, id, name, img, contentType, design, by)` — rewrites an existing
    floor's rasterised image + `Design`, used when the operator uploads a real plan over a
    generated blank canvas, or re-saves a plan from the in-app editor (`floor_editor.js`).
    `name`/`contentType`/`Width`/`Height`/`UpdatedBy`/`UpdatedAt` are all refreshed; `name == ""`
    leaves the existing name unchanged. **First-time annotation of a plain uploaded image** (the
    row has no `Design` and no `BgPath` yet, and this call is the first one to carry a non-empty
    `design`): before the new composite overwrites `ImagePath`, the *current* file is read back
    and copied to `BgPath` — this is the one moment the pristine original is captured for an
    uploaded photo that was never drawn on until now. Returns `ErrFloorUnknown`/`ErrBadImage`
    like `AddFloor`.
  - `FloorImage(ctx, id)` — reads the file at `ImagePath` and decrypts it (`cipher.DecryptBytes`)
    when a cipher is configured, returning the raw bytes + content type ready to serve.
  - `FloorBackground(ctx, id)` — same as `FloorImage` but reads `BgPath` instead; returns
    `ErrFloorUnknown` when `BgPath` is empty (a generated blank area, or a plan drawn from
    scratch, has no background image to serve), which the API maps to a plain 404 rather than an
    error the editor needs to special-case.
  - `UpdateFloorModel(ctx, id, grid, scale, wallHeight, elevation, by)` — rewrites a floor's 3D
    layout (the painted wall/floor grid, plus its real-world scale/wall-height/elevation in
    metres), leaving `ImagePath`/`BgPath` and placements untouched — the 3D view is authored
    independently of the 2D plan image, autosaved (debounced) by the in-app editor as the
    operator draws. Returns `ErrFloorUnknown` for an unknown floor.
  - `DeleteFloor(ctx, id)` — deletes the floor's placements first (`floorPlacements` internal
    helper), removes both on-disk image files (`ImagePath` and `BgPath`, via `removeImage`, which
    is a no-op on an empty path), then deletes the row. A floor that no longer exists is treated
    as already deleted (returns `nil`), not an error.
- **Placements** — `ListPlacements`/`AddPlacement`/`UpdatePlacement`/`DeletePlacement`.
  `AddPlacement` validates the floor exists first (`ErrFloorUnknown` otherwise) and gives a
  camera placement (non-empty `cameraId`) a default `70`° `Fov` on drop so it has a visible
  coverage arc immediately; a node/sensor placement (`cameraId == ""`) gets `Fov: 0` (no arc).
  `UpdatePlacement(ctx, id, x, y, heading, fov, mountHeight, pitch *float64, by)` takes every
  positional/orientation/3D field as a pointer — nil means "leave unchanged" — so a plain drag
  (`x`/`y`) never resets a camera's aim, the FOV editor (`heading`/`fov`) never resets its
  position, and the 3D editor (`mountHeight`/`pitch`) never resets either of the other two; it
  replaced the earlier plain `MovePlacement`. `ListPlacements` and the internal
  `floorPlacements` both use `Get` with an explicit `FloorId` filter rather than `GetByForeign` —
  the shared SQLite layer's `GetByForeign` hardcodes `limit=1`, so it can only ever return one
  child row; a real one-to-many list needs the explicit-filter form (the same gotcha noted in
  `services/node_registry.go.md`'s sibling `ListFloors`).
- **`NodeFloorplans(ctx, nodeID)`** — the geo-map drill-down query: finds every placement
  belonging to `nodeID` (again `Get` + explicit `NodeId` filter, not `GetByForeign`), groups them
  by `FloorId`, loads each referenced `FloorPlan` (skipping one that no longer resolves — the
  floor was deleted out from under the placements), and returns `[]NodeFloorplan{Floor,
  Placements}` sorted **most-populated plan first** (the building holding the most of this
  node's cameras). Backs `GET /api/node-floorplan/{nodeId}`, which the frontend's "Locate on
  plan" action (`fleet_map.js`'s `locateOnPlan`, `node_floor_view.js`) calls to jump straight to
  the right floor and focus the right marker.

## Errors

| Error | Meaning |
|---|---|
| `ErrSiteUnknown` | Referenced site does not exist (`AddFloor`, `AddBlankFloor`, `UpdateSite`). |
| `ErrFloorUnknown` | Referenced floor does not exist (`AddPlacement`, `UpdateFloor`, `UpdateFloorModel`, `GetFloor`, `FloorImage`, `ReplaceFloorImage`), or has no background image (`FloorBackground`). |
| `ErrBadImage` | Uploaded image bytes could not be decoded, or had zero width/height (`AddFloor`, `ReplaceFloorImage`). |

## Notes

- Floor-plan image bytes are never stored in the database — only metadata + dimensions
  (`entities/site.go.md`'s `FloorPlan`). The bytes live on disk under `planDir`, encrypted at
  rest whenever `cipher` is non-nil (default on — see `apps/myseliasan/README.md` → "Fleet secret
  encryption at rest" for the shared `security` config block that governs this cipher). Both
  `ImagePath` (rendered) and `BgPath` (pristine background, when present) are encrypted the same
  way.
- `isNoResultFoundErr` (shared with the rest of this package) turns a "no rows" condition from
  `Get` into an empty slice rather than propagating an error, so an empty site/floor list is not
  an error case for the API layer.
