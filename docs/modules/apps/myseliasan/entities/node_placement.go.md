# Module: apps/myseliasan/entities/node_placement.go

## Purpose

`NodePlacement` pins a node — or one camera on a node — to a spot on a `FloorPlan` (see
`entities/site.go.md`). It is the **first myseliasan-owned record of a node's cameras**: the
control plane otherwise has no camera table of its own, cameras are fetched live from the node
over the tunnel. That live fetch returns nothing when the node is offline, so a placement carries
a name snapshot and stays meaningful and renderable while the node is unreachable.

## Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `FloorId` | Indexed FK (`idx:"floor"`) to the owning `FloorPlan`. |
| `NodeId` | Required. Always set — the `ManagedNode.NodeId` this marker belongs to. Part of the `(NodeId, CameraId)` unique key (`ukey:"camera"`) — see "Exclusive placement" below. |
| `CameraId` | Empty for a placement of the node itself (a sensor hub, or a camera node shown as one marker); non-empty pins a single camera on that node. Part of the `(NodeId, CameraId)` unique key (`ukey:"camera"`); empty participates in the key too, so a node's own marker is single-placement just like a camera. |
| `LastKnownName` | A snapshot of the node/camera label taken at placement time, shown when the node is offline and the live name cannot be fetched. |
| `X` / `Y` | Position in the floor image's pixel coordinate space (the OL pixel projection — see `FloorPlan.Width`/`Height`), so a marker sits exactly where it was dropped regardless of zoom. |
| `Heading` | The camera's facing direction in degrees clockwise from north (up on the plan image). Together with `Fov`, draws the coverage arc on the plan. Meaningless (ignored) for a node/sensor placement. |
| `Fov` | The field-of-view spread in degrees. `0` means "no arc" — a plain marker, the default for a node/sensor placement. A camera placement defaults to `70` on drop (`AddPlacement`); the operator then aims it (`Heading`) and widens/narrows it (`Fov`) via the on-marker FOV handles in the floor editor (`floor_editor.js`, opened from `building_editor_dialog.js`). |
| `MountHeight` | How high the camera sits above the floor, in **metres** (a wall-mounted camera is ~2.5m). `0` = use a sensible default. Together with `Heading`/`Fov`/`Pitch`, positions the camera's 3D coverage cone (see `floor_3d.js`). Meaningless for a node/sensor placement. |
| `Pitch` | The camera's downward tilt in degrees (`0` = looking level, `90` = straight down). `0` = use a sensible default. Set via the same on-marker inspector as `MountHeight`. |
| `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` | Standard audit columns. |

## Notes

- **Exclusive placement**: `(NodeId, CameraId)` is a **DB unique index** (`ukey:"camera"`,
  `ux_node_placement_camera`) — a camera is in exactly one physical place, so it holds exactly one
  pin fleet-wide. `services.siteService.AddPlacement` checks this before inserting (via
  `FindPlacementOf`) and refuses a second placement with `*services.ErrAlreadyPlaced`, which names
  the site/floor the existing pin sits in (`apis/sites.go`'s `addPlacement` maps this to `409` with
  `{reason:"alreadyPlaced", siteId, siteName, floorId, floorName}`) — the unique index is only the
  backstop against two concurrent requests both winning. Moving a camera means unplacing it first,
  then placing it at the new spot. `services.PlacementIndex` lists every placement fleet-wide with
  the site/floor it sits on (`GET /api/placements`), so the floor editor's palette can grey out an
  already-placed camera and say where. Backfilled on upgrade by migration
  `20260724-03-placement-unique-camera` (`app/app.go.md`), which drops orphaned placements (a floor
  that no longer exists) and dedupes existing rows (oldest pin per camera kept) before the index is
  added, since the unique index cannot be created over pre-existing duplicates.
- Deleting the owning `FloorPlan` cascades to its placements (`services.siteService.DeleteFloor`)
  so a removed plan never leaves orphaned pins.
- Deleting a `Site` deletes its floors, which in turn deletes their placements. A placement whose
  `FloorId` no longer resolves (e.g. cross-referenced from elsewhere) is tolerated by the
  placement service rather than treated as an error.
- `UpdatePlacement` takes `X`/`Y`/`Heading`/`Fov`/`MountHeight`/`Pitch` as `*float64` (nil = "not
  provided") so a plain drag (`x`/`y` only) never resets a camera's aim, the FOV editor
  (`heading`/`fov` only) never resets its position, and the 3D editor (`mountHeight`/`pitch`
  only) never resets either of the other two axes.
- Registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`. `Heading`/`Fov`
  and, later, `MountHeight`/`Pitch` (the 3D coverage cone) were each added by an explicit
  migration in `app.go`'s `Migrations()` (not just the auto-migrator), backfilled to `0` on any
  pre-existing row, so an upgrade never leaves a placement with a NULL that the non-pointer
  `float64` fields can't scan.
