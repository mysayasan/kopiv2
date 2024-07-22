package dbsql

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// IDbCrud interface
type IDbCrud interface {
	Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
	SelectSingle(ctx context.Context, model interface{}, filters []data.Filter, datasrc string) (map[string]interface{}, error)
	Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	Update(ctx context.Context, model interface{}, datasrc string, updByUKey bool) (uint64, error)
	Delete(ctx context.Context, model interface{}, datasrc string, delByUKey bool) (uint64, error)
	BeginTx(ctx context.Context) error
	RollbackTx() error
	CommitTx() error
}

// IGenericRepo interface
type IGenericRepo[T any] interface {
	ReadAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*T, uint64, error)
	ReadByIds(ctx context.Context, ids ...uint64) (*T, error)
	ReadByUids(ctx context.Context, uids ...any) (*T, error)
	Create(ctx context.Context, model T) (uint64, error)
	CreateMultiple(ctx context.Context, models []T) (uint64, error)
	Update(ctx context.Context, model T) (uint64, error)
	Delete(ctx context.Context, model T) (uint64, error)
}
