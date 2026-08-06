package services

import (
	"context"
	"errors"
	"testing"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// in-memory fake of the RuntimeSetting repo (only the methods the cursor uses).
type fakeRuntimeSettingRepo struct {
	dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	rows   []*sharedentities.RuntimeSetting
	nextID int64
}

func (f *fakeRuntimeSettingRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*sharedentities.RuntimeSetting, error) {
	key, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.Key == key {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeRuntimeSettingRepo) Create(_ context.Context, _ string, m sharedentities.RuntimeSetting) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeRuntimeSettingRepo) UpdateById(_ context.Context, _ string, m sharedentities.RuntimeSetting) (uint64, error) {
	for i, r := range f.rows {
		if r.Id == m.Id {
			cp := m
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func TestRollupCursorMissingRowReadsZero(t *testing.T) {
	c := NewRollupCursor(&fakeRuntimeSettingRepo{})
	got, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("Get on empty repo: %v", err)
	}
	if got != 0 {
		t.Fatalf("missing cursor row should read 0, got %d", got)
	}
}

func TestRollupCursorSetGetRoundtrip(t *testing.T) {
	repo := &fakeRuntimeSettingRepo{}
	c := NewRollupCursor(repo)
	ctx := context.Background()

	if err := c.Set(ctx, 42); err != nil {
		t.Fatalf("Set(create): %v", err)
	}
	if got, _ := c.Get(ctx); got != 42 {
		t.Fatalf("roundtrip after create = %d, want 42", got)
	}
	// Second Set must update the existing row, not create a duplicate.
	if err := c.Set(ctx, 99); err != nil {
		t.Fatalf("Set(update): %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("Set twice must keep one row, got %d", len(repo.rows))
	}
	if got, _ := c.Get(ctx); got != 99 {
		t.Fatalf("roundtrip after update = %d, want 99", got)
	}
}

func TestRollupCursorCorruptValueReadsZero(t *testing.T) {
	repo := &fakeRuntimeSettingRepo{}
	repo.rows = append(repo.rows, &sharedentities.RuntimeSetting{Id: 1, Key: rollupCursorKey, Value: "not-a-number"})
	c := NewRollupCursor(repo)
	got, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("Get with corrupt value: %v", err)
	}
	if got != 0 {
		t.Fatalf("corrupt cursor value should restart from 0, got %d", got)
	}
}
