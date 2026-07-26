package services

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// This file renders a floor's AUTHORED geometry — walls, doors, windows, stairs, raised
// floors, parking — into the inventory report, matching the frontend's read-only 2D
// overlay (FloorPlanGrid in node_floor_view.js). The geometry lives as vectors in
// FloorPlan.Grid and is NEVER baked into the stored plan image (the editor only extrudes
// it in 3D), so a floor drawn with walls looks blank without this pass — which is exactly
// what "the report shows cameras but no walls" was. Coordinates are raw image pixels,
// top-left origin, y-DOWN — the same space Grid uses (camera placements differ: they are
// y-UP and get flipped by the caller).

// floorGrid is the v2 vector schema the editor writes (with the legacy cell fields kept
// for plans authored before it). Field names match the JSON the frontend stores.
type floorGrid struct {
	Version   int           `json:"version"`
	Unit      float64       `json:"unit"`
	CellPx    float64       `json:"cellPx"`
	Segments  []gridSeg     `json:"segments"`
	Doors     []gridOpening `json:"doors"`
	Windows   []gridOpening `json:"windows"`
	Stairs    []gridRect    `json:"stairs"`
	Parking   []gridRect    `json:"parking"`
	Platforms []gridRect    `json:"platforms"`
	Walls     [][]float64   `json:"walls"` // legacy painted cells [col,row]
}

type gridSeg struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// gridOpening is a door or window: a centre on a wall, a width, and the wall's angle (rad).
// Hf/Sf are the door hand flips (hinge end / swing side); windows ignore them.
type gridOpening struct {
	Cx float64 `json:"cx"`
	Cy float64 `json:"cy"`
	W  float64 `json:"w"`
	A  float64 `json:"a"`
	Hf bool    `json:"hf"`
	Sf bool    `json:"sf"`
}

// gridRect is an axis-aligned footprint (two corners) rotated about its centre by A (rad),
// shared by stairs, parking and raised floors; the extra fields are per-kind.
type gridRect struct {
	X1     float64 `json:"x1"`
	Y1     float64 `json:"y1"`
	X2     float64 `json:"x2"`
	Y2     float64 `json:"y2"`
	A      float64 `json:"a"`
	Dir    string  `json:"dir"`
	Steps  int     `json:"steps"`
	Height float64 `json:"height"`
	Down   bool    `json:"down"`
	Bays   int     `json:"bays"`
	Rise   float64 `json:"rise"`
}

// Palette matching the frontend overlay (node_floor_view.js).
var (
	gridWall  = color.RGBA{51, 65, 85, 255}   // #334155
	gridStair = color.RGBA{15, 118, 110, 255} // #0f766e
	gridPlat  = color.RGBA{161, 98, 7, 255}   // #a16207
	gridPark  = color.RGBA{71, 85, 105, 255}  // #475569
	gridGlaze = color.RGBA{56, 189, 248, 255} // #38bdf8
	gridDoor  = color.RGBA{180, 83, 9, 255}   // #b45309
	gridWinFr = color.RGBA{3, 105, 161, 255}  // #0369a1 window frame
)

