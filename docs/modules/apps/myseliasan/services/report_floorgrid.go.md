# Module: apps/myseliasan/services/report_floorgrid.go

## Purpose

Renders a floor's AUTHORED vector geometry — walls, doors, windows, stairs, parking bays,
raised floors ("platforms") — into the Inventory report, matching the frontend's read-only
2D overlay (`FloorPlanGrid` in `node_floor_view.js`). This geometry lives as vectors in
`FloorPlan.Grid` and is never baked into the stored plan image (the in-app editor only
extrudes it in 3D), so without this pass a floor drawn with walls printed with cameras but no
walls at all. Coordinates are raw image pixels, top-left origin, y-DOWN — the same space
`Grid` uses (camera placements differ: they are y-UP and are flipped by the caller in
`report_floorplan.go`).

## Schema types

`floorGrid` is the v2 vector schema the editor writes (JSON field names match what the
frontend stores), with the legacy `Walls [][]float64` painted-cell field kept for plans
authored before it:

| Field | Meaning |
|---|---|
| `Segments []gridSeg` | Wall centre-lines (`X1,Y1`→`X2,Y2`). |
| `Doors`/`Windows []gridOpening` | A centre `(Cx,Cy)` on a wall, width `W`, the wall's angle `A` (rad); `Hf`/`Sf` (doors only) pick the hinge end / swing side. |
| `Stairs`/`Parking`/`Platforms []gridRect` | Axis-aligned footprints (`X1,Y1`-`X2,Y2`) rotated `A` about their centre; per-kind extras (`Dir`/`Steps`/`Height`/`Down` for stairs, `Bays` for parking, `Rise` for platforms). |
| `Walls [][]float64` | Legacy painted grid cells `[col,row]`, drawn only when `Segments` is empty. |
| `Unit`/`CellPx` | Pixels-per-grid-unit; `Unit` wins, falls back to `CellPx`, then `20`. |

Palette constants (`gridWall`/`gridStair`/`gridPlat`/`gridPark`/`gridGlaze`/`gridDoor`/
`gridWinFr`) match the frontend overlay's colours exactly.

## `renderFloorGrid(dst *image.RGBA, gridJSON string)`

No-op on a blank/unparseable `gridJSON` (the plan is then just its image + pins). Draw order
matches the frontend overlay — platforms, parking, and stairs are filled UNDER the walls:

1. **Platforms** — translucent fill + outline, labelled with rise (`"+%.2f m"`, default
   `0.6`).
2. **Parking** — translucent fill + outline + bay dividers (`drawRectDividers`, 1–60 bays,
   clamped).
3. **Stairs** — translucent fill + outline + tread lines (`drawStairTreads`, 2–40 steps,
   derived from `Height`/0.18m rise-per-step when `Steps` is unset) + an `"UP"`/`"DN"` label.
4. **Legacy painted-cell walls** — filled squares, only when `len(Walls) > 0 &&
   len(Segments) == 0`.
5. **Wall segments** — each broken by every door/window opening that lies on it
   (`carveSegGo`/`remainingSpans`/`openingSpanOnSeg`, ported verbatim from the frontend's
   `plan_geometry.js` so the two never drift), stroked as thick rounded lines
   (`drawThickLine`).
6. **Doors** (`drawDoor`) — the architectural symbol: jambs at each end, the leaf swung open,
   and a quarter-circle swing arc (`drawArc`) — printed even though the read-only screen
   overlay draws only a gap, because a printed plan should show the door.
7. **Windows** (`drawWindow`) — an opaque white frame body punched through the wall, a
   glazing bar down the middle, and heavy jamb end-stops.

## Geometry helpers

- `carveSegGo`/`remainingSpans`/`openingSpanOnSeg` — projects each opening onto a wall
  segment's parametric `[0,1]` span, merges overlapping/adjacent spans, and returns the
  segments of wall that remain outside every opening. A direct Go port of the frontend's
  `plan_geometry.js`, kept in lockstep deliberately (see file header comment) rather than
  reimplemented independently.
- `rotatePt`/`fillRotatedRect`/`fillAxisRect`/`rectCx`/`rectCy`/`withAlpha` — rotation and
  fill primitives shared by platforms/parking/stairs.
- `drawThickLine`/`distToSegSq` — strokes a line of a given width with round caps by filling
  every pixel within half-width of the segment (distance-to-segment gives round ends for
  free, no separate cap-drawing code).
- `drawCenteredLabel`/`drawStairTreads`/`drawRectDividers` — small per-shape label/divider
  helpers.

## Notes

- Shares `blendPx`/`drawThickLine`'s underlying blend primitive with
  `report_floorplan.go.md`'s pixel compositing — both files draw onto the same `*image.RGBA`
  canvas in one pass (grid geometry first, then camera pins).
- Every numeric shape parameter (`Steps`, `Bays`, `Height`/`Rise`) is defaulted and clamped
  defensively (e.g. `Bays` 1–60, `Steps` 2–40) so a malformed or hand-edited `Grid` JSON
  cannot make the renderer allocate an unbounded number of divider/tread lines.
