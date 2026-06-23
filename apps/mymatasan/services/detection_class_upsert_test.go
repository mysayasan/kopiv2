package services

import (
	"context"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeClassRepo is a minimal in-memory IGenericRepo for the three methods
// UpsertTrained touches (Get/Create/UpdateById). Get returns fresh copies so the
// test exercises the UpdateById persistence path rather than mutating shared
// pointers. Other interface methods are unimplemented (embedded nil) and panic
// if called, which they aren't here.
type fakeClassRepo struct {
	dbsql.IGenericRepo[entities.DetectionClass]
	rows   []*entities.DetectionClass
	nextID uint64
}

func (f *fakeClassRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.DetectionClass, uint64, error) {
	out := make([]*entities.DetectionClass, len(f.rows))
	for i, r := range f.rows {
		cp := *r
		out[i] = &cp
	}
	return out, uint64(len(out)), nil
}

func (f *fakeClassRepo) Create(_ context.Context, _ string, model entities.DetectionClass) (uint64, error) {
	f.nextID++
	model.Id = int64(f.nextID)
	cp := model
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeClassRepo) UpdateById(_ context.Context, _ string, model entities.DetectionClass) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == model.Id {
			*r = model
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeClassRepo) byName(name string) *entities.DetectionClass {
	for _, r := range f.rows {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func newFireSmokeRegistry() *fakeClassRepo {
	repo := &fakeClassRepo{}
	for _, seed := range []struct {
		name, member, kind string
	}{
		{"fire", "fire", ClassKindHazard},
		{"smoke", "smoke", ClassKindHazard},
	} {
		repo.nextID++
		repo.rows = append(repo.rows, &entities.DetectionClass{
			Id:      int64(repo.nextID),
			Name:    seed.name,
			Kind:    seed.kind,
			Members: `["` + seed.member + `"]`,
			Source:  ClassSourceBuiltin,
		})
	}
	return repo
}

func memberSet(row *entities.DetectionClass) map[string]bool {
	out := map[string]bool{}
	for _, m := range decodeMembers(row.Members) {
		out[m] = true
	}
	return out
}

// A model that emits non-canonical spellings ("flame", "Smoking") should fold
// those into the existing Fire/Smoke hazard classes, not create new ones.
func TestUpsertTrainedFoldsSynonymsIntoBuiltins(t *testing.T) {
	repo := newFireSmokeRegistry()
	svc := &detectionClassService{repo: repo, now: fixedNow}

	if err := svc.UpsertTrained(context.Background(), []string{"flame", "Smoking"}, 1); err != nil {
		t.Fatalf("UpsertTrained: %v", err)
	}

	if got := len(repo.rows); got != 2 {
		t.Fatalf("expected no new classes (still 2), got %d: %+v", got, repo.rows)
	}
	if m := memberSet(repo.byName("fire")); !m["fire"] || !m["flame"] {
		t.Fatalf("fire members = %v, want fire+flame", m)
	}
	if m := memberSet(repo.byName("smoke")); !m["smoke"] || !m["smoking"] {
		t.Fatalf("smoke members = %v, want smoke+smoking", m)
	}
}

// The canonical label itself, when already a member, is a no-op (no duplicate
// member, no new class).
func TestUpsertTrainedExactCanonicalIsNoop(t *testing.T) {
	repo := newFireSmokeRegistry()
	svc := &detectionClassService{repo: repo, now: fixedNow}

	if err := svc.UpsertTrained(context.Background(), []string{"fire", "smoke"}, 1); err != nil {
		t.Fatalf("UpsertTrained: %v", err)
	}
	if got := len(repo.rows); got != 2 {
		t.Fatalf("expected 2 classes, got %d", got)
	}
	if m := decodeMembers(repo.byName("fire").Members); len(m) != 1 {
		t.Fatalf("fire members = %v, want exactly [fire]", m)
	}
}

// A label with no synonym match still registers as its own trained class.
func TestUpsertTrainedUnknownLabelCreatesClass(t *testing.T) {
	repo := newFireSmokeRegistry()
	svc := &detectionClassService{repo: repo, now: fixedNow}

	if err := svc.UpsertTrained(context.Background(), []string{"forklift"}, 1); err != nil {
		t.Fatalf("UpsertTrained: %v", err)
	}
	row := repo.byName("forklift")
	if row == nil || row.Source != ClassSourceTrained {
		t.Fatalf("forklift class = %+v, want a trained class", row)
	}
}

func fixedNow() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
