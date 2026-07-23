# Module: apps/myseliasan/entities/site.go

## Purpose

`Site` + `FloorPlan` — the indoor half of the fleet map. A `Site` is a named physical location
(a building, campus, or yard) that groups one or more `FloorPlan` images an operator drags
cameras and nodes onto. `Site` is **also** the digital-twin **building marker** on the geographic
view: it carries its own `Lat`/`Lon`/`MapPlaced`, mirroring `ManagedNode`'s fields of the same
name (`entities/managed_node.go.md`) — a building, not the appliance that happens to record its
cameras, is what anchors those cameras geographically. A node can carry a `SiteId` (the building
the *box itself* resides in, `entities/managed_node.go.md`); that is a separate, optional
relationship from a camera's floor-plan placement.

## `Site` Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `Name` | Required. |
| `Description` | Optional. |
| `Icon` | A single operator-chosen emoji glyph (🏢/🏭/🏠/…) shown on the geo-map building marker and in site pickers. Empty renders as the default building glyph client-side (`DEFAULT_BUILDING_ICON` in `building_wizard.js`, reused by `fleet_map.js`). Emoji needs no image asset and renders natively in the OpenLayers canvas marker. |
| `Ordinal` | Orders sites in the picker; lower shows first. |
| `Lat` / `Lon` | The building's position on the geographic fleet map, set by an operator dragging its marker via `PUT /api/sites/{id}/position` — the building-level counterpart of `ManagedNode.Lat`/`Lon`. |
| `MapPlaced` | Distinguishes "deliberately positioned" from the `(0,0)` zero value (a real coordinate, Gulf of Guinea) — same convention as `ManagedNode.MapPlaced`. `false` (every site's default, including ones created before this field existed) means the building is simply absent from the geographic map until placed. |
| `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` | Standard audit columns. |

## `FloorPlan` Fields

One uploaded plan image belonging to a site — a floor, a wing, a yard layout.

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `SiteId` | Indexed FK (`idx:"site"`) to the owning `Site`. |
| `Name` | Required. |
| `Ordinal` | Orders floors within a site (ground floor below first floor, etc.). |
| `ImagePath` | `json:"-"` — the on-disk location of the **encrypted, rendered** plan image (background + any drawn shapes composited together), relative to the data dir. Never served directly; `GET /api/floors/{id}/image` decrypts on the fly (see `services/sites.go.md`). |
| `BgPath` | `json:"-"` — the on-disk location of the **encrypted, pristine background** image: an uploaded plan photo, or empty when the plan is a generated blank canvas (`AddBlankFloor`) or was drawn from scratch. Served (decrypted) only by `GET /api/floors/{id}/background`, loaded as the in-app floor editor's canvas background so re-editing draws on the original image rather than an already-composited render. |
| `ContentType` | One of `image/png`, `image/jpeg`, `image/gif`. |
| `Width` / `Height` | The image's pixel dimensions, captured at upload (or, for a generated blank area, at creation) via `image.DecodeConfig`. These ARE the extent of the OpenLayers pixel projection the frontend renders the plan in — image coordinates are map coordinates, so a camera dropped at pixel `(412, 380)` is stored as exactly that. |
| `Design` | The JSON vector shapes (rooms/walls/text/pen strokes) when the plan was **drawn** in the in-app floor editor rather than uploaded as a photo — empty for an uploaded image. Lets the operator reopen and re-edit a drawn plan; saving re-rasterises the shapes to `ImagePath` and rewrites this field via `ReplaceFloorImage`. |
| `Grid` | The JSON wall/floor layout the operator paints in the floor editor's canvas — cell size plus wall segments/cells, in the SAME pixel space as `Width`/`Height` and a placement's `X`/`Y`. Empty when no layout has been authored yet, in which case the 3D view falls back to a perimeter box. Rewritten independently of `Design`/`ImagePath` via `PUT /api/floors/{id}/model` (`UpdateFloorModel`). |
| `Scale` | The real-world size of one image pixel, in **metres** (metres-per-pixel). `0` = unset — the 3D view then assumes a nominal building size so proportions still read correctly; with a real scale set, wall/mount heights and camera coverage are physically accurate. |
| `WallHeight` | The extruded wall height in **metres** for the 3D view. `0` = use the default storey height. |
| `Elevation` | This floor's base height in **metres** above the building's ground, used to stack floors vertically in the 3D view (ground floor `0`, first floor ~`WallHeight`, …). `0` = derive from `Ordinal` at render time. |
| `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` | Standard audit columns. |

## Notes

- The image bytes themselves are never in the database — only this metadata row. The bytes are
  encrypted at rest on disk with the same fleet cipher that protects the CA key/PSK
  (`infra/atrest`, see `apps/myseliasan/README.md` → "Fleet secret encryption at rest").
- An area's plan image is either **uploaded** (`POST /api/sites/{id}/floors`) or a **generated
  blank canvas** (`POST /api/sites/{id}/areas`, `services.AddBlankFloor` — a plain white PNG
  rendered server-side, defaulting to 1600×1000 and capped at 8000px/side); either way the
  operator then draws walls/rooms over it in the same in-app editor.
- Both entities are registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`.
  `Design` and `BgPath` (`FloorPlan`) and `Lat`/`Lon`/`MapPlaced`/`Icon` (`Site`) were all added by
  explicit migrations in `app.go`'s `Migrations()` (not just the auto-migrator), and `Grid`/
  `Scale`/`WallHeight`/`Elevation` (`FloorPlan`, 3D view) likewise, so an upgrade always has these
  columns even with `autoMigrate` off.
- See `entities/node_placement.go.md` for how nodes/cameras are pinned onto a `FloorPlan`, and
  how a camera's coverage arc (`Heading`/`Fov`) — and, in 3D, its coverage cone
  (`MountHeight`/`Pitch`) — is drawn over this image.
