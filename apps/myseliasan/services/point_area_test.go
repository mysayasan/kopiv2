package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakePointSiteRepo is an in-memory site table. Sites are read by id (EnsurePointArea) and listed whole
// (EnsurePointAreas), which is all the point-asset paths need.
type fakePointSiteRepo struct {
	dbsql.IGenericRepo[entities.Site]
	rows   []*entities.Site
	nextID int64
}

func (f *fakePointSiteRepo) GetById(_ context.Context, _ string, id uint64) (*entities.Site, error) {
	for _, r := range f.rows {
		if uint64(r.Id) == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakePointSiteRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.Site, uint64, error) {
	out := make([]*entities.Site, 0, len(f.rows))
	for _, r := range f.rows {
		cp := *r
		out = append(out, &cp)
	}
	return out, uint64(len(out)), nil
}

func (f *fakePointSiteRepo) Create(_ context.Context, _ string, m entities.Site) (uint64, error) {
	f.nextID++
	cp := m
	cp.Id = f.nextID
	f.rows = append(f.rows, &cp)
	return uint64(cp.Id), nil
}

// fakePointFloorRepo is an in-memory floor table honouring the SiteId equality filter ListFloors uses,
// so "does this site already have an area?" is answered through the real query path.
type fakePointFloorRepo struct {
	dbsql.IGenericRepo[entities.FloorPlan]
	rows   []*entities.FloorPlan
	nextID int64
}

func (f *fakePointFloorRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.FloorPlan, uint64, error) {
	out := []*entities.FloorPlan{}
	for _, r := range f.rows {
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

func (f *fakePointFloorRepo) GetById(_ context.Context, _ string, id uint64) (*entities.FloorPlan, error) {
	for _, r := range f.rows {
		if uint64(r.Id) == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakePointFloorRepo) Create(_ context.Context, _ string, m entities.FloorPlan) (uint64, error) {
	f.nextID++
	cp := m
	cp.Id = f.nextID
	f.rows = append(f.rows, &cp)
	return uint64(cp.Id), nil
}

func (f *fakePointFloorRepo) UpdateById(_ context.Context, _ string, m entities.FloorPlan) (uint64, error) {
	for i, r := range f.rows {
		if r.Id == m.Id {
			cp := m
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

// pointAreaSvc builds a site service over the in-memory repos, writing plan images into a temp dir
// with encryption disabled (cipher nil), so the blank canvas is a plain PNG on disk.
func pointAreaSvc(t *testing.T, sites ...*entities.Site) (*siteService, *fakePointSiteRepo, *fakePointFloorRepo) {
	t.Helper()
	siteRepo := &fakePointSiteRepo{}
	for _, s := range sites {
		cp := *s
		siteRepo.nextID++
		cp.Id = siteRepo.nextID
		siteRepo.rows = append(siteRepo.rows, &cp)
	}
	floorRepo := &fakePointFloorRepo{}
	return &siteService{sites: siteRepo, floors: floorRepo, dir: t.TempDir()}, siteRepo, floorRepo
}

func TestEnsurePointAreaGivesAPointAssetSomethingToPinCamerasTo(t *testing.T) {
	svc, _, floors := pointAreaSvc(t, &entities.Site{Name: "Jalan Ampang junction", Kind: entities.SiteKindPoint})

	area, err := svc.EnsurePointArea(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("EnsurePointArea() error = %v", err)
	}
	if area == nil {
		t.Fatal("EnsurePointArea() returned no area — a point asset with no area can hold no camera, which is the whole bug")
	}
	if area.SiteId != 1 {
		t.Fatalf("area.SiteId = %d, want 1", area.SiteId)
	}
	// The canvas has to be a real pixel space: placement X/Y live in it, so a zero-sized area
	// would put every camera at the same spot.
	if area.Width != pointAreaWidth || area.Height != pointAreaHeight {
		t.Fatalf("area = %dx%d, want %dx%d", area.Width, area.Height, pointAreaWidth, pointAreaHeight)
	}
	// It is a blank canvas, not an operator's uploaded plan — the editor keys "is there a plan to
	// remove?" off this flag.
	if area.HasPlanImage {
		t.Fatal("implicit point area reports HasPlanImage — it is a generated canvas, not an upload")
	}
	if len(floors.rows) != 1 {
		t.Fatalf("floor rows = %d, want 1", len(floors.rows))
	}
}

func TestEnsurePointAreaIsIdempotent(t *testing.T) {
	svc, _, floors := pointAreaSvc(t, &entities.Site{Name: "Main gate", Kind: entities.SiteKindPoint})

	first, err := svc.EnsurePointArea(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("first EnsurePointArea() error = %v", err)
	}
	second, err := svc.EnsurePointArea(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("second EnsurePointArea() error = %v", err)
	}
	// A second area would split the point asset's cameras across two canvases for no reason, and
	// the boot backfill runs on every start — so this has to be a no-op, not a second insert.
	if len(floors.rows) != 1 {
		t.Fatalf("floor rows after two calls = %d, want 1", len(floors.rows))
	}
	if first.Id != second.Id {
		t.Fatalf("second call returned area %d, want the existing %d", second.Id, first.Id)
	}
}

func TestEnsurePointAreaLeavesBuildingsAndOutdoorAreasAlone(t *testing.T) {
	svc, _, floors := pointAreaSvc(t,
		&entities.Site{Name: "Head Office", Kind: entities.SiteKindBuilding},
		&entities.Site{Name: "North Yard", Kind: entities.SiteKindOutdoor},
		// The empty kind every site that predates the field carries — a building.
		&entities.Site{Name: "Legacy site", Kind: ""},
	)

	for id := int64(1); id <= 3; id++ {
		area, err := svc.EnsurePointArea(context.Background(), id, 42)
		if err != nil {
			t.Fatalf("EnsurePointArea(%d) error = %v", id, err)
		}
		if area != nil {
			t.Fatalf("EnsurePointArea(%d) created an area for a non-point site — its areas are the operator's to make", id)
		}
	}
	if len(floors.rows) != 0 {
		t.Fatalf("floor rows = %d, want 0", len(floors.rows))
	}
}

func TestEnsurePointAreasBackfillsOnlyPointAssetsMissingOne(t *testing.T) {
	svc, _, floors := pointAreaSvc(t,
		&entities.Site{Name: "Head Office", Kind: entities.SiteKindBuilding},
		&entities.Site{Name: "Jalan Ampang junction", Kind: entities.SiteKindPoint},
		&entities.Site{Name: "Main gate", Kind: entities.SiteKindPoint},
	)
	// Site 3 already has its area (it was created after the implicit area existed). nextID is
	// advanced past the seeded id so the next Create cannot mint a duplicate — two rows sharing an
	// id would make the fake's UpdateById rewrite the wrong one.
	floors.nextID = 91
	floors.rows = append(floors.rows, &entities.FloorPlan{Id: 91, SiteId: 3, Name: pointAreaName})

	repaired, err := svc.EnsurePointAreas(context.Background())
	if err != nil {
		t.Fatalf("EnsurePointAreas() error = %v", err)
	}
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1 (only the junction was missing an area)", repaired)
	}

	// Running again on every boot must change nothing.
	again, err := svc.EnsurePointAreas(context.Background())
	if err != nil {
		t.Fatalf("second EnsurePointAreas() error = %v", err)
	}
	if again != 0 {
		t.Fatalf("second run repaired = %d, want 0 — the backfill is not idempotent", again)
	}
	if len(floors.rows) != 2 {
		t.Fatalf("floor rows = %d, want 2 (one per point asset, none for the building)", len(floors.rows))
	}
}

func TestCreateSiteGivesANewPointAssetItsAreaImmediately(t *testing.T) {
	svc, _, floors := pointAreaSvc(t)

	site, err := svc.CreateSite(context.Background(), "Jalan Ampang junction", "", "🚦", entities.SiteKindPoint, 42)
	if err != nil {
		t.Fatalf("CreateSite() error = %v", err)
	}
	// Without this, a freshly created junction is a site nothing can be pinned to, and the map
	// falls back to claiming every camera on whichever appliance is assigned to it.
	if len(floors.rows) != 1 || floors.rows[0].SiteId != site.Id {
		t.Fatalf("new point asset has %d area(s), want 1 on site %d", len(floors.rows), site.Id)
	}
}

func TestCreateSiteDoesNotInventAreasForBuildings(t *testing.T) {
	svc, _, floors := pointAreaSvc(t)

	if _, err := svc.CreateSite(context.Background(), "Head Office", "", "🏢", entities.SiteKindBuilding, 42); err != nil {
		t.Fatalf("CreateSite() error = %v", err)
	}
	// A building's areas come from the wizard ("Ground floor", "1st floor"), named by the operator.
	if len(floors.rows) != 0 {
		t.Fatalf("new building has %d area(s), want 0", len(floors.rows))
	}
}
