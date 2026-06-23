package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeRuntimeSettingRepo is a minimal in-memory IGenericRepo for the key-value
// methods SetupStateService touches.
type fakeRuntimeSettingRepo struct {
	dbsql.IGenericRepo[entities.RuntimeSetting]
	rows   []*entities.RuntimeSetting
	nextID uint64
}

func (f *fakeRuntimeSettingRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.RuntimeSetting, error) {
	key, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.Key == key {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeRuntimeSettingRepo) Create(_ context.Context, _ string, model entities.RuntimeSetting) (uint64, error) {
	f.nextID++
	model.Id = int64(f.nextID)
	cp := model
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeRuntimeSettingRepo) UpdateById(_ context.Context, _ string, model entities.RuntimeSetting) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == model.Id {
			*r = model
			return 1, nil
		}
	}
	return 0, nil
}

func TestSetupStateLifecycle(t *testing.T) {
	repo := &fakeRuntimeSettingRepo{}
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

	// Re-read sees it completed.
	state, err = svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get after complete: %v", err)
	}
	if !state.Completed {
		t.Fatal("should be completed after Complete")
	}

	// Idempotent: completing again updates in place (one row).
	if _, err := svc.Complete(ctx); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected a single persisted row, got %d", len(repo.rows))
	}
}
