package services

import (
	"context"
	"os"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeSiteRepo captures what CreateSite/UpdateSite actually persist, so the kind assertions are
// about the stored row rather than the value handed back to the caller.
type fakeSiteRepo struct {
	dbsql.IGenericRepo[entities.Site]
	row *entities.Site
}

func (f *fakeSiteRepo) Create(_ context.Context, _ string, m entities.Site) (uint64, error) {
	cp := m
	cp.Id = 1
	f.row = &cp
	return 1, nil
}

// Get backs ListSites, which PlacementIndex uses to name the building a pin sits in.
func (f *fakeSiteRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.Site, uint64, error) {
	if f.row == nil {
		return []*entities.Site{}, 0, nil
	}
	cp := *f.row
	return []*entities.Site{&cp}, 1, nil
}

func (f *fakeSiteRepo) GetById(_ context.Context, _ string, id uint64) (*entities.Site, error) {
	if f.row == nil || uint64(f.row.Id) != id {
		return nil, os.ErrNotExist
	}
	cp := *f.row
	return &cp, nil
}

func (f *fakeSiteRepo) UpdateById(_ context.Context, _ string, m entities.Site) (uint64, error) {
	cp := m
	f.row = &cp
	return 1, nil
}

func TestCreateSiteStoresKind(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"outdoor area", entities.SiteKindOutdoor, entities.SiteKindOutdoor},
		{"point asset", entities.SiteKindPoint, entities.SiteKindPoint},
		{"explicit building", entities.SiteKindBuilding, entities.SiteKindBuilding},
		// A client that predates the field, or one sending junk, must not be able to park an
		// unknown kind in the database — the frontend would have no marker shape for it.
		{"absent defaults to building", "", entities.SiteKindBuilding},
		{"unknown defaults to building", "spaceship", entities.SiteKindBuilding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSiteRepo{}
			svc := &siteService{sites: repo}
			if _, err := svc.CreateSite(context.Background(), "Somewhere", "", "🚦", tc.in, 7); err != nil {
				t.Fatalf("CreateSite: %v", err)
			}
			if repo.row == nil {
				t.Fatal("nothing persisted")
			}
			if repo.row.Kind != tc.want {
				t.Fatalf("stored kind = %q, want %q", repo.row.Kind, tc.want)
			}
		})
	}
}

func TestUpdateSiteKeepsKindWhenEchoedBack(t *testing.T) {
	repo := &fakeSiteRepo{}
	svc := &siteService{sites: repo}
	created, err := svc.CreateSite(context.Background(), "Central Park", "", "🌳", entities.SiteKindOutdoor, 7)
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// A rename echoes the kind back; the park must still be a park afterwards.
	if _, err := svc.UpdateSite(context.Background(), created.Id, "North Park", "", "🌳", entities.SiteKindOutdoor, 0, 7); err != nil {
		t.Fatalf("UpdateSite: %v", err)
	}
	if repo.row.Kind != entities.SiteKindOutdoor {
		t.Fatalf("kind after rename = %q, want %q", repo.row.Kind, entities.SiteKindOutdoor)
	}
	if repo.row.Name != "North Park" {
		t.Fatalf("name after rename = %q", repo.row.Name)
	}
}

func TestHasPlansOnlyExcludesPointAssets(t *testing.T) {
	if !entities.HasPlans(entities.SiteKindBuilding) || !entities.HasPlans(entities.SiteKindOutdoor) {
		t.Fatal("buildings and outdoor areas own plans")
	}
	if entities.HasPlans(entities.SiteKindPoint) {
		t.Fatal("a point asset has no plan surface")
	}
	// An empty kind is a building (every site that predates the field), so it must own plans —
	// otherwise the existing fleet's floor plans would become unreachable.
	if !entities.HasPlans("") {
		t.Fatal("a kindless (legacy) site must still own plans")
	}
}
