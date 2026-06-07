package dbsql

import (
	"context"
	"fmt"

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

func (m *genericRepo[T]) Get(ctx context.Context, datasrc string, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter) ([]*T, uint64, error) {
	var tmodel = new(T)
	res, totalCnt, err := m.dbCrud.Select(ctx, *tmodel, limit, offset, filters, sorter, datasrc)
	if err != nil {
		return nil, 0, fmt.Errorf("select list failed: %w", err)
	}

	list := make([]*T, 0)

	for _, row := range res {
		row := row
		var model T
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}

func (m *genericRepo[T]) GetJoin(ctx context.Context, datasrc string, model any, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, joinsrc ...string) ([]map[string]any, uint64, error) {

	res, totalCnt, err := m.dbCrud.Select(ctx, model, limit, offset, filters, sorter, datasrc, joinsrc...)
	if err != nil {
		return nil, 0, fmt.Errorf("select join failed: %w", err)
	}

	return res, totalCnt, nil
}

func (m *genericRepo[T]) GetSingle(ctx context.Context, datasrc string, filters []sqldataenums.Filter) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectSingle(ctx, *tmodel, filters, datasrc)
	if err != nil {
		return nil, fmt.Errorf("select single failed: %w", err)
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) GetById(ctx context.Context, datasrc string, id uint64) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectById(ctx, *tmodel, datasrc, id)
	if err != nil {
		return nil, fmt.Errorf("select by id failed: %w", err)
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) GetByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectByUnique(ctx, *tmodel, datasrc, keyGroup, uids...)
	if err != nil {
		return nil, fmt.Errorf("select by unique failed: %w", err)
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *genericRepo[T]) GetByForeign(ctx context.Context, datasrc string, keyGroup string, fids ...any) ([]*T, error) {
	var tmodel = new(T)
	res, err := m.dbCrud.SelectByForeign(ctx, *tmodel, datasrc, keyGroup, fids...)
	if err != nil {
		return nil, fmt.Errorf("select by foreign failed: %w", err)
	}

	list := make([]*T, 0)

	for _, row := range res {
		row := row
		var model T
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}
	return list, nil
}

func (m *genericRepo[T]) Create(ctx context.Context, datasrc string, model T) (uint64, error) {
	res, err := m.dbCrud.Insert(ctx, model, datasrc)
	if err != nil {
		return 0, fmt.Errorf("insert failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) CreateMultiple(ctx context.Context, datasrc string, models []T) (uint64, error) {
	res, err := m.dbCrud.Insert(ctx, models, datasrc)
	if err != nil {
		return 0, fmt.Errorf("bulk insert failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) UpdateById(ctx context.Context, datasrc string, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateById(ctx, model, datasrc)
	if err != nil {
		return 0, fmt.Errorf("update by id failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) UpdateByUnique(ctx context.Context, datasrc string, keyGroup string, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateByUnique(ctx, model, datasrc, keyGroup)
	if err != nil {
		return 0, fmt.Errorf("update by unique failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) UpdateByForeign(ctx context.Context, datasrc string, keyGroup string, model T) (uint64, error) {
	res, err := m.dbCrud.UpdateByForeign(ctx, model, datasrc, keyGroup)
	if err != nil {
		return 0, fmt.Errorf("update by foreign failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) Delete(ctx context.Context, datasrc string, filters []sqldataenums.Filter) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.Delete(ctx, *tmodel, datasrc, filters)
	if err != nil {
		return 0, fmt.Errorf("delete failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) DeleteById(ctx context.Context, datasrc string, id uint64) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.DeleteById(ctx, *tmodel, datasrc, id)
	if err != nil {
		return 0, fmt.Errorf("delete by id failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) DeleteByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.DeleteByUnique(ctx, *tmodel, datasrc, keyGroup, uids...)
	if err != nil {
		return 0, fmt.Errorf("delete by unique failed: %w", err)
	}

	return res, nil
}

func (m *genericRepo[T]) DeleteByForeign(ctx context.Context, datasrc string, keyGroup string, fids ...any) (uint64, error) {
	tmodel := new(T)
	res, err := m.dbCrud.DeleteByForeign(ctx, *tmodel, datasrc, keyGroup, fids...)
	if err != nil {
		return 0, fmt.Errorf("delete by foreign failed: %w", err)
	}

	return res, nil
}
