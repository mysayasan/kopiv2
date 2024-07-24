package dbsql

import (
	"context"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// IDbCrud interface
type IDbCrud interface {
	Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
	SelectSingle(ctx context.Context, model interface{}, filters []sqldataenums.Filter, datasrc string) (map[string]interface{}, error)
	SelectByPKey(ctx context.Context, model interface{}, datasrc string, ids ...any) (map[string]interface{}, error)
	SelectByUKey(ctx context.Context, model interface{}, datasrc string, uids ...any) (map[string]interface{}, error)
	SelectByFKey(ctx context.Context, model interface{}, datasrc string, fids ...any) ([]map[string]interface{}, error)
	Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	UpdateByPKey(ctx context.Context, model interface{}, datasrc string, ids ...any) (uint64, error)
	UpdateByUKey(ctx context.Context, model interface{}, datasrc string, uids ...any) (uint64, error)
	UpdateByFKey(ctx context.Context, model interface{}, datasrc string, fids ...any) (uint64, error)
	DeleteByPKey(ctx context.Context, model interface{}, datasrc string, ids ...any) (uint64, error)
	DeleteByUKey(ctx context.Context, model interface{}, datasrc string, uids ...any) (uint64, error)
	DeleteByFKey(ctx context.Context, model interface{}, datasrc string, fids ...any) (uint64, error)
	BeginTx(ctx context.Context) error
	RollbackTx() error
	CommitTx() error
}

// IGenericRepo interface
type IGenericRepo[T any] interface {
	Read(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string) ([]*T, uint64, error)
	ReadSingle(ctx context.Context, filters []sqldataenums.Filter, datasrc string) (*T, error)
	ReadById(ctx context.Context, ids ...any) (*T, error)
	ReadByUnique(ctx context.Context, uids ...any) (*T, error)
	ReadByForeign(ctx context.Context, fids ...any) ([]*T, error)
	Create(ctx context.Context, model T) (uint64, error)
	CreateMultiple(ctx context.Context, models []T) (uint64, error)
	Update(ctx context.Context, model T) (uint64, error)
	Delete(ctx context.Context, model T) (uint64, error)
}
