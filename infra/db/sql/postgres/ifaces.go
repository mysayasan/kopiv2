package postgres

import (
	"reflect"

	sqldb "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IDbCrud interface
type IDbCrud interface {
	Get(model reflect.Value, limit uint64, offset uint64, filters []sqldb.Filter, sorter []sqldb.Sorter, datasrc string) ([]map[string]interface{}, uint64, error)
}
