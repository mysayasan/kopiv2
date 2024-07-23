package dbsql

import (
	"context"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// IDbCrud interface
type IDbCrud interface {
	Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
	SelectSingle(ctx context.Context, model interface{}, filters []sqldataenums.Filter, datasrc string) (map[string]interface{}, error)
	SelectByPKey(ctx context.Context, model interface{}, datasrc string, ids ...uint64) (map[string]interface{}, error)
	SelectByUKey(ctx context.Context, model interface{}, datasrc string, uids ...any) (map[string]interface{}, error)
	SelectByFKey(ctx context.Context, model interface{}, datasrc string, fids ...any) ([]map[string]interface{}, error)
	Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	UpdateByPKey(ctx context.Context, model interface{}, datasrc string, ids ...uint64) (uint64, error)
	UpdateByUKey(ctx context.Context, model interface{}, datasrc string, uids ...any) (uint64, error)
	UpdateByFKey(ctx context.Context, model interface{}, datasrc string, fids ...any) (uint64, error)
	DeleteByPKey(ctx context.Context, model interface{}, datasrc string, ids ...uint64) (uint64, error)
	DeleteByUKey(ctx context.Context, model interface{}, datasrc string, uids ...any) (uint64, error)
	DeleteByFKey(ctx context.Context, model interface{}, datasrc string, fids ...any) (uint64, error)
	BeginTx(ctx context.Context) error
	RollbackTx() error
	CommitTx() error
}

// IGenericRepo interface
type IGenericRepo[T any] interface {
	ReadAll(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter) ([]*T, uint64, error)
	ReadByPKey(ctx context.Context, pkeys ...uint64) (*T, error)
	ReadByUKey(ctx context.Context, ukeys ...any) (*T, error)
	Create(ctx context.Context, model T) (uint64, error)
	CreateMultiple(ctx context.Context, models []T) (uint64, error)
	Update(ctx context.Context, model T) (uint64, error)
	Delete(ctx context.Context, model T) (uint64, error)
}
