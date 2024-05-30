package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// userService struct
type userService struct {
	repo repos.IUserRepo
}

// Create new IUserService
func NewUserService(repo repos.IUserRepo) IUserService {
	return &userService{
		repo: repo,
	}
}

func (m *userService) GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entity.UserLoginEntity, uint64, error) {
	sorters := []data.Sorter{
		{
			FieldName: "CreatedOn",
			Sort:      2,
		},
	}

	return m.repo.GetAll(ctx, limit, offset, nil, sorters)
}

func (m *userService) GetByEmail(ctx context.Context, email string) (*entity.UserLoginEntity, error) {
	return m.repo.GetByEmail(ctx, email)
}

func (m *userService) Add(ctx context.Context, model entity.UserLoginEntity) (uint64, error) {
	return m.repo.Add(ctx, model)
}
