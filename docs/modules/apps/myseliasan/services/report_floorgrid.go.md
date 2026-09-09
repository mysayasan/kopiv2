# Module: apps/myseliasan/services/report_floorgrid.go

## Purpose

Renders a floor's AUTHORED vector geometry — walls, doors, windows, stairs, parking bays,
raised floors ("platforms"), and the outdoor kit (roads, trees, hedges, ground) — into the
Inventory report, matching the frontend's read-only 2D overlay (`FloorPlanGrid` in
`node_floor_view.js`). This geometry lives as vectors in `FloorPlan.Grid` and is never baked
into the stored plan image (the in-app editor only extrudes it in 3D), so without this pass a
floor drawn with walls printed with cameras but no walls at all. Coordinates are raw image
pixels, top-left origin, y-DOWN — the same space `Grid` uses (camera placements differ: they
are y-UP and are flipped by the caller in `report_floorplan.go`) — **except** the outdoor
kit's own fields (road/hedge width, canopy radius, tree height/stem), which are real-world
METRES, the reason `renderFloorGrid` now takes the floor's scale.

## Schema types

`floorGrid` is the v3 vector schema the editor writes (JSON field names match what the
frontend stores), with the legacy `Walls [][]float64` painted-cell field kept for plans
authored before it:

| Field | Meaning |
|---|---|
| `Segments []gridSeg` | Wall centre-lines (`X1,Y1`→`X2,Y2`). |
| `Doors`/`Windows []gridOpening` | A centre `(Cx,Cy)` on a wall, width `W`, the wall's angle `A` (rad); `Hf`/`Sf` (doors only) pick the hinge end / swing side; `Sill`/`Head` (windows only, real-world METRES) are the glazing's bottom/top — unused by the drawn window symbol itself, but exactly what `buildOccluders` (below) needs to decide whether a camera at a given mount height sees through it. |
| `Stairs`/`Parking`/`Platforms []gridRect` | Axis-aligned footprints (`X1,Y1`-`X2,Y2`) rotated `A` about their centre; per-kind extras (`Dir`/`Steps`/`Height`/`Down` for stairs, `Bays` for parking, `Rise` for platforms). |
| `Roads`/`Hedges`/`Ground []gridPolyline` | A point run (`Pts []gridPt`) plus `Width`/`Height`/`Surface`/`Markings`/`Kerb` — one shape shared by all three because the geometry is identical; only the paint differs. `Ground`'s run is drawn CLOSED (an outline, not an open line). Real-world metres, not pixels. |
| `Trees []gridTree` | A point (`X,Y`) plus `Canopy`/`Height`/`Stem`/`Species`. `Stem` (the CLEAR height before the canopy starts) is the field that decides whether a camera below it can see past the tree — this is why the type exists rather than being folded into `Ground`. Real-world metres. |
| `Walls [][]float64` | Legacy painted grid cells `[col,row]`, drawn only when `Segments` is empty. |
| `Unit`/`CellPx` | Pixels-per-grid-unit; `Unit` wins, falls back to `CellPx`, then `20`. |

Palette constants (`gridWall`/`gridStair`/`gridPlat`/`gridPark`/`gridGlaze`/`gridDoor`/
`gridWinFr`) match the frontend overlay's colours exactly, as do the outdoor kit's
(`gridRoadAsphalt`/`gridRoadConc`/`gridRoadGravel`/`gridRoadPaved` via `roadColour(surface)`,
`gridHedge`, `gridTreeStroke`, `gridGrass`/`gridWater` via `groundColour(surface)`) against
the frontend's `ROAD_SURFACE`/`GROUND_FILL` palettes in `map/plan_objects.js`.

## `renderFloorGrid(dst *image.RGBA, gridJSON string, scale float64)`

No-op on a blank/unparseable `gridJSON` (the plan is then just its image + pins). `scale` is
the floor's metres-per-pixel (`FloorPlan.Scale`); a value `<= 0` (never scaled) falls back to
the same nominal 0.5 m/cell the editor itself assumes, so outdoor-kit geometry still prints —
a road at a guessed width beats a report that silently omits it. Draw order matches the
frontend overlay — ground, roads and hedges paint first (ground is the surface everything
else sits on), then platforms/parking/stairs are filled UNDER the walls, and trees paint LAST
so a canopy visibly covers whatever it overhangs:

1. **Ground** — filled polygon (`fillPolygon`, even-odd scanline) + outline, per `Surface`.
2. **Roads** — a kerb ribbon (`Width`+3 px, unless `Kerb` is explicitly `false`) then the
   carriageway ribbon coloured by `Surface` (`roadColour`), both via `strokePolyline`.
3. **Hedges** — a solid ribbon via `strokePolyline`, no kerb.
4. **Platforms** — translucent fill + outline, labelled with rise (`"+%.2f m"`, default
   `0.6`).
5. **Parking** — translucent fill + outline + bay dividers (`drawRectDividers`, 1–60 bays,
   clamped).
6. **Stairs** — translucent fill + outline + tread lines (`drawStairTreads`, 2–40 steps,
   derived from `Height`/0.18m rise-per-step when `Steps` is unset) + an `"UP"`/`"DN"` label.
7. **Legacy painted-cell walls** — filled squares, only when `len(Walls) > 0 &&
   len(Segments) == 0`.
