# Module: apps/myseliasan/services/report_floorplan.go

## Purpose

Composites a floor's decrypted plan image with camera/node placement pins for the Inventory
report (`reports.go`'s `Inventory` -> `renderFloorPlan` -> `renderFloorPlacements`). Draws
the authored wall/door/window/stairs/parking/raised-floor/outdoor-kit (road/tree/hedge/
ground) vector geometry from `FloorPlan.Grid` first (`report_floorgrid.go.md` — the editor
never bakes this into the stored plan image), then overlays a translucent field-of-view
wedge — clipped by whatever blocks that camera at its own mount height, so the wedge shows
real coverage rather than a decorative aim (`report_floorgrid.go.md`'s `buildOccluders`/
`visibleFrom`) — a marker disc, and a name label per placement on top — matching the
frontend's stacked read-only 2D overlay (`node_floor_view.js`) so a printed floor plan looks
like what the operator sees on screen.

## `renderFloorPlacements(planImage []byte, gridJSON string, scale, wallHeight float64, placements []*entities.NodePlacement) (image.Image, error)`

1. Decodes `planImage` into an `image.RGBA` canvas the same size as the source.
2. Calls `renderFloorGrid`, passing `scale` (the floor's metres-per-pixel) through so the
   outdoor kit's real-world-metres fields (road/hedge width, tree canopy) draw at the right
   size — `report_floorgrid.go.md` covers the fallback when a plan was never scaled.
3. Parses `gridJSON` a second time into a `floorGrid` (best-effort; a parse failure just means
   no occluders, not a failed render) and works out the metres-per-pixel to occlude with:
   `scale` when set, else derived from the grid's own `Unit`/`CellPx` (nominal 0.5 m/cell,
   the same fallback `renderFloorGrid` uses).
4. For each non-nil placement: flips its stored `(X,Y)` from the OpenLayers bottom-left,
   y-UP pixel space into the image's top-left, y-DOWN space (`cy := h - pl.Y`; grid geometry
   is already in image space and is drawn as-is — the two coordinate systems differ and this
   is the one place they are reconciled), and when `pl.Fov > 0` builds that camera's own
   `occluders` (`buildOccluders(grid, mountH, wallHeight, mpp)` — `report_floorgrid.go.md`
   — `mountH` from `pl.MountHeight`, defaulting to 2.5 m when unset) and draws a coverage
   wedge clipped by it (`drawFovWedge`, radius proportional to the plan:
   `max(50, min(w,h)*0.16)`), a marker (`drawMarker` — white ring + steel-blue fill so it
   stays visible on both dark and light plan areas), and a name label (`drawLabel`) when
   `pl.LastKnownName` is set. Rebuilding the occluder set per placement (rather than once for
   the floor) is deliberate: which geometry blocks a view depends on THAT camera's own mount
   height, so two cameras on the same floor can have different occluders even though the
   floor's walls never change.

## Pixel-blending primitives

The translucent fills are hand-rolled straight-alpha Porter-Duff compositing
(`blendPx`/`fillDisc`/`drawFovWedge`) rather than routed through a path rasteriser —
`golang.org/x/image/vector` was found to bleed colour outside the intended shape for a
translucent fill, so this file keeps every pixel write under direct control:

- `blendPx(dst, x, y, c)` — one pixel, out-of-bounds coordinates silently ignored so callers
  can scan a bounding box freely.
- `drawFovWedge(dst, cx, cy, r, headingDeg, fovDeg, col, occ occluders)` — fills a circular
  sector via `inArc` (all angles normalised to `[0,360)`, so a sector straddling north, e.g.
  `350°→30°`, is handled). Heading is degrees clockwise from north (image up = -Y), matching
  how placements store `Heading`, so the wedge points where the camera looks. Every candidate
  pixel that passes `inArc` is then checked against `occ` via `visibleFrom`
  (`report_floorgrid.go.md`'s coverage-occlusion section) and skipped when it is not reachable
  — a pixel behind a wall is not covered, it only used to look as though it were.
- `drawMarker`/`fillDisc` — a filled disc with a white ring underneath.
- `drawLabel(dst, x, y, text)` — the built-in 7x13 bitmap face (`basicfont.Face7x13`, no font
  file needed, keeping the renderer air-gap-clean) on a semi-opaque dark pill background so a
  camera name stays legible over any plan; non-Latin glyphs the face lacks are skipped, not
  substituted.

## Notes

- Registers `image/gif`, `image/jpeg`, `image/png` decoders via blank imports so
  `image.Decode` in this file (and `reports.go`'s `decodeImageBytes` for incident snapshots)
  can read any of the three formats a stored/uploaded plan or notification image might be in.
- Any decode failure on the plan image itself is returned (not swallowed), and
  `reports.go`'s `renderFloorPlan` turns it into an on-page note rather than dropping the
  floor's section silently — see `services/reports.go.md` -> "Inventory".
