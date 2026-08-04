package dbsql

import (
	"context"
	"fmt"
	"strings"

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
		if isNoResultErr(err) {
			return []*T{}, 0, nil
		}
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

func isNoResultErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func (m *genericRepo[T]) GetJoin(ctx context.Context, datasrc string, model any, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, joinsrc ...string) ([]map[string]any, uint64, error) {
	res, totalCnt, err := m.dbCrud.Select(ctx, model, limit, offset, filters, sorter, datasrc, joinsrc...)
	if err != nil {
		return nil, 0, fmt.Errorf("select join failed: %w", err)
	}

	return res, totalCnt, nil
}

func (m *genericRepo[T]) GetJoinWithSpec(ctx context.Context, datasrc string, model any, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, joins ...JoinSpec) ([]map[string]any, uint64, error) {
	res, totalCnt, err := m.dbCrud.SelectJoin(ctx, model, limit, offset, filters, sorter, datasrc, joins...)
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
	// NOTE: in practice a missing row does NOT reach this branch. SelectById's underlying Select
	// treats zero rows as an error ("no result found") rather than returning a nil map, so that
	// error is returned above and this nil check never fires for the not-found case. Unlike
	// GetByUnique below (where SelectByUnique genuinely returns a nil map, and this check is live),
	// callers of GetById must not rely on `res == nil`; they must check the returned error instead
	// (see apps/mypintusan/services/store_sql.go's isNotFound for one such caller-side workaround).
	if res == nil {
		return nil, nil
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
	// Not found → nil (not a zero-value struct). SelectByUnique returns a nil map when
	// no row matches; decoding that would yield a non-nil zero struct, silently
	// defeating every `x == nil` not-found check across the codebase (a serious bug for
	// auth/RBAC lookups, e.g. treating a missing role/user as a real zero-id record).
	if res == nil {
		return nil, nil
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