// renderFloorGrid draws the authored geometry from a floor's Grid JSON onto dst. A blank
// or unparseable Grid is a no-op (the plan is then just its image + pins).
func renderFloorGrid(dst *image.RGBA, gridJSON string) {
	if strings.TrimSpace(gridJSON) == "" {
		return
	}
	var g floorGrid
	if err := json.Unmarshal([]byte(gridJSON), &g); err != nil {
		return
	}
	unit := g.Unit
	if unit == 0 {
		unit = g.CellPx
	}
	if unit == 0 {
		unit = 20
	}
	wallW := math.Max(3, unit*0.22)
	openings := append(append([]gridOpening{}, g.Doors...), g.Windows...)

	// Draw order matches the overlay: raised floors, parking, stairs sit UNDER the walls.
	for _, p := range g.Platforms {
		fillRotatedRect(dst, p, withAlpha(gridPlat, 36), gridPlat, math.Max(2, wallW*0.5))
		rise := p.Rise
		if rise <= 0 {
			rise = 0.6
		}
		drawCenteredLabel(dst, rectCx(p), rectCy(p), fmt.Sprintf("+%.2f m", rise), gridPlat)
	}
	for _, p := range g.Parking {
		fillRotatedRect(dst, p, withAlpha(gridPark, 20), gridPark, math.Max(2, wallW*0.4))
		n := p.Bays
		if n < 1 {
			n = 1
		}
		if n > 60 {
			n = 60
		}
		drawRectDividers(dst, p, n, gridPark)
	}
	for _, st := range g.Stairs {
		fillRotatedRect(dst, st, withAlpha(gridStair, 31), gridStair, math.Max(2, wallW*0.5))
		climb := st.Height
		if climb <= 0 {
			climb = 2.7
		}
		steps := st.Steps
		if steps <= 0 {
			steps = int(math.Round(climb / 0.18))
		}
		if steps < 2 {
			steps = 2
		}
		if steps > 40 {
			steps = 40
		}
		drawStairTreads(dst, st, steps, gridStair)
		mark := "UP"
		if st.Down {
			mark = "DN"
		}
		drawCenteredLabel(dst, rectCx(st), rectCy(st), mark, gridStair)
	}
	// Legacy painted-cell walls (filled squares) only when there are no vector segments.
	if len(g.Walls) > 0 && len(g.Segments) == 0 {
		cell := g.CellPx
		if cell == 0 {
			cell = unit
		}
		for _, cr := range g.Walls {
			if len(cr) < 2 {
				continue
			}
			fillAxisRect(dst, cr[0]*cell, cr[1]*cell, cell, cell, gridWall)
		}
	}
	// Walls: each segment broken by every opening that sits on it.
	for _, s := range g.Segments {
		for _, pc := range carveSegGo(s, openings, unit*0.6) {
			drawThickLine(dst, pc.X1, pc.Y1, pc.X2, pc.Y2, wallW, gridWall)
		}
	}
	// Doors: the architectural symbol (jambs + leaf + swing arc), matching the editor —
	// the read-only overlay draws only a gap, but a printed plan should show the door.
	for _, d := range g.Doors {
		drawDoor(dst, d)
	}
	// Windows: opaque frame body punched through the wall, a glazing bar, and jamb stops.
	for _, d := range g.Windows {
		drawWindow(dst, d, wallW)
	}
}

// drawDoor renders a door as jambs at each end, the leaf swung open, and a dashed-style
// quarter-circle swing arc — ported from the editor (floor_editor.js). cx,cy sit on the
// wall; a is the wall angle; hf/sf pick the door hand.
func drawDoor(dst *image.RGBA, d gridOpening) {
	ux, uy := math.Cos(d.A), math.Sin(d.A)
	nx, ny := -math.Sin(d.A), math.Cos(d.A)
	half := d.W / 2
	hs, ss := 1.0, 1.0
	if d.Hf {
		hs = -1
	}
	if d.Sf {
		ss = -1
	}
	hx, hy := d.Cx-ux*half*hs, d.Cy-uy*half*hs // hinge
	lx, ly := d.Cx+ux*half*hs, d.Cy+uy*half*hs // latch
	tx, ty := hx+nx*d.W*ss, hy+ny*d.W*ss       // open leaf tip
	// Jambs (short strokes across the wall at each end of the opening).
	drawThickLine(dst, hx-nx*3, hy-ny*3, hx+nx*3, hy+ny*3, 2, gridDoor)
	drawThickLine(dst, lx-nx*3, ly-ny*3, lx+nx*3, ly+ny*3, 2, gridDoor)
	// Leaf.
	drawThickLine(dst, hx, hy, tx, ty, 2, gridDoor)
	// Swing arc from latch to tip about the hinge, always the short 90° sweep.
	a0 := math.Atan2(ly-hy, lx-hx)
	a1 := math.Atan2(ty-hy, tx-hx)
	delta := a1 - a0
	for delta > math.Pi {
		delta -= 2 * math.Pi
	}
	for delta < -math.Pi {
		delta += 2 * math.Pi
	}
	drawArc(dst, hx, hy, d.W, a0, a0+delta, 1.5, gridDoor)
}

