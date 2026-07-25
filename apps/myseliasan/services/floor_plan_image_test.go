package services

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

// planPNG encodes a small image standing in for an operator's uploaded plan.
func planPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode plan: %v", err)
	}
	return buf.Bytes()
}

// Uploading a picture onto an area marks it as carrying a plan — this is what makes the editor
// offer "Remove plan" at all.
func TestReplaceFloorImageMarksPlanUploaded(t *testing.T) {
	svc, repo, _, _ := seedFloorWithPlan(t)
	repo.row.HasPlanImage = false // start from a blank area

	if _, err := svc.ReplaceFloorImage(context.Background(), 7, "Ground floor", planPNG(t, 640, 480), "image/png", "", 42); err != nil {
		t.Fatalf("ReplaceFloorImage: %v", err)
	}
	if !repo.row.HasPlanImage {
		t.Fatal("an uploaded plan must set HasPlanImage")
	}
}

// Re-saving from the designer only re-rasterises what is already there. Drawing walls on a blank
// area does not turn it into an uploaded plan, so the button must stay hidden.
func TestReplaceFloorImageWithDesignDoesNotClaimAnUpload(t *testing.T) {
	svc, repo, _, _ := seedFloorWithPlan(t)
	repo.row.HasPlanImage = false

	if _, err := svc.ReplaceFloorImage(context.Background(), 7, "Ground floor", planPNG(t, 640, 480), "image/png", `{"walls":[[1,2]]}`, 42); err != nil {
		t.Fatalf("ReplaceFloorImage: %v", err)
	}
	if repo.row.HasPlanImage {
		t.Fatal("a designer re-save must not mark a blank area as having an uploaded plan")
	}
}

// A designer re-save must equally not LOSE the flag on a floor that really does have a plan.
func TestReplaceFloorImageWithDesignKeepsAnExistingUpload(t *testing.T) {
	svc, repo, _, _ := seedFloorWithPlan(t)
	repo.row.HasPlanImage = true

	if _, err := svc.ReplaceFloorImage(context.Background(), 7, "Ground floor", planPNG(t, 640, 480), "image/png", `{"walls":[[1,2]]}`, 42); err != nil {
		t.Fatalf("ReplaceFloorImage: %v", err)
	}
	if !repo.row.HasPlanImage {
		t.Fatal("re-saving a drawn plan must not drop HasPlanImage")
	}
}

// Removing the plan returns the floor to the blank canvas, which is exactly the state in which the
// button should disappear.
func TestClearFloorImageClearsHasPlanImage(t *testing.T) {
	svc, repo, _, _ := seedFloorWithPlan(t)
	repo.row.HasPlanImage = true

	got, err := svc.ClearFloorImage(context.Background(), 7, 42)
	if err != nil {
		t.Fatalf("ClearFloorImage: %v", err)
	}
	if got.HasPlanImage || repo.row.HasPlanImage {
		t.Fatal("clearing the plan must clear HasPlanImage")
	}
}

// The wizard's areas start blank, so they must not advertise a plan to remove.
func TestAddBlankFloorHasNoPlanImage(t *testing.T) {
	dir := t.TempDir()
	floors := &fakeFloorRepo{}
	svc := &siteService{sites: &fakeSiteRepo{row: &entities.Site{Id: 3, Name: "Head Office"}}, floors: floors, dir: dir}

	row, err := svc.AddBlankFloor(context.Background(), 3, "Ground floor", 0, 0, 0, 42)
	if err != nil {
		t.Fatalf("AddBlankFloor: %v", err)
	}
	if row.HasPlanImage {
		t.Fatal("a generated blank canvas is not an uploaded plan")
	}
}

// An area created straight from an uploaded file does carry a plan.
func TestAddFloorFromUploadHasPlanImage(t *testing.T) {
	dir := t.TempDir()
	floors := &fakeFloorRepo{}
	svc := &siteService{sites: &fakeSiteRepo{row: &entities.Site{Id: 3, Name: "Head Office"}}, floors: floors, dir: dir}

	row, err := svc.AddFloor(context.Background(), 3, "Ground floor", planPNG(t, 800, 600), "image/png", "", 42)
	if err != nil {
		t.Fatalf("AddFloor: %v", err)
	}
	if !row.HasPlanImage {
		t.Fatal("an uploaded plan must set HasPlanImage")
	}
}
