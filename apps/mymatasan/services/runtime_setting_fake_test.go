package services

import (
	"context"
	"errors"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeRuntimeSettingRepo is a minimal in-memory IGenericRepo for the key-value
// methods the settings, backup and setup-state paths touch. It is keyed by Key
// rather than by the generic accessors fakeEntityRepo uses, because every caller
// here looks rows up by their unique key.
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
