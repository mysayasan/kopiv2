package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakePlacementRepo is an in-memory placement table that honours the (NodeId, CameraId) equality
// filters FindPlacementOf uses, so the exclusivity rule is exercised through the same query path
// the real repo takes rather than a shortcut.
type fakePlacementRepo struct {
	dbsql.IGenericRepo[entities.NodePlacement]
	rows   []*entities.NodePlacement
	nextID int64
}

func (f *fakePlacementRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.NodePlacement, uint64, error) {
	out := []*entities.NodePlacement{}
	for _, r := range f.rows {
		keep := true
		for _, flt := range filters {
			if flt.Compare != sqldataenums.Equal {
				continue
			}
			switch flt.FieldName {
			case "NodeId":
				keep = keep && r.NodeId == flt.Value
			case "CameraId":
				keep = keep && r.CameraId == flt.Value
			case "FloorId":
				keep = keep && r.FloorId == flt.Value
			}
		}
		if keep {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}

func (f *fakePlacementRepo) Create(_ context.Context, _ string, m entities.NodePlacement) (uint64, error) {
	f.nextID++
	cp := m
	cp.Id = f.nextID
	f.rows = append(f.rows, &cp)
	return uint64(cp.Id), nil
}

func (f *fakePlacementRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if uint64(r.Id) == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// multiFloorRepo holds several floors, so a placement can be checked against a floor that exists
// and against one that has been deleted.
type multiFloorRepo struct {
	dbsql.IGenericRepo[entities.FloorPlan]
	rows map[int64]*entities.FloorPlan
}

func (m *multiFloorRepo) GetById(_ context.Context, _ string, id uint64) (*entities.FloorPlan, error) {
	r, ok := m.rows[int64(id)]
	if !ok {
		return nil, errors.New("no result found")
	}
	cp := *r
	return &cp, nil
}

// Get answers ListFloors' SiteId filter, so PlacementIndex can resolve a pin to its area.
func (m *multiFloorRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.FloorPlan, uint64, error) {
	out := []*entities.FloorPlan{}
	for _, r := range m.rows {
		keep := true
		for _, flt := range filters {
			if flt.Compare == sqldataenums.Equal && flt.FieldName == "SiteId" {
				keep = keep && r.SiteId == flt.Value
			}
		}
		if keep {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}

func exclusiveSvc() (*siteService, *fakePlacementRepo) {
	placements := &fakePlacementRepo{}
	floors := &multiFloorRepo{rows: map[int64]*entities.FloorPlan{
		10: {Id: 10, SiteId: 1, Name: "Ground floor"},
		11: {Id: 11, SiteId: 1, Name: "Level 2"},
		20: {Id: 20, SiteId: 2, Name: "Grounds"},
	}}
	sites := &fakeSiteRepo{row: &entities.Site{Id: 1, Name: "Head Office", Kind: entities.SiteKindBuilding}}
	return &siteService{sites: sites, floors: floors, placements: placements}, placements
}

// The rule: once a camera is placed, it cannot be placed anywhere else.
func TestAddPlacementRefusesACameraThatIsAlreadyPlaced(t *testing.T) {
	svc, repo := exclusiveSvc()
	ctx := context.Background()

	if _, err := svc.AddPlacement(ctx, 10, "node-a", "3", "Lobby cam", 100, 100, 7); err != nil {
		t.Fatalf("first placement: %v", err)
	}

	// A different area of the same building.
	_, err := svc.AddPlacement(ctx, 11, "node-a", "3", "Lobby cam", 200, 200, 7)
	var taken *ErrAlreadyPlaced
	if !errors.As(err, &taken) {
		t.Fatalf("second placement error = %v, want *ErrAlreadyPlaced", err)
	}
	if taken.FloorName() != "Ground floor" {
		t.Fatalf("refusal names floor %q, want \"Ground floor\"", taken.FloorName())
	}
	if taken.SiteName() != "Head Office" {
		t.Fatalf("refusal names site %q, want \"Head Office\"", taken.SiteName())
	}

	// And a different building entirely — the rule is fleet-wide, not per building.
	if _, err := svc.AddPlacement(ctx, 20, "node-a", "3", "Lobby cam", 50, 50, 7); !errors.As(err, &taken) {
		t.Fatalf("cross-building placement error = %v, want *ErrAlreadyPlaced", err)
	}

	if len(repo.rows) != 1 {
		t.Fatalf("stored %d placements, want exactly 1", len(repo.rows))
	}
}

// Unplacing is what frees it again — that is the whole escape hatch.
func TestAddPlacementAllowedAfterUnplacing(t *testing.T) {
	svc, repo := exclusiveSvc()
	ctx := context.Background()

	first, err := svc.AddPlacement(ctx, 10, "node-a", "3", "Lobby cam", 100, 100, 7)
	if err != nil {
		t.Fatalf("first placement: %v", err)
	}
	if err := svc.DeletePlacement(ctx, first.Id); err != nil {
		t.Fatalf("DeletePlacement: %v", err)
	}
	if _, err := svc.AddPlacement(ctx, 20, "node-a", "3", "Lobby cam", 50, 50, 7); err != nil {
		t.Fatalf("re-placement after unplacing: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("stored %d placements, want exactly 1", len(repo.rows))
	}
	if repo.rows[0].FloorId != 20 {
		t.Fatalf("re-placed on floor %d, want 20", repo.rows[0].FloorId)
	}
}

// The rule is per camera, not per node: a node's other cameras are unaffected, and the node's own
// marker is a separate key again.
func TestAddPlacementIsPerCameraNotPerNode(t *testing.T) {
	svc, repo := exclusiveSvc()
	ctx := context.Background()

	if _, err := svc.AddPlacement(ctx, 10, "node-a", "3", "Lobby cam", 100, 100, 7); err != nil {
		t.Fatalf("camera 3: %v", err)
	}
	if _, err := svc.AddPlacement(ctx, 11, "node-a", "4", "Dock cam", 100, 100, 7); err != nil {
		t.Fatalf("camera 4 on the same node must be placeable: %v", err)
	}
	if _, err := svc.AddPlacement(ctx, 20, "node-a", "", "node-a", 100, 100, 7); err != nil {
		t.Fatalf("the node's own marker is a separate key: %v", err)
	}
	if len(repo.rows) != 3 {
		t.Fatalf("stored %d placements, want 3", len(repo.rows))
	}
}

// A node marker, like a camera, is single-placement.
func TestAddPlacementRefusesADuplicateNodeMarker(t *testing.T) {
	svc, _ := exclusiveSvc()
	ctx := context.Background()

	if _, err := svc.AddPlacement(ctx, 10, "node-a", "", "node-a", 100, 100, 7); err != nil {
		t.Fatalf("first node marker: %v", err)
	}
	var taken *ErrAlreadyPlaced
	if _, err := svc.AddPlacement(ctx, 11, "node-a", "", "node-a", 100, 100, 7); !errors.As(err, &taken) {
		t.Fatalf("second node marker error = %v, want *ErrAlreadyPlaced", err)
	}
}

// A pin on a floor that no longer exists renders nowhere and cannot be unplaced by hand. If it
// counted as a placement it would make its camera permanently unplaceable — so it is treated as
// unplaced and cleaned up.
func TestAddPlacementIgnoresAndCleansUpAPinOnADeletedFloor(t *testing.T) {
	svc, repo := exclusiveSvc()
	ctx := context.Background()

	// A leftover pointing at a floor id the floor repo does not know.
	repo.rows = append(repo.rows, &entities.NodePlacement{Id: 99, FloorId: 777, NodeId: "node-a", CameraId: "3"})

	if _, err := svc.AddPlacement(ctx, 10, "node-a", "3", "Lobby cam", 100, 100, 7); err != nil {
		t.Fatalf("placement blocked by an orphaned pin: %v", err)
	}
	for _, r := range repo.rows {
		if r.Id == 99 {
			t.Fatal("the orphaned pin should have been cleaned up")
		}
	}
	if len(repo.rows) != 1 {
		t.Fatalf("stored %d placements, want exactly 1", len(repo.rows))
	}
}

// The palette reads this to grey out cameras that are placed elsewhere, so it must name the area
// and the building — and must not report orphaned pins as placements.
func TestPlacementIndexNamesWhereEachPinSits(t *testing.T) {
	svc, repo := exclusiveSvc()
	ctx := context.Background()
	if _, err := svc.AddPlacement(ctx, 10, "node-a", "3", "Lobby cam", 100, 100, 7); err != nil {
		t.Fatalf("placement: %v", err)
	}
	repo.rows = append(repo.rows, &entities.NodePlacement{Id: 99, FloorId: 777, NodeId: "node-b", CameraId: "9"})

	// ListSites goes through the shared site repo, which holds the one building above.
	index, err := svc.PlacementIndex(ctx)
	if err != nil {
		t.Fatalf("PlacementIndex: %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("index has %d entries, want 1 (the orphan must be skipped)", len(index))
	}
	got := index[0]
	if got.NodeId != "node-a" || got.CameraId != "3" {
		t.Fatalf("index entry = %+v", got)
	}
	if got.FloorName != "Ground floor" || got.SiteName != "Head Office" {
		t.Fatalf("index entry does not name where it sits: %+v", got)
	}
}
