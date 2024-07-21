package repos

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/domain/enums/data"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// residentPropRepo struct
type residentPropRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IResidentPropRepo
func NewResidentPropRepo(dbCrud postgres.IDbCrud) IResidentPropRepo {
	return &residentPropRepo{
		dbCrud: dbCrud,
	}
}

func (m *residentPropRepo) GetLatest(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*models.ResidentProp, uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return nil, 0, err
	}

	res, totalCnt, err := m.dbCrud.Select(ctx, models.ResidentProp{}, limit, offset, filters, sorter, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return nil, 0, err
		}
		return nil, 0, err
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return nil, 0, err
	}

	list := make([]*models.ResidentProp, 0)

	for _, row := range res {
		var model models.ResidentProp
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}
