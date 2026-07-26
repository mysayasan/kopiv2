package services

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

func TestRenderFloorPlacements(t *testing.T) {
	// A plain white 400x300 plan.
	base := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := range base.Pix {
		base.Pix[i] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, base); err != nil {
		t.Fatal(err)
	}

	// A grid with a vertical wall broken by a door and a window, plus a stair footprint —
	// the authored geometry the editor stores in FloorPlan.Grid (never baked into the image).
	grid := `{"version":2,"unit":24,
		"segments":[{"x1":200,"y1":40,"x2":200,"y2":260}],
		"doors":[{"cx":200,"cy":150,"w":36,"a":1.5708}],
		"windows":[{"cx":200,"cy":80,"w":40,"a":1.5708}],
		"stairs":[{"x1":300,"y1":40,"x2":360,"y2":140,"dir":"n","steps":8}]}`

	// Placements use y-UP (bottom-left) space; they get flipped to (h - Y) internally.
	placements := []*entities.NodePlacement{
		{X: 120, Y: 210, Heading: 45, Fov: 90, LastKnownName: "Lobby Cam"}, // wedge+label at y=300-210=90
		{X: 300, Y: 100, LastKnownName: "Sensor"},                          // marker at y=200
		nil,                                                                // must be skipped
	}
	out, err := renderFloorPlacements(buf.Bytes(), grid, placements)
	if err != nil {
		t.Fatalf("renderFloorPlacements() error = %v", err)
	}
	rgba, ok := out.(*image.RGBA)
	if !ok {
		t.Fatalf("output is not *image.RGBA")
	}
	if b := out.Bounds(); b.Dx() != 400 || b.Dy() != 300 {
		t.Fatalf("output size = %dx%d, want 400x300", b.Dx(), b.Dy())
	}
	if dir := os.Getenv("REPORT_DUMP_DIR"); dir != "" {
		f, _ := os.Create(filepath.Join(dir, "composited-floor.png"))
		_ = png.Encode(f, out)
		_ = f.Close()
	}

	// The wall must be drawn: a point on the segment away from the door/window is dark.
	if c := rgba.RGBAAt(200, 210); (c == color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("wall pixel (200,210) is white — grid walls were not rendered")
	}
	// The door gap at y≈150 must NOT be a solid wall (the wall is carved there).
	if c := rgba.RGBAAt(200, 150); (c == gridWall) {
		t.Fatalf("door gap pixel (200,150) is solid wall — wall not carved by door")
	}
	// The door symbol (leaf + swing arc, amber #b45309) must be drawn near the opening.
	door := 0
	for y := 120; y < 180; y++ {
		for x := 150; x <= 200; x++ {
			c := rgba.RGBAAt(x, y)
			if c.R > 140 && c.G > 55 && c.G < 120 && c.B < 60 {
				door++
			}
		}
	}
	if door == 0 {
		t.Fatal("door symbol (amber leaf/arc) was not rendered near the opening")
	}
	// The camera marker/wedge (placement 1 flips to y≈90) must paint pixels.
	painted := 0
	for y := 80; y < 100; y++ {
		for x := 110; x < 130; x++ {
			if rgba.RGBAAt(x, y) != (color.RGBA{255, 255, 255, 255}) {
				painted++
			}
		}
	}
	if painted == 0 {
		t.Fatal("expected the camera marker/wedge to paint pixels near its flipped position")
	}
	// A pixel clear of all geometry stays white — the compositor never tints the whole plan.
	if c := rgba.RGBAAt(20, 20); (c != color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("background pixel (20,20) = %v, want opaque white", c)
	}
}
