package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type fakeWallRepo struct {
	dbsql.IGenericRepo[entities.WallLayout]
	rows []*entities.WallLayout
	seq  int64
}

func (f *fakeWallRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.WallLayout, uint64, error) {
	var out []*entities.WallLayout
	for _, row := range f.rows {
		keep := true
		for _, fl := range filters {
			if fl.FieldName == "IsDefault" {
				keep = keep && row.IsDefault == fl.Value.(bool)
			}
		}
		if keep {
			cp := *row
			out = append(out, &cp)
		}
	}
	for _, s := range sorters {
		if s.FieldName == "Name" {
			sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		}
	}
	total := uint64(len(out))
	if offset >= uint64(len(out)) {
		return nil, total, nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeWallRepo) GetById(_ context.Context, _ string, id uint64) (*entities.WallLayout, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeWallRepo) Create(_ context.Context, _ string, model entities.WallLayout) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeWallRepo) UpdateById(_ context.Context, _ string, model entities.WallLayout) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeWallRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// fakeWallCameras answers "which camera ids exist".
type fakeWallCameras struct{ ids []int64 }

func (f *fakeWallCameras) Get(_ context.Context, _ uint64, _ uint64) ([]*CameraDetail, uint64, error) {
	out := make([]*CameraDetail, 0, len(f.ids))
	for _, id := range f.ids {
		d := &CameraDetail{}
		d.Id = id
		out = append(out, d)
	}
	return out, uint64(len(out)), nil
}

func newWallFixture(cameraIds ...int64) (*wallService, *fakeWallRepo) {
	repo := &fakeWallRepo{}
	svc := &wallService{repo: repo, cameras: &fakeWallCameras{ids: cameraIds}, now: func() int64 { return 1_700_000_000 }}
	return svc, repo
}

func TestAWallNeedsANameAGridAndACamera(t *testing.T) {
	svc, _ := newWallFixture(1, 2)
	ctx := context.Background()
	if _, err := svc.Save(ctx, WallSave{Name: " ", Grid: "2x2", CameraIds: []int64{1}}); err == nil {
		t.Fatal("a nameless wall must be refused")
	}
	if _, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "9x9", CameraIds: []int64{1}}); err == nil {
		t.Fatal("an unknown grid must be refused")
	}
	// And the refusal must NAME the grids that exist, or the client is left guessing at
	// the one thing the server will accept.
	_, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "9x9", CameraIds: []int64{1}})
	if !strings.Contains(err.Error(), "3x3") {
		t.Fatalf("the refusal must list the grids that exist: %v", err)
	}
	if _, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "2x2"}); err == nil {
		t.Fatal("a wall with no cameras must be refused")
	}
}

func TestWallNamesAreUniqueRegardlessOfCase(t *testing.T) {
	svc, _ := newWallFixture(1, 2)
	ctx := context.Background()
	if _, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Two walls called "Perimeter" and "perimeter" are two rows nobody can tell apart in
	// the picker that chooses between them.
	if _, err := svc.Save(ctx, WallSave{Name: "perimeter", Grid: "2x2", CameraIds: []int64{2}}); err == nil {
		t.Fatal("a duplicate name must be refused")
	}
}

func TestSavingAWallOverItselfKeepsItsName(t *testing.T) {
	svc, _ := newWallFixture(1, 2)
	ctx := context.Background()
	wall, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// The uniqueness check must exclude the row being saved, or a wall can never be
	// edited without renaming it.
	if _, err := svc.Save(ctx, WallSave{Id: wall.Id, Name: "Perimeter", Grid: "3x3", CameraIds: []int64{1, 2}}); err != nil {
		t.Fatalf("re-saving a wall under its own name must work: %v", err)
	}
}

// THE ONE THAT IS EASY TO GET WRONG. "The default" with two answers is a screen that opens
// differently depending on which row the database hands back first.
func TestOnlyOneWallCanBeTheDefault(t *testing.T) {
	svc, repo := newWallFixture(1, 2, 3)
	ctx := context.Background()
	first, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1}, IsDefault: true})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	second, err := svc.Save(ctx, WallSave{Name: "Loading bays", Grid: "2x2", CameraIds: []int64{2}, IsDefault: true})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	defaults := []int64{}
	for _, row := range repo.rows {
		if row.IsDefault {
			defaults = append(defaults, row.Id)
		}
	}
	if len(defaults) != 1 || defaults[0] != second.Id {
		t.Fatalf("expected only wall %d to be the default, got %v (first was %d)", second.Id, defaults, first.Id)
	}
}

