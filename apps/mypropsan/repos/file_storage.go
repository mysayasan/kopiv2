package repos

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// fileStorageRepo struct
type fileStorageRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IFileStorageRepo
func NewFileStorageRepo(dbCrud postgres.IDbCrud) IFileStorageRepo {
	return &fileStorageRepo{
		dbCrud: dbCrud,
	}
}

func (m *fileStorageRepo) GetByGuid(ctx context.Context, guid string) (*entity.FileStorageEntity, error) {
	res, err := m.dbCrud.GetSingle(ctx, entity.FileStorageEntity{}, "")
	if err != nil {
		return nil, err
	}
	var model *entity.FileStorageEntity
	mapstructure.Decode(res, model)

	return model, nil
}

func (m *fileStorageRepo) Add(ctx context.Context, model entity.FileStorageEntity) (uint64, error) {
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

func (m *fileStorageRepo) AddMultiple(ctx context.Context, model []entity.FileStorageEntity) (uint64, error) {
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
