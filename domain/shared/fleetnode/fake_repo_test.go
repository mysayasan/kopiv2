package fleetnode

import (
	"context"
	"errors"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

var _ = sqldataenums.ASC
var _ dbsql.IDbCrud

// A test fake for the runtime-setting repo, copied from mymatasan's test package. A test fake
// is not API, so it is duplicated rather than exported.

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
