package postgres

import (
	"context"

	sqldb "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IDbCrud interface
type IDbCrud interface {
	Get(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldb.Filter, sorter []sqldb.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
	GetSingle(ctx context.Context, model interface{}, datasrc string) (map[string]interface{}, error)
	Add(ctx context.Context, model interface{}, datasrc string) (uint64, error)
	BeginTx(ctx context.Context) error
	RollbackTx() error
	CommitTx() error
}
