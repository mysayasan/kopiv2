package repos

import (
	"context"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/domain/enums/data"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// userRepo struct
type userRepo[T any] struct {
	dbCrud postgres.IDbCrud
}

// Create new IUserRepo
func NewUserRepo[T any](dbCrud postgres.IDbCrud) IRepo[T] {
	return &userRepo[T]{
		dbCrud: dbCrud,
	}
}

func (m *userRepo[T]) GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*T, uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return nil, 0, err
	}

	var tmodel = new(T)
	res, totalCnt, err := m.dbCrud.Select(ctx, *tmodel, limit, offset, filters, sorter, "")
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

	list := make([]*T, 0)

	for _, row := range res {
		var model T
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}

func (m *userRepo[T]) GetByEmail(ctx context.Context, email string) (*T, error) {
	var filters []data.Filter
	filter := data.Filter{
		FieldName: "Email",
		Compare:   1,
		Value:     email,
	}

	filters = append(filters, filter)

	var tmodel = new(T)
	res, err := m.dbCrud.SelectSingle(ctx, *tmodel, filters, "")
	if err != nil {
		return nil, err
	}

	if len(res) < 1 {
		return nil, errors.New("not found")
	}

	var model T
	mapstructure.Decode(res, &model)

	return &model, nil
}

func (m *userRepo[T]) Create(ctx context.Context, model T) (uint64, error) {
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

func (m *userRepo[T]) Update(ctx context.Context, model T) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Update(ctx, model, "", false)
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

func (m *userRepo[T]) Delete(ctx context.Context, model T) (uint64, error) {
	if err := m.dbCrud.BeginTx(ctx); err != nil {
		return 0, err
	}

	res, err := m.dbCrud.Delete(ctx, model, "", false)
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
