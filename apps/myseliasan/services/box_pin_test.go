package services

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

// bindCall records what the placement service asked the node registry to write, so these tests are
// about the RULE (which pins bind, which do not) rather than about the registry.
type bindCall struct {
	nodeID string
	siteID int64
}

func boxPinSvc(t *testing.T) (*siteService, *fakePointFloorRepo, *[]bindCall) {
	t.Helper()
	siteRepo := &fakePointSiteRepo{}
	siteRepo.nextID = 1
	siteRepo.rows = append(siteRepo.rows, &entities.Site{Id: 1, Name: "Head Office", Kind: entities.SiteKindBuilding})
	siteRepo.nextID = 2
	siteRepo.rows = append(siteRepo.rows, &entities.Site{Id: 2, Name: "North Yard", Kind: entities.SiteKindOutdoor})
	floors := &fakePointFloorRepo{nextID: 10}
	floors.rows = append(floors.rows,
		&entities.FloorPlan{Id: 11, SiteId: 1, Name: "Ground floor"},
		&entities.FloorPlan{Id: 12, SiteId: 2, Name: "Grounds"},
	)
	calls := &[]bindCall{}
	svc := &siteService{
		sites: siteRepo, floors: floors, placements: &fakePlacementRepo{}, dir: t.TempDir(),
	}
	svc.SetNodeSiteBinder(func(_ context.Context, nodeID string, siteID, _ int64) error {
		*calls = append(*calls, bindCall{nodeID, siteID})
		return nil
	})
	return svc, floors, calls
}

func TestPinningTheApplianceRecordsWhereItsBoxLives(t *testing.T) {
	svc, _, calls := boxPinSvc(t)

	// Empty cameraId = the appliance's own marker.
	if _, err := svc.AddPlacement(context.Background(), 11, "nvr-hq-01", "", "nvr-hq-01", 10, 10, 7); err != nil {
		t.Fatalf("AddPlacement() error = %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("bind calls = %d, want 1 — the pin IS the record of where the box is", len(*calls))
	}
	if (*calls)[0] != (bindCall{"nvr-hq-01", 1}) {
		t.Fatalf("bound %+v, want nvr-hq-01 -> site 1", (*calls)[0])
	}
}

func TestPinningACameraNeverTouchesWhereTheBoxLives(t *testing.T) {
	svc, _, calls := boxPinSvc(t)

	// The whole reason the tree exists: one recorder's cameras land in two different places.
	// Neither of them says anything about where the recorder itself sits.
	if _, err := svc.AddPlacement(context.Background(), 11, "nvr-hq-01", "1", "Lobby north", 10, 10, 7); err != nil {
		t.Fatalf("AddPlacement(camera 1) error = %v", err)
	}
	if _, err := svc.AddPlacement(context.Background(), 12, "nvr-hq-01", "2", "Yard gate", 20, 20, 7); err != nil {
		t.Fatalf("AddPlacement(camera 2) error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("camera pins bound the node %d time(s), want 0 — a recorder feeding two places has no single site", len(*calls))
	}
}

func TestUnpinningTheApplianceClearsWhereItsBoxLives(t *testing.T) {
	svc, _, calls := boxPinSvc(t)

	pin, err := svc.AddPlacement(context.Background(), 11, "nvr-hq-01", "", "nvr-hq-01", 10, 10, 7)
	if err != nil {
		t.Fatalf("AddPlacement() error = %v", err)
	}
	if err := svc.DeletePlacement(context.Background(), pin.Id); err != nil {
		t.Fatalf("DeletePlacement() error = %v", err)
	}
	// Without this the node stays recorded as living somewhere it no longer has a pin, which is
	// exactly the drift that having a single writer is supposed to remove.
	if len(*calls) != 2 {
		t.Fatalf("bind calls = %d, want 2 (pin then unpin)", len(*calls))
	}
	if (*calls)[1] != (bindCall{"nvr-hq-01", 0}) {
		t.Fatalf("unpin bound %+v, want nvr-hq-01 -> site 0", (*calls)[1])
	}
}

func TestUnpinningACameraLeavesTheBoxAlone(t *testing.T) {
	svc, _, calls := boxPinSvc(t)

	pin, err := svc.AddPlacement(context.Background(), 11, "nvr-hq-01", "4", "Car park east", 10, 10, 7)
	if err != nil {
		t.Fatalf("AddPlacement() error = %v", err)
	}
	if err := svc.DeletePlacement(context.Background(), pin.Id); err != nil {
		t.Fatalf("DeletePlacement() error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("removing a camera pin bound the node %d time(s), want 0", len(*calls))
	}
}

func TestPlacementStillWorksWithNoBinderWired(t *testing.T) {
	svc, _, _ := boxPinSvc(t)
	svc.SetNodeSiteBinder(nil)

	// The pin is the thing the map draws. A missing binder (tests, or a partially wired app) must
	// not cost the operator the placement itself.
	if _, err := svc.AddPlacement(context.Background(), 11, "iot-roof-01", "", "iot-roof-01", 5, 5, 7); err != nil {
		t.Fatalf("AddPlacement() with no binder error = %v", err)
	}
}