8. **Wall segments** — each broken by every door/window opening that lies on it
   (`carveSegGo`/`remainingSpans`/`openingSpanOnSeg`, ported verbatim from the frontend's
   `plan_geometry.js` so the two never drift), stroked as thick rounded lines
   (`drawThickLine`).
9. **Doors** (`drawDoor`) — the architectural symbol: jambs at each end, the leaf swung open,
   and a quarter-circle swing arc (`drawArc`) — printed even though the read-only screen
   overlay draws only a gap, because a printed plan should show the door.
10. **Windows** (`drawWindow`) — an opaque white frame body punched through the wall, a
    glazing bar down the middle, and heavy jamb end-stops.
11. **Trees** — a translucent canopy disc (`fillCircle`) + stroked outline (`strokeCircle`) +
    a small solid trunk dot, radius from `Canopy` converted metres→pixels via `scale`.

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
- `strokePolyline` — draws a run of `gridPt` as a thick line via `drawThickLine`; `closed`
  joins the last point back to the first, which is what makes a `Ground` area an outline
  rather than an open run.
- `fillPolygon` — fills an arbitrary polygon by even-odd scanline (`sort`-ed edge crossings
  per row), for the same reason the rest of this file rasterises by hand: `x/image/vector`
  was found to bleed a translucent uniform outside the intended shape.
- `fillCircle`/`strokeCircle` — a filled disc (row-by-row half-width) and a stroked circle
  (line segments around the circumference), used for a tree's canopy.
- `drawCenteredLabel`/`drawStairTreads`/`drawRectDividers` — small per-shape label/divider
  helpers.

## Coverage occlusion (`buildOccluders`, `visibleFrom`)

A printed wedge that shows a camera covering ground a wall stands in front of is worse than one
that shows nothing, because it looks like an answer. `buildOccluders(g floorGrid, mountH, wallH,
mpp float64) occluders` filters a floor's geometry down to what actually blocks **that camera at
its own mount height** — the same rule `plan_geometry.js`'s `occluderSet` applies in the frontend:

- a wall segment blocks unless `mountH >= wallH`; when it doesn't, doorways are carved out of every
  wall unconditionally (`carveSegGo`) and a window is carved only when `mountH` falls strictly
  between its `Sill` and `Head` (defaulting to 0.9/2.1 m when unset) — otherwise the wall is left
  solid through the glazing.
- a hedge (`gridPolyline`, turned into a chain of `gridSeg`) blocks while `mountH < Height` (default
  1.6 m).
- a tree canopy becomes an `occDisc{X, Y, R}` (`Canopy` metres → pixels via `mpp`) only when `mountH`
  is strictly between `Stem` and `Height` (defaults 2.2/8 m) — below the clear stem or above the
  crown, it is not returned at all.
- roads/ground/parking/platforms never appear in an `occluders` value; they are flat.

`occluders{Segs []gridSeg, Discs []occDisc}` is the resulting geometry. `visibleFrom(cx, cy, tx, ty,
occ) bool` reports whether a target pixel is reachable from the camera past every one of them —
`segmentsCross` (open-segment intersection, endpoints excluded) against each `Segs` entry,
`segmentHitsDisc` (point-to-segment distance vs. radius, via the shared `distToSegSq`) against each
`Discs` entry — and `drawFovWedge` (`report_floorplan.go.md`) calls it per pixel of the sector,
skipping any pixel it returns false for.

**Two implementations of one rule, deliberately.** `plan_geometry.js` sweeps rays and returns a
polygon; this file tests each pixel of the sector directly instead of porting that sweep into a
rasteriser that already works per pixel — the per-pixel test is the simpler and more exact of the
two here. `report_floorgrid_parity_test.go`'s parity check still holds the two sides' ARRAY set
together (it checks array names against `floorGrid` json tags, not individual fields like `Sill`/
`Head`); it does not, and cannot, assert the two occlusion algorithms agree pixel-for-pixel — there
is no cross-language bench for that yet.

## Notes

- Shares `blendPx`/`drawThickLine`'s underlying blend primitive with
  `report_floorplan.go.md`'s pixel compositing — both files draw onto the same `*image.RGBA`
  canvas in one pass (grid geometry first, then camera pins).
- Every numeric shape parameter (`Steps`, `Bays`, `Height`/`Rise`) is defaulted and clamped
  defensively (e.g. `Bays` 1–60, `Steps` 2–40) so a malformed or hand-edited `Grid` JSON
  cannot make the renderer allocate an unbounded number of divider/tread lines. The outdoor
  kit follows the same rule (`Width`/`Canopy` default when zero/negative, e.g. a road defaults
  to 6 m, a hedge to 0.8 m, a canopy to 4.5 m).
- `report_floorgrid_parity_test.go` is a parity gate against the frontend's plan object
  registry (`views/react-webpack/src/views/components/map/plan_objects.js`): it reads the
  registry's `array:` declarations as data (a regex, not a JS parser) and asserts every one of
  them has a matching `floorGrid` json tag, so a new object type added to the editor either
  reaches this file too or fails the build with the array name that was missed, instead of the
  PDF silently omitting geometry the operator drew. It also asserts `renderFloorGrid` survives
  a `gridJSON` carrying arrays this build has never heard of (the same forward-compatible
  round-trip `readModel`/`writeModel` give the JS side) rather than panicking on it. This test
  is what caught the outdoor kit's four new arrays by name before `renderFloorGrid` knew about
  them.
