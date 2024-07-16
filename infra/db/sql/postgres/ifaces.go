package postgres

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// IDbCrud interface
type IDbCrud interface {
	Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
	SelectSingle(ctx context.Context, model interface{}, filters []data.Filter, datasrc string) (map[string]interface{}, error)
	Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	Update(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	BeginTx(ctx context.Context) error
	RollbackTx() error
	CommitTx() error
}
