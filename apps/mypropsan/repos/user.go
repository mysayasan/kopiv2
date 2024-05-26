package repos

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
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

func (m *userRepo) GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entity.User, uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return nil, 0, err
	}

	res, totalCnt, err := m.dbCrud.Get(ctx, entity.User{}, limit, offset, filters, sorter, "")
	if err != nil {
		if rbErr := m.dbCrud.RollbackTx(); rbErr != nil {
			err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			return nil, 0, err
		}
	}

	if err = m.dbCrud.CommitTx(); err != nil {
		return nil, 0, err
	}

	list := make([]*entity.User, 0)

	for _, row := range res {
		var model entity.User
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}

func (m *userRepo) GetByEmail(ctx context.Context, email string) (*entity.User, error) {
	var filters []data.Filter
	filter := data.Filter{
		FieldName: "Email",
		Compare:   1,
		Value:     email,
	}

	filters = append(filters, filter)

	res, err := m.dbCrud.GetSingle(ctx, entity.User{}, filters, "")
	if err != nil {
		return nil, err
	}

	var model entity.User
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *userRepo) Add(ctx context.Context, model entity.User) (uint64, error) {
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
