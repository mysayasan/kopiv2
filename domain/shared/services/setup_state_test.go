package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeSetupSettingRepo is a minimal in-memory IGenericRepo covering only the
// key-value methods the setup-state service touches.
type fakeSetupSettingRepo struct {
	dbsql.IGenericRepo[entities.RuntimeSetting]
	rows   []*entities.RuntimeSetting
	nextID int64
	// missAsZeroRow makes GetByUnique report "not present" the way some repo
	// backends do — a zero-valued row instead of an error.
	missAsZeroRow bool
}

func (f *fakeSetupSettingRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.RuntimeSetting, error) {
	key, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.Key == key {
			cp := *r
			return &cp, nil
		}
	}
	if f.missAsZeroRow {
		return &entities.RuntimeSetting{}, nil
	}
	return nil, errors.New("select by unique failed: no result found")
}

func (f *fakeSetupSettingRepo) Create(_ context.Context, _ string, model entities.RuntimeSetting) (uint64, error) {
	f.nextID++
	model.Id = f.nextID
	cp := model
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeSetupSettingRepo) UpdateById(_ context.Context, _ string, model entities.RuntimeSetting) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == model.Id {
			*r = model
			return 1, nil
		}
	}
	return 0, nil
}

func TestSetupStateLifecycle(t *testing.T) {
	repo := &fakeSetupSettingRepo{}
	svc := NewSetupStateService(repo)
	ctx := context.Background()

	// Fresh install: not completed.
	state, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Completed {
		t.Fatal("fresh install should not be completed")
	}

	// Complete persists the flag.
	done, err := svc.Complete(ctx)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !done.Completed || done.CompletedAt == 0 {
		t.Fatalf("Complete returned %#v", done)
	}
	if len(repo.rows) != 1 || !strings.Contains(repo.rows[0].Value, `"completed":true`) {
		t.Fatalf("row not persisted: %+v", repo.rows)
	}

	// Re-read sees it completed.
	state, err = svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get after complete: %v", err)
	}
	if !state.Completed {
		t.Fatal("should be completed after Complete")
	}

	// Idempotent: completing again updates in place (still one row).
	if _, err := svc.Complete(ctx); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected a single persisted row, got %d", len(repo.rows))
	}
}

// A repo that signals "missing" with a zero row rather than an error must still
// INSERT — updating id 0 would report success while persisting nothing, and the
// wizard would then reappear on every boot.
func TestSetupStateCompleteInsertsWhenMissRowIsZeroValued(t *testing.T) {
	repo := &fakeSetupSettingRepo{missAsZeroRow: true}
	svc := NewSetupStateService(repo)
	ctx := context.Background()

	if _, err := svc.Complete(ctx); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected the flag to be inserted, got %d rows", len(repo.rows))
	}

	state, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !state.Completed {
		t.Fatal("setup flag did not survive: the zero row was updated instead of inserted")
	}
}

// A corrupt value re-runs setup rather than wedging the app on a parse error.
func TestSetupStateGetTreatsCorruptValueAsIncomplete(t *testing.T) {
	repo := &fakeSetupSettingRepo{}
	if _, err := repo.Create(context.Background(), "", entities.RuntimeSetting{Key: SetupStateKey, Value: "{not-json"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	state, err := NewSetupStateService(repo).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Completed {
		t.Fatal("corrupt value should read as not completed")
	}
}
