package dbsql

import (
	"context"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// IDbCrud interface
type IDbCrud interface {
	Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string, joinsrc ...string) ([]map[string]interface{}, uint64, error)
	SelectSingle(ctx context.Context, model interface{}, filters []sqldataenums.Filter, datasrc string) (map[string]interface{}, error)
	SelectById(ctx context.Context, model interface{}, datasrc string, id uint64) (map[string]interface{}, error)
	SelectByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (map[string]interface{}, error)
	SelectByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) ([]map[string]interface{}, error)
	Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	UpdateById(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	UpdateByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string) (uint64, error)
	UpdateByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string) (uint64, error)
	DeleteById(ctx context.Context, model interface{}, datasrc string, id uint64) (uint64, error)
	DeleteByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (uint64, error)
	DeleteByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) (uint64, error)
	BeginTx(ctx context.Context) error
	RollbackTx() error
	CommitTx() error
}

// IGenericRepo interface
type IGenericRepo[T any] interface {
	Get(ctx context.Context, datasrc string, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter) ([]*T, uint64, error)
	GetJoin(ctx context.Context, datasrc string, model any, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, joinsrc ...string) ([]map[string]any, uint64, error)
	GetSingle(ctx context.Context, datasrc string, filters []sqldataenums.Filter) (*T, error)
	GetById(ctx context.Context, datasrc string, id uint64) (*T, error)
	GetByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (*T, error)
	GetByForeign(ctx context.Context, datasrc string, keyGroup string, fids ...any) ([]*T, error)
	Create(ctx context.Context, datasrc string, model T) (uint64, error)
	CreateMultiple(ctx context.Context, datasrc string, models []T) (uint64, error)
	UpdateById(ctx context.Context, datasrc string, model T) (uint64, error)
	UpdateByUnique(ctx context.Context, datasrc string, keyGroup string, model T) (uint64, error)
	UpdateByForeign(ctx context.Context, datasrc string, keyGroup string, model T) (uint64, error)
	DeleteById(ctx context.Context, datasrc string, id uint64) (uint64, error)
	DeleteByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (uint64, error)
	DeleteByForeign(ctx context.Context, datasrc string, keyGroup string, fids ...any) (uint64, error)
}