func TestWallCycleAndPopBoundsAreRefusedNotClamped(t *testing.T) {
	svc, _ := newWallFixture(1)
	ctx := context.Background()
	base := WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1}}
	// Refused rather than clamped: a wall silently cycling at 3s when the operator asked
	// for 1s is a wall nobody can explain the behaviour of.
	bad := base
	bad.CycleSeconds = 1
	if _, err := svc.Save(ctx, bad); err == nil {
		t.Fatal("a one-second cycle must be refused")
	}
	bad = base
	bad.AutoPopSeconds = 1
	if _, err := svc.Save(ctx, bad); err == nil {
		t.Fatal("a one-second pop must be refused")
	}
	ok := base
	ok.CycleSeconds = 0
	ok.AutoPopSeconds = 0
	if _, err := svc.Save(ctx, ok); err != nil {
		t.Fatalf("zero must mean off, not invalid: %v", err)
	}
}

func TestWallKeepsCameraOrderAndDropsDuplicates(t *testing.T) {
	svc, _ := newWallFixture(1, 2, 3)
	wall, err := svc.Save(context.Background(), WallSave{
		Name: "Perimeter", Grid: "2x2", CameraIds: []int64{3, 1, 3, 2},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// Order IS the arrangement — sorting it would rearrange somebody's wall on save.
	if len(wall.CameraIds) != 3 || wall.CameraIds[0] != 3 || wall.CameraIds[1] != 1 || wall.CameraIds[2] != 2 {
		t.Fatalf("camera order must survive the round trip: %v", wall.CameraIds)
	}
}

// A wall naming a camera that has been deleted must SAY so. Quietly rendering five tiles
// where six were arranged tells an operator they are watching everything, and the camera
// that went missing is the one nobody is watching.
func TestAWallReportsCamerasThatNoLongerExist(t *testing.T) {
	svc, _ := newWallFixture(1, 2)
	ctx := context.Background()
	if _, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1, 2}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc.cameras = &fakeWallCameras{ids: []int64{1}}

	walls, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(walls) != 1 {
		t.Fatalf("expected one wall, got %d", len(walls))
	}
	if len(walls[0].CameraIds) != 2 {
		t.Fatalf("the wall must still name every camera it was built with: %v", walls[0].CameraIds)
	}
	if len(walls[0].MissingCameras) != 1 || walls[0].MissingCameras[0] != 2 {
		t.Fatalf("the deleted camera must be reported: %v", walls[0].MissingCameras)
	}
}

// Fail QUIET, not wrong: if the camera list cannot be read, reporting every camera as
// deleted would be a false alarm on every wall at once.
func TestAnUnreadableCameraListReportsNothingMissing(t *testing.T) {
	svc, _ := newWallFixture(1, 2)
	ctx := context.Background()
	if _, err := svc.Save(ctx, WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1, 2}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	svc.cameras = nil

	walls, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(walls[0].MissingCameras) != 0 {
		t.Fatalf("with no camera list to check against, nothing may be claimed missing: %v",
			walls[0].MissingCameras)
	}
}

func TestMissingCamerasEncodesAsAnEmptyListNotNull(t *testing.T) {
	svc, _ := newWallFixture(1)
	wall, err := svc.Save(context.Background(), WallSave{Name: "Perimeter", Grid: "2x2", CameraIds: []int64{1}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// Same rule the export manifest's gap list follows: a missing field reads as "did not
	// look", and the screen would render `null.length`.
	if wall.MissingCameras == nil {
		t.Fatal("missingCameras must never be null")
	}
}

func TestDeletingAWallThatIsNotThereSaysSo(t *testing.T) {
	svc, _ := newWallFixture(1)
	if err := svc.Delete(context.Background(), 99); err == nil {
		t.Fatal("deleting a wall that does not exist must be an error, not a silent success")
	}
}
