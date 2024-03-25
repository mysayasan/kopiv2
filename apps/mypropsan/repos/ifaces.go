package repos

import (
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IIResidentPropRepo interface
type IResidentPropRepo interface {
	GetLatest(limit uint64, offset uint64, filters []dbsql.Filter, sorter []dbsql.Sorter) ([]*models.ResidentPropModel, uint64, error)
}
