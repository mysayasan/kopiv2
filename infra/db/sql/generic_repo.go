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

func (m *genericRepo[T]) ReadAll(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter) ([]*T, uint64, error) {
	var tmodel = new(T)
	res, totalCnt, err := m.dbCrud.Select(ctx, *tmodel, limit, offset, filters, sorter, "")
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

func (m *genericRepo[T]) ReadByPKey(ctx context.Context, ids ...uint64) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectByPKey(ctx, *tmodel, "", ids...)
	if err != nil {
		return nil, err
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) ReadByUKey(ctx context.Context, uids ...any) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectByUKey(ctx, *tmodel, "", uids...)
	if err != nil {
		return nil, err
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
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

func (m *genericRepo[T]) Update(ctx context.Context, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateByPKey(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (m *genericRepo[T]) Delete(ctx context.Context, model T) (uint64, error) {
	res, err := m.dbCrud.DeleteByPKey(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}
