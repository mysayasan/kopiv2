package postgres

import sqldb "github.com/mysayasan/kopiv2/infra/db/sql"

// IDbCrud interface
type IDbCrud interface {
	Get(model interface{}, dataset string, limit uint64, offset uint64, filters ...sqldb.Filter) ([]map[string]interface{}, uint64, error)
}