// drawWindow renders the plan window symbol in the window's own frame: an opaque white
// frame body breaking the wall, a glass-blue glazing bar, and heavy jamb end-stops.
func drawWindow(dst *image.RGBA, d gridOpening, wallW float64) {
	half := d.W / 2
	depth := 4.0 // frame stand-off from the wall centre line (px)
	// Frame corners in the window's own rotated frame → world space.
	corner := func(lx, ly float64) (float64, float64) {
		return rotatePt(d.Cx+lx, d.Cy+ly, d.Cx, d.Cy, d.A)
	}
	// Opaque white frame body (punches a clean break through the wall).
	for py := int(d.Cy - half - depth - 2); py <= int(d.Cy+half+depth+2); py++ {
		for px := int(d.Cx - half - depth - 2); px <= int(d.Cx+half+depth+2); px++ {
			lx, ly := rotatePt(float64(px)+0.5, float64(py)+0.5, d.Cx, d.Cy, -d.A)
			if math.Abs(lx-d.Cx) <= half && math.Abs(ly-d.Cy) <= depth {
				blendPx(dst, px, py, color.RGBA{255, 255, 255, 250})
			}
		}
	}
	// Frame outline.
	c0x, c0y := corner(-half, -depth)
	c1x, c1y := corner(half, -depth)
	c2x, c2y := corner(half, depth)
	c3x, c3y := corner(-half, depth)
	drawThickLine(dst, c0x, c0y, c1x, c1y, 2, gridWinFr)
	drawThickLine(dst, c1x, c1y, c2x, c2y, 2, gridWinFr)
	drawThickLine(dst, c2x, c2y, c3x, c3y, 2, gridWinFr)
	drawThickLine(dst, c3x, c3y, c0x, c0y, 2, gridWinFr)
	// Glazing bar down the middle (glass blue).
	g0x, g0y := corner(-half, 0)
	g1x, g1y := corner(half, 0)
	drawThickLine(dst, g0x, g0y, g1x, g1y, math.Max(2, wallW*0.5), gridGlaze)
	// Heavy jamb end-stops.
	j0x, j0y := corner(-half, -depth*1.35)
	j1x, j1y := corner(-half, depth*1.35)
	j2x, j2y := corner(half, -depth*1.35)
	j3x, j3y := corner(half, depth*1.35)
	drawThickLine(dst, j0x, j0y, j1x, j1y, 3, gridWinFr)
	drawThickLine(dst, j2x, j2y, j3x, j3y, 3, gridWinFr)
}

// drawArc strokes a circular arc from a0 to a1 (radians) about (cx,cy) as short chords.
func drawArc(dst *image.RGBA, cx, cy, r, a0, a1, width float64, col color.RGBA) {
	const steps = 24
	px, py := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
	for i := 1; i <= steps; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(steps)
		x, y := cx+r*math.Cos(a), cy+r*math.Sin(a)
		drawThickLine(dst, px, py, x, y, width, col)
		px, py = x, y
	}
}

// --- geometry carve (ported verbatim from plan_geometry.js) -----------------------

func openingSpanOnSeg(s gridSeg, d gridOpening, tol float64) (t0, t1 float64, ok bool) {
	vx, vy := s.X2-s.X1, s.Y2-s.Y1
	len2 := vx*vx + vy*vy
	if len2 < 1e-6 {
		return 0, 0, false
	}
	length := math.Sqrt(len2)
	t := ((d.Cx-s.X1)*vx + (d.Cy-s.Y1)*vy) / len2
	px, py := s.X1+t*vx, s.Y1+t*vy
	if math.Hypot(d.Cx-px, d.Cy-py) > tol {
		return 0, 0, false
	}
	da := math.Abs(math.Mod(math.Atan2(vy, vx)-d.A, math.Pi))
	da = math.Min(da, math.Pi-da)
	if da > 0.35 {
		return 0, 0, false
	}
	half := (d.W / 2) / length
	t0, t1 = t-half, t+half
	if t1 <= 0 || t0 >= 1 {
		return 0, 0, false
	}
	return math.Max(0, t0), math.Min(1, t1), true
}

