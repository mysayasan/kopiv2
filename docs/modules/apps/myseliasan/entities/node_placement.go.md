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
| `NodeId` | Required. Always set — the `ManagedNode.NodeId` this marker belongs to. |
| `CameraId` | Empty for a placement of the node itself (a sensor hub, or a camera node shown as one marker); non-empty pins a single camera on that node. |
| `LastKnownName` | A snapshot of the node/camera label taken at placement time, shown when the node is offline and the live name cannot be fetched. |
| `X` / `Y` | Position in the floor image's pixel coordinate space (the OL pixel projection — see `FloorPlan.Width`/`Height`), so a marker sits exactly where it was dropped regardless of zoom. |
| `Heading` | The camera's facing direction in degrees clockwise from north (up on the plan image). Together with `Fov`, draws the coverage arc on the plan. Meaningless (ignored) for a node/sensor placement. |
| `Fov` | The field-of-view spread in degrees. `0` means "no arc" — a plain marker, the default for a node/sensor placement. A camera placement defaults to `70` on drop (`AddPlacement`); the operator then aims it (`Heading`) and widens/narrows it (`Fov`) via the on-marker FOV handles in `indoor_map.js`. |
| `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` | Standard audit columns. |

## Notes

- Keyed on `(NodeId, CameraId)` in spirit (not a DB uniqueness constraint) so a placement stays
  meaningful even while its node is unreachable and cannot be asked to confirm it still exists.
- Deleting the owning `FloorPlan` cascades to its placements (`services.siteService.DeleteFloor`)
  so a removed plan never leaves orphaned pins.
- Deleting a `Site` deletes its floors, which in turn deletes their placements. A placement whose
  `FloorId` no longer resolves (e.g. cross-referenced from elsewhere) is tolerated by the
  placement service rather than treated as an error.
- `UpdatePlacement` takes `X`/`Y`/`Heading`/`Fov` as `*float64` (nil = "not provided") so a
  plain drag (`x`/`y` only) never resets a camera's aim, and the FOV editor (`heading`/`fov`
  only) never resets its position.
- Registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`. `Heading`/`Fov`
  were added by an explicit migration in `app.go`'s `Migrations()` (not just the
  auto-migrator), backfilled to `0` on any pre-existing row, so an upgrade never leaves a
  placement with a NULL that the non-pointer `float64` fields can't scan.
