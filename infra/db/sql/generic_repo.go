package dbsql

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// genericRepo struct
type genericRepo[T any] struct {
	dbCrud IDbCrud
}

// Create new IGenericRepo
func NewGenericRepo[T any](dbCrud IDbCrud) IGenericRepo[T] {
	return &genericRepo[T]{
		dbCrud: dbCrud,
	}
}

func (m *genericRepo[T]) Read(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string) ([]*T, uint64, error) {
	var tmodel = new(T)
	res, totalCnt, err := m.dbCrud.Select(ctx, *tmodel, limit, offset, filters, sorter, datasrc)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*T, 0)

	for _, row := range res {
		var model T
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}

func (m *genericRepo[T]) ReadSingle(ctx context.Context, filters []sqldataenums.Filter, datasrc string) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectSingle(ctx, *tmodel, filters, datasrc)
	if err != nil {
		return nil, err
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) ReadById(ctx context.Context, id uint64) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectById(ctx, *tmodel, "", id)
	if err != nil {
		return nil, err
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) ReadByUnique(ctx context.Context, uids ...any) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectByUnique(ctx, *tmodel, "", uids...)
	if err != nil {
		return nil, err
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) ReadByForeign(ctx context.Context, fids ...any) ([]*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectByForeign(ctx, *tmodel, "", fids...)
	if err != nil {
		return nil, err
	}

	list := make([]*T, 0)

	for _, row := range res {
		var model T
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}
	return list, nil
}

func (m *genericRepo[T]) Create(ctx context.Context, model T) (uint64, error) {
	res, err := m.dbCrud.Insert(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) CreateMultiple(ctx context.Context, models []T) (uint64, error) {
	res, err := m.dbCrud.Insert(ctx, models, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) UpdateById(ctx context.Context, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateById(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) UpdateByUnique(ctx context.Context, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateByUnique(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) UpdateByForeign(ctx context.Context, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateByForeign(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) DeleteById(ctx context.Context, id uint64) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.DeleteById(ctx, *tmodel, "", id)
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) DeleteByUnique(ctx context.Context, uids ...any) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.DeleteByUnique(ctx, *tmodel, "", uids...)
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) DeleteByForeign(ctx context.Context, fids ...any) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.DeleteByForeign(ctx, *tmodel, "", fids)
	if err != nil {
		return 0, err
	}

	return res, nil
}