func remainingSpans(s gridSeg, openings []gridOpening, tol float64) [][2]float64 {
	var spans [][2]float64
	for _, d := range openings {
		if t0, t1, ok := openingSpanOnSeg(s, d, tol); ok {
			spans = append(spans, [2]float64{t0, t1})
		}
	}
	if len(spans) == 0 {
		return [][2]float64{{0, 1}}
	}
	// sort by start (small n; insertion sort keeps it dependency-free)
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j][0] < spans[j-1][0]; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	var merged [][2]float64
	for _, sp := range spans {
		if len(merged) == 0 || sp[0] > merged[len(merged)-1][1] {
			merged = append(merged, sp)
		} else if sp[1] > merged[len(merged)-1][1] {
			merged[len(merged)-1][1] = sp[1]
		}
	}
	var walls [][2]float64
	cursor := 0.0
	for _, m := range merged {
		if m[0] > cursor+1e-4 {
			walls = append(walls, [2]float64{cursor, m[0]})
		}
		cursor = math.Max(cursor, m[1])
	}
	if cursor < 1-1e-4 {
		walls = append(walls, [2]float64{cursor, 1})
	}
	return walls
}

func carveSegGo(s gridSeg, openings []gridOpening, tol float64) []gridSeg {
	spans := remainingSpans(s, openings, tol)
	if len(spans) == 1 && spans[0][0] == 0 && spans[0][1] == 1 {
		return []gridSeg{s}
	}
	out := make([]gridSeg, 0, len(spans))
	dx, dy := s.X2-s.X1, s.Y2-s.Y1
	for _, sp := range spans {
		out = append(out, gridSeg{
			X1: s.X1 + dx*sp[0], Y1: s.Y1 + dy*sp[0],
			X2: s.X1 + dx*sp[1], Y2: s.Y1 + dy*sp[1],
		})
	}
	return out
}

// --- rect helpers -----------------------------------------------------------------

func rectCx(r gridRect) float64 { return (r.X1 + r.X2) / 2 }
func rectCy(r gridRect) float64 { return (r.Y1 + r.Y2) / 2 }

func withAlpha(c color.RGBA, a uint8) color.RGBA { return color.RGBA{c.R, c.G, c.B, a} }

// rotatePt rotates (x,y) about (cx,cy) by ang radians.
func rotatePt(x, y, cx, cy, ang float64) (float64, float64) {
	if ang == 0 {
		return x, y
	}
	dx, dy := x-cx, y-cy
	c, s := math.Cos(ang), math.Sin(ang)
	return cx + dx*c - dy*s, cy + dx*s + dy*c
}

// fillRotatedRect fills a footprint (translucent) and strokes its outline, honouring the
// footprint's own rotation about its centre.
func fillRotatedRect(dst *image.RGBA, r gridRect, fill, stroke color.RGBA, strokeW float64) {
	cx, cy := rectCx(r), rectCy(r)
	x0, x1 := math.Min(r.X1, r.X2), math.Max(r.X1, r.X2)
	y0, y1 := math.Min(r.Y1, r.Y2), math.Max(r.Y1, r.Y2)
	hw, hh := (x1-x0)/2, (y1-y0)/2
	// Fill: scan the AABB of the rotated rect, inverse-rotate each pixel into local frame.
	corners := [4][2]float64{}
	for i, cnr := range [4][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}} {
		px, py := rotatePt(cnr[0], cnr[1], cx, cy, r.A)
		corners[i] = [2]float64{px, py}
	}
	minx, miny := corners[0][0], corners[0][1]
	maxx, maxy := corners[0][0], corners[0][1]
	for _, c := range corners {
		minx, miny = math.Min(minx, c[0]), math.Min(miny, c[1])
		maxx, maxy = math.Max(maxx, c[0]), math.Max(maxy, c[1])
	}
	if fill.A > 0 {
		for py := int(miny); py <= int(maxy); py++ {
			for px := int(minx); px <= int(maxx); px++ {
				lx, ly := rotatePt(float64(px)+0.5, float64(py)+0.5, cx, cy, -r.A)
				if math.Abs(lx-cx) <= hw && math.Abs(ly-cy) <= hh {
					blendPx(dst, px, py, fill)
				}
			}
		}
	}
	// Stroke: outline the four rotated edges.
	for i := 0; i < 4; i++ {
		a, b := corners[i], corners[(i+1)%4]
		drawThickLine(dst, a[0], a[1], b[0], b[1], strokeW, stroke)
	}
}

