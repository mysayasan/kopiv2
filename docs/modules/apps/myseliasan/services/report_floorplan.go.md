# Module: apps/myseliasan/services/report_floorplan.go

## Purpose

Composites a floor's decrypted plan image with camera/node placement pins for the Inventory
report (`reports.go`'s `Inventory` -> `renderFloorPlan` -> `renderFloorPlacements`). Draws
the authored wall/door/window/stairs/parking/raised-floor vector geometry from
`FloorPlan.Grid` first (`report_floorgrid.go.md` — the editor never bakes this into the
stored plan image), then overlays a translucent field-of-view wedge, a marker disc, and a
name label per placement on top — matching the frontend's stacked read-only 2D overlay
(`node_floor_view.js`) so a printed floor plan looks like what the operator sees on screen.

## `renderFloorPlacements(planImage []byte, gridJSON string, placements []*entities.NodePlacement) (image.Image, error)`

1. Decodes `planImage` into an `image.RGBA` canvas the same size as the source.
2. Calls `renderFloorGrid` to draw the authored geometry.
3. For each non-nil placement: flips its stored `(X,Y)` from the OpenLayers bottom-left,
   y-UP pixel space into the image's top-left, y-DOWN space (`cy := h - pl.Y`; grid geometry
   is already in image space and is drawn as-is — the two coordinate systems differ and this
   is the one place they are reconciled), draws a coverage wedge when `pl.Fov > 0`
   (`drawFovWedge`, radius proportional to the plan: `max(50, min(w,h)*0.16)`), a marker
   (`drawMarker` — white ring + steel-blue fill so it stays visible on both dark and light
   plan areas), and a name label (`drawLabel`) when `pl.LastKnownName` is set.

## Pixel-blending primitives

The translucent fills are hand-rolled straight-alpha Porter-Duff compositing
(`blendPx`/`fillDisc`/`drawFovWedge`) rather than routed through a path rasteriser —
`golang.org/x/image/vector` was found to bleed colour outside the intended shape for a
translucent fill, so this file keeps every pixel write under direct control:

- `blendPx(dst, x, y, c)` — one pixel, out-of-bounds coordinates silently ignored so callers
  can scan a bounding box freely.
- `drawFovWedge(dst, cx, cy, r, headingDeg, fovDeg, col)` — fills a circular sector via
  `inArc` (all angles normalised to `[0,360)`, so a sector straddling north, e.g. `350°→30°`,
  is handled). Heading is degrees clockwise from north (image up = -Y), matching how
  placements store `Heading`, so the wedge points where the camera looks.
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
