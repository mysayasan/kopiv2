package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// homeService struct
type homeService struct {
	dbCrud           dbsql.IDbCrud
	resPropRepoModel dbsql.IGenericRepo[models.ResidentProp]
}

// Create new IHomeService
func NewHomeService(dbCrud dbsql.IDbCrud) IHomeService {
	return &homeService{
		dbCrud:           dbCrud,
		resPropRepoModel: dbsql.NewGenericRepo[models.ResidentProp](dbCrud),
	}
}

func (m *homeService) GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentProp, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "LandAreaSize",
			Sort:      2,
		},
	}

	return m.resPropRepoModel.ReadAll(ctx, limit, offset, nil, sorters)
}
