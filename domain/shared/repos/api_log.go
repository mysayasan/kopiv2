package repos

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// apiLogRepo struct
type apiLogRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IApiLogRepo
func NewApiLogRepo(dbCrud postgres.IDbCrud) IApiLogRepo {
	return &apiLogRepo{
		dbCrud: dbCrud,
	}
}

func (m *apiLogRepo) GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entities.ApiLogEntity, uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return nil, 0, err
	}

	res, totalCnt, err := m.dbCrud.Get(ctx, entities.ApiLogEntity{}, limit, offset, filters, sorter, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return nil, 0, err
		}
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return nil, 0, err
	}

	list := make([]*entities.ApiLogEntity, 0)

	for _, row := range res {
		var model entities.ApiLogEntity
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}

func (m *apiLogRepo) Add(ctx context.Context, model entities.ApiLogEntity) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Add(ctx, model, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return 0, err
		}
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return 0, err
	}

	return res, nil
}