// fillAxisRect fills an axis-aligned rectangle (legacy wall cells).
func fillAxisRect(dst *image.RGBA, x, y, w, h float64, col color.RGBA) {
	for py := int(y); py < int(y+h); py++ {
		for px := int(x); px < int(x+w); px++ {
			blendPx(dst, px, py, col)
		}
	}
}

// drawStairTreads draws the internal tread lines, rotated with the footprint.
func drawStairTreads(dst *image.RGBA, r gridRect, steps int, col color.RGBA) {
	cx, cy := rectCx(r), rectCy(r)
	x0, x1 := math.Min(r.X1, r.X2), math.Max(r.X1, r.X2)
	y0, y1 := math.Min(r.Y1, r.Y2), math.Max(r.Y1, r.Y2)
	w, h := x1-x0, y1-y0
	vertical := r.Dir == "n" || r.Dir == "s"
	for k := 1; k < steps; k++ {
		f := float64(k) / float64(steps)
		var ax, ay, bx, by float64
		if vertical {
			ax, ay, bx, by = x0, y0+h*f, x0+w, y0+h*f
		} else {
			ax, ay, bx, by = x0+w*f, y0, x0+w*f, y0+h
		}
		ax, ay = rotatePt(ax, ay, cx, cy, r.A)
		bx, by = rotatePt(bx, by, cx, cy, r.A)
		drawThickLine(dst, ax, ay, bx, by, 1, col)
	}
}

// drawRectDividers draws the bay dividers of a parking footprint, rotated with it.
func drawRectDividers(dst *image.RGBA, r gridRect, bays int, col color.RGBA) {
	cx, cy := rectCx(r), rectCy(r)
	x0, x1 := math.Min(r.X1, r.X2), math.Max(r.X1, r.X2)
	y0, y1 := math.Min(r.Y1, r.Y2), math.Max(r.Y1, r.Y2)
	w, h := x1-x0, y1-y0
	acrossX := w >= h
	for k := 1; k < bays; k++ {
		f := float64(k) / float64(bays)
		var ax, ay, bx, by float64
		if acrossX {
			ax, ay, bx, by = x0+w*f, y0, x0+w*f, y0+h
		} else {
			ax, ay, bx, by = x0, y0+h*f, x0+w, y0+h*f
		}
		ax, ay = rotatePt(ax, ay, cx, cy, r.A)
		bx, by = rotatePt(bx, by, cx, cy, r.A)
		drawThickLine(dst, ax, ay, bx, by, 1, col)
	}
}

// drawThickLine strokes a line of the given width with round caps, by filling every pixel
// within half-width of the segment (distance-to-segment gives the round ends for free).
func drawThickLine(dst *image.RGBA, x1, y1, x2, y2, width float64, col color.RGBA) {
	half := width / 2
	if half < 0.5 {
		half = 0.5
	}
	minx := int(math.Floor(math.Min(x1, x2) - half - 1))
	maxx := int(math.Ceil(math.Max(x1, x2) + half + 1))
	miny := int(math.Floor(math.Min(y1, y2) - half - 1))
	maxy := int(math.Ceil(math.Max(y1, y2) + half + 1))
	half2 := half * half
	for py := miny; py <= maxy; py++ {
		for px := minx; px <= maxx; px++ {
			if distToSegSq(float64(px)+0.5, float64(py)+0.5, x1, y1, x2, y2) <= half2 {
				blendPx(dst, px, py, col)
			}
		}
	}
}

func distToSegSq(px, py, x1, y1, x2, y2 float64) float64 {
	vx, vy := x2-x1, y2-y1
	len2 := vx*vx + vy*vy
	if len2 < 1e-9 {
		dx, dy := px-x1, py-y1
		return dx*dx + dy*dy
	}
	t := ((px-x1)*vx + (py-y1)*vy) / len2
	t = math.Max(0, math.Min(1, t))
	dx, dy := px-(x1+t*vx), py-(y1+t*vy)
	return dx*dx + dy*dy
}

// drawCenteredLabel writes a short label centred at (cx,cy) using the built-in bitmap face.
func drawCenteredLabel(dst *image.RGBA, cx, cy float64, text string, col color.RGBA) {
	face := basicfont.Face7x13
	tw := font.MeasureString(face, text).Ceil()
	th := face.Metrics().Ascent.Ceil()
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(int(cx)-tw/2, int(cy)+th/2),
	}
	d.DrawString(text)
}
