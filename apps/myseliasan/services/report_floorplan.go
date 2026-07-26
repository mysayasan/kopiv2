package services

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// renderFloorPlacements composites a floor's camera/node pins onto its plan image for
// the inventory report. The decrypted plan bytes already carry the drawn walls (the
// stored image IS the rendered plan), so this only overlays what the 2D map shows on
// top of it: a translucent field-of-view wedge per camera (Heading ± Fov/2), a marker
// disc, and a legible label from LastKnownName — the placement's offline-safe snapshot.
//
// The plan image is drawn first, then the authored wall/door/window/stairs geometry from
// gridJSON (FloorPlan.Grid) — which the editor never bakes into the image — then the
// camera pins on top, mirroring the frontend's stacked read-only view.
//
// COORDINATE CONVENTIONS (they differ, and must match the frontend exactly):
//   - Grid geometry is image-pixel space, top-left origin, y-DOWN — drawn as-is.
//   - A placement's stored (X,Y) is the OpenLayers pixel projection, bottom-left origin,
//     y-UP, so every marker/wedge is flipped with (h - Y) into the image's top-left space.
//
// The primitives blend pixels directly (straight-alpha Porter-Duff over) rather than via
// a path rasteriser: a translucent uniform through golang.org/x/image/vector was found to
// bleed colour outside the intended shape, so the fill is kept fully under our control.
func renderFloorPlacements(planImage []byte, gridJSON string, placements []*entities.NodePlacement) (image.Image, error) {
	src, _, err := image.Decode(bytes.NewReader(planImage))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)

	// Authored walls/openings/stairs/floors (the reason a grid-drawn plan looked blank).
	renderFloorGrid(dst, gridJSON)

	// Radius of the coverage wedge, proportional to the plan (matches fovRadius()).
	fovR := math.Max(50, math.Min(float64(w), float64(h))*0.16)
	steel := color.RGBA{43, 108, 176, 255}
	fovFill := color.RGBA{43, 108, 176, 70} // straight alpha → translucent

	for _, pl := range placements {
		if pl == nil {
			continue
		}
		cx := pl.X
		cy := float64(h) - pl.Y // flip y-up placement space into the image's y-down space
		if pl.Fov > 0 {
			drawFovWedge(dst, cx, cy, fovR, pl.Heading, pl.Fov, fovFill)
		}
		drawMarker(dst, cx, cy, steel)
		if pl.LastKnownName != "" {
			drawLabel(dst, cx+6, cy-6, pl.LastKnownName)
		}
	}
	return dst, nil
}

// blendPx composites a straight-alpha colour over one pixel (Porter-Duff source-over).
// Out-of-bounds coordinates are ignored so callers can scan a bounding box freely.
func blendPx(dst *image.RGBA, x, y int, c color.RGBA) {
	bnd := dst.Bounds()
	if x < bnd.Min.X || y < bnd.Min.Y || x >= bnd.Max.X || y >= bnd.Max.Y {
		return
	}
	a := float64(c.A) / 255
	if a <= 0 {
		return
	}
	i := dst.PixOffset(x, y)
	dst.Pix[i] = uint8(float64(c.R)*a + float64(dst.Pix[i])*(1-a))
	dst.Pix[i+1] = uint8(float64(c.G)*a + float64(dst.Pix[i+1])*(1-a))
	dst.Pix[i+2] = uint8(float64(c.B)*a + float64(dst.Pix[i+2])*(1-a))
	dst.Pix[i+3] = 255
}

// drawFovWedge fills a translucent circular sector centred at (cx,cy), spanning fovDeg
// degrees around headingDeg. Heading is degrees clockwise from north (up = -Y on the
// image), matching how placements store it, so the wedge points where the camera looks.
func drawFovWedge(dst *image.RGBA, cx, cy, r, headingDeg, fovDeg float64, col color.RGBA) {
	start := headingDeg - fovDeg/2
	end := headingDeg + fovDeg/2
	x0, x1 := int(cx-r)-1, int(cx+r)+1
	y0, y1 := int(cy-r)-1, int(cy+r)+1
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			dx := float64(px) + 0.5 - cx
			dy := float64(py) + 0.5 - cy
			if dx*dx+dy*dy > r*r {
				continue
			}
			// Angle clockwise from north: north = -Y, east = +X.
			ang := math.Atan2(dx, -dy) * 180 / math.Pi
			if inArc(ang, start, end) {
				blendPx(dst, px, py, col)
			}
		}
	}
}

// inArc reports whether ang (degrees) falls within the [start,end] sweep, all normalised
// to [0,360) so a sector straddling north (e.g. 350°→30°) is handled.
func inArc(ang, start, end float64) bool {
	n := func(a float64) float64 {
		a = math.Mod(a, 360)
		if a < 0 {
			a += 360
		}
		return a
	}
	ang, s, e := n(ang), n(start), n(end)
	if s <= e {
		return ang >= s && ang <= e
	}
	return ang >= s || ang <= e
}

// drawMarker paints a solid disc with a white ring so a pin stays visible over both
// dark plan lines and light background.
func drawMarker(dst *image.RGBA, cx, cy float64, col color.RGBA) {
	fillDisc(dst, cx, cy, 5.5, color.RGBA{255, 255, 255, 255})
	fillDisc(dst, cx, cy, 4, col)
}

func fillDisc(dst *image.RGBA, cx, cy, r float64, col color.RGBA) {
	x0, x1 := int(cx-r)-1, int(cx+r)+1
	y0, y1 := int(cy-r)-1, int(cy+r)+1
	rr := r * r
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			dx := float64(px) + 0.5 - cx
			dy := float64(py) + 0.5 - cy
			if dx*dx+dy*dy <= rr {
				blendPx(dst, px, py, col)
			}
		}
	}
}

// drawLabel writes a short caption with a semi-opaque dark pill behind it, so camera
// names stay readable over any plan. Uses the built-in 7x13 bitmap face (no font file
// needed, keeping the renderer air-gap-clean); non-Latin glyphs it lacks are skipped.
func drawLabel(dst *image.RGBA, x, y float64, text string) {
	face := basicfont.Face7x13
	tw := font.MeasureString(face, text).Ceil()
	th := face.Metrics().Height.Ceil()
	pad := 3
	bx0, by0 := int(x)-pad, int(y)-th
	bx1, by1 := int(x)+tw+pad, int(y)+pad
	// Pill background (translucent dark), blended per pixel.
	pill := color.RGBA{26, 32, 44, 200}
	for py := by0; py <= by1; py++ {
		for px := bx0; px <= bx1; px++ {
			blendPx(dst, px, py, pill)
		}
	}
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
		Face: face,
		Dot:  fixed.P(int(x), int(y)),
	}
	d.DrawString(text)
}
