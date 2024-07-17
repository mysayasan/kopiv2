package repos

import (
	"context"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// userRepo struct
type userRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IUserRepo
func NewUserRepo(dbCrud postgres.IDbCrud) IUserRepo {
	return &userRepo{
		dbCrud: dbCrud,
	}
}

func (m *userRepo) GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entities.UserLogin, uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return nil, 0, err
	}

	res, totalCnt, err := m.dbCrud.Select(ctx, entities.UserLogin{}, limit, offset, filters, sorter, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return nil, 0, err
		}
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return nil, 0, err
	}

	list := make([]*entities.UserLogin, 0)

	for _, row := range res {
		var model entities.UserLogin
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}

func (m *userRepo) GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	var filters []data.Filter
	filter := data.Filter{
		FieldName: "Email",
		Compare:   1,
		Value:     email,
	}

	filters = append(filters, filter)

	res, err := m.dbCrud.SelectSingle(ctx, entities.UserLogin{}, filters, "")
	if err != nil {
		return nil, err
	}

	if len(res) < 1 {
		return nil, errors.New("not found")
	}

	var model entities.UserLogin
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *userRepo) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Insert(ctx, model, "")
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

func (m *userRepo) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Update(ctx, model, "")
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
