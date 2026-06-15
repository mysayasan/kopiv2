package vision

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// AnnotatedBox is a labeled detection box (normalized 0..1 coordinates) to draw
// onto a frame.
type AnnotatedBox struct {
	Box   Box
	Label string
}

// boxOverlayColor is the cyan used for detection boxes, matching the AI Log
// detail overlay in the web UI.
var boxOverlayColor = color.RGBA{R: 0x3a, G: 0xd2, B: 0xf0, A: 0xff}

// AnnotateJPEG decodes a JPEG, draws each labeled bounding box (cyan outline with
// a filled label tag, mirroring the AI Log detail overlay), and re-encodes to
// JPEG. On any decode/encode failure — or with no boxes — it returns the original
// bytes unchanged so callers always have something to send.
func AnnotateJPEG(data []byte, boxes []AnnotatedBox, quality int) []byte {
	if len(data) == 0 || len(boxes) == 0 {
		return data
	}
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	bounds := src.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, src, bounds.Min, draw.Src)

	w := bounds.Dx()
	h := bounds.Dy()
	thickness := w / 320
	if thickness < 2 {
		thickness = 2
	}

	for _, b := range boxes {
		x0 := bounds.Min.X + int(clamp(b.Box.X)*float64(w))
		y0 := bounds.Min.Y + int(clamp(b.Box.Y)*float64(h))
		x1 := bounds.Min.X + int(clamp(b.Box.X+b.Box.W)*float64(w))
		y1 := bounds.Min.Y + int(clamp(b.Box.Y+b.Box.H)*float64(h))
		drawRectOutline(rgba, x0, y0, x1, y1, thickness, boxOverlayColor)
		if label := strings.TrimSpace(b.Label); label != "" {
			drawLabelTag(rgba, x0, y0, label)
		}
	}

	if quality <= 0 {
		quality = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: quality}); err != nil {
		return data
	}
	return buf.Bytes()
}

// drawRectOutline draws a rectangle border of the given thickness in c.
func drawRectOutline(img *image.RGBA, x0, y0, x1, y1, thickness int, c color.RGBA) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	top := image.Rect(x0, y0, x1, y0+thickness)
	bottom := image.Rect(x0, y1-thickness, x1, y1)
	left := image.Rect(x0, y0, x0+thickness, y1)
	right := image.Rect(x1-thickness, y0, x1, y1)
	uniform := image.NewUniform(c)
	for _, r := range []image.Rectangle{top, bottom, left, right} {
		draw.Draw(img, r.Intersect(img.Bounds()), uniform, image.Point{}, draw.Src)
	}
}

// drawLabelTag draws a filled tag with the label text just above the box's
// top-left corner (or inside the box when there is no room above), matching the
// UI's label chip.
func drawLabelTag(img *image.RGBA, x, y int, label string) {
	face := basicfont.Face7x13
	const padX, padY = 4, 3
	textW := font.MeasureString(face, label).Ceil()
	tagH := face.Metrics().Height.Ceil() + padY*2
	tagW := textW + padX*2

	tagY := y - tagH
	if tagY < img.Bounds().Min.Y {
		tagY = y // no room above the box; place the tag inside the top edge
	}
	tag := image.Rect(x, tagY, x+tagW, tagY+tagH).Intersect(img.Bounds())
	draw.Draw(img, tag, image.NewUniform(boxOverlayColor), image.Point{}, draw.Src)

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{A: 0xff}), // black text on the cyan tag
		Face: face,
		Dot:  fixed.P(x+padX, tagY+padY+face.Metrics().Ascent.Ceil()),
	}
	drawer.DrawString(label)
}
