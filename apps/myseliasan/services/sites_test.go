package services

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeFloorRepo is an in-memory stand-in holding a single floor row, enough for the
// get-modify-update shape ClearFloorImage uses.
type fakeFloorRepo struct {
	dbsql.IGenericRepo[entities.FloorPlan]
	row       *entities.FloorPlan
	updateErr error
}

func (f *fakeFloorRepo) GetById(_ context.Context, _ string, id uint64) (*entities.FloorPlan, error) {
	if f.row == nil || uint64(f.row.Id) != id {
		return nil, os.ErrNotExist
	}
	cp := *f.row
	return &cp, nil
}

func (f *fakeFloorRepo) UpdateById(_ context.Context, _ string, m entities.FloorPlan) (uint64, error) {
	if f.updateErr != nil {
		return 0, f.updateErr
	}
	cp := m
	f.row = &cp
	return 1, nil
}

// seedFloorWithPlan writes an uploaded-looking floor to disk: a non-white plan image plus the
// pristine background kept for re-editing, mirroring what an upload leaves behind.
func seedFloorWithPlan(t *testing.T) (*siteService, *fakeFloorRepo, string, string) {
	t.Helper()
	dir := t.TempDir()

	// A distinctly non-white image, so "was it actually replaced by a blank canvas?" is decidable.
	src := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for i := range src.Pix {
		src.Pix[i] = 0x20
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode seed plan: %v", err)
	}

	imgPath := filepath.Join(dir, "floor-7.img")
	bgPath := filepath.Join(dir, "floor-7.bg.img")
	if err := os.WriteFile(imgPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if err := os.WriteFile(bgPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write background: %v", err)
	}

	repo := &fakeFloorRepo{row: &entities.FloorPlan{
		Id: 7, SiteId: 3, Name: "Ground floor", Ordinal: 1,
		ImagePath: imgPath, BgPath: bgPath, ContentType: "image/jpeg",
		Width: 640, Height: 480,
		Design: `{"walls":[[1,2]]}`,
		// The 3D authoring that must survive removing the picture.
		Grid: `{"cellPx":32,"cols":20,"rows":15,"walls":[[0,0]]}`, Scale: 0.05,
		WallHeight: 2.8, Elevation: 3.2,
	}}
	// cipher nil: at-rest encryption disabled, so bytes on disk are the plain image and the test
	// can decode them directly.
	return &siteService{floors: repo, dir: dir}, repo, imgPath, bgPath
}

func TestClearFloorImageRestoresBlankCanvasAtSameDimensions(t *testing.T) {
	svc, _, imgPath, _ := seedFloorWithPlan(t)

	got, err := svc.ClearFloorImage(context.Background(), 7, 42)
	if err != nil {
		t.Fatalf("ClearFloorImage() error = %v", err)
	}

	raw, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("read cleared plan: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("cleared plan is not a valid PNG: %v", err)
	}
	b := decoded.Bounds()
	// Dimensions must be preserved: placement X/Y and the 3D grid live in this same pixel space,
	// so a resized canvas would silently shift every camera marker.
	if b.Dx() != 640 || b.Dy() != 480 {
		t.Fatalf("cleared canvas = %dx%d, want 640x480", b.Dx(), b.Dy())
	}
	if got.Width != 640 || got.Height != 480 {
		t.Fatalf("row dimensions = %dx%d, want 640x480", got.Width, got.Height)
	}
	// Every pixel white — the old plan is genuinely gone, not merely dereferenced.
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := decoded.At(x, y).RGBA()
			if r != 0xffff || g != 0xffff || bl != 0xffff || a != 0xffff {
				t.Fatalf("pixel (%d,%d) = %v,%v,%v,%v, want opaque white", x, y, r, g, bl, a)
			}
		}
	}
	if got.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", got.ContentType)
	}
}

func TestClearFloorImageKeepsAuthoringAndDropsBackground(t *testing.T) {
	svc, repo, _, bgPath := seedFloorWithPlan(t)

	got, err := svc.ClearFloorImage(context.Background(), 7, 42)
	if err != nil {
		t.Fatalf("ClearFloorImage() error = %v", err)
	}

	// The drawn design and the stored background belong to the plan being removed.
	if got.Design != "" {
		t.Errorf("Design = %q, want cleared", got.Design)
	}
	if got.BgPath != "" {
		t.Errorf("BgPath = %q, want cleared", got.BgPath)
	}
	if _, err := os.Stat(bgPath); !os.IsNotExist(err) {
		t.Errorf("background file still on disk (stat err = %v); it would resurrect the removed plan as the designer canvas", err)
	}

	// The 3D model is authored independently of the 2D picture and must survive.
	if got.Grid != `{"cellPx":32,"cols":20,"rows":15,"walls":[[0,0]]}` {
		t.Errorf("Grid = %q, want preserved", got.Grid)
	}
	if got.Scale != 0.05 || got.WallHeight != 2.8 || got.Elevation != 3.2 {
		t.Errorf("scale/wallHeight/elevation = %v/%v/%v, want 0.05/2.8/3.2", got.Scale, got.WallHeight, got.Elevation)
	}
	// Identity is untouched — this clears the picture, not the area.
	if got.Name != "Ground floor" || got.SiteId != 3 || got.Ordinal != 1 {
		t.Errorf("identity changed: %+v", got)
	}
	if got.UpdatedBy != 42 {
		t.Errorf("UpdatedBy = %d, want 42", got.UpdatedBy)
	}
	if repo.row.Design != "" || repo.row.BgPath != "" {
		t.Errorf("persisted row not cleared: %+v", repo.row)
	}
}

func TestClearFloorImageUnknownFloor(t *testing.T) {
	svc, _, _, _ := seedFloorWithPlan(t)
	if _, err := svc.ClearFloorImage(context.Background(), 999, 42); err != ErrFloorUnknown {
		t.Fatalf("ClearFloorImage(unknown) error = %v, want ErrFloorUnknown", err)
	}
}

// A failed row update must not have already deleted the background, or the floor would be left
// pointing at a file that is gone.
func TestClearFloorImageKeepsBackgroundWhenUpdateFails(t *testing.T) {
	svc, repo, _, bgPath := seedFloorWithPlan(t)
	repo.updateErr = os.ErrPermission

	if _, err := svc.ClearFloorImage(context.Background(), 7, 42); err == nil {
		t.Fatal("ClearFloorImage() error = nil, want the update failure")
	}
	if _, err := os.Stat(bgPath); err != nil {
		t.Fatalf("background removed despite failed update: %v", err)
	}
}
