package postgres

import (
	sqldb "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IDbCrud interface
type IDbCrud interface {
	Get(model interface{}, limit uint64, offset uint64, filters []sqldb.Filter, sorter []sqldb.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
	GetSingle(model interface{}, datasrc string) (map[string]interface{}, error)
}
