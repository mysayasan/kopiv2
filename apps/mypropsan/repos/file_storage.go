package repos

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
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

func (m *fileStorageRepo) GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error) {
	var filters []data.Filter
	filter := data.Filter{
		FieldName: "Guid",
		Compare:   1,
		Value:     guid,
	}

	filters = append(filters, filter)

	res, err := m.dbCrud.SelectSingle(ctx, entities.FileStorage{}, filters, "")
	if err != nil {
		return nil, err
	}

	var model entities.FileStorage
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *fileStorageRepo) Create(ctx context.Context, model entities.FileStorage) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Insert(ctx, model, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return 0, err
		}
		return 0, err
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return 0, err
	}

	return res, nil
}

func (m *fileStorageRepo) CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Insert(ctx, model, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return 0, err
		}
		return 0, err
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return 0, err
	}

	return res, nil
}
