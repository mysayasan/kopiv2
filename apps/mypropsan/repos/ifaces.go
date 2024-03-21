package repos

import (
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IIResidentPropRepo interface
type IResidentPropRepo interface {
	GetLatest(limit uint64, offset uint64, filters ...dbsql.Filter) ([]*models.ResidentPropListModel, uint64, error)
}
