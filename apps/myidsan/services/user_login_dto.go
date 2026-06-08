package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// IUserLoginDtoService returns the DTO type selected by the caller while reusing
// the core user-login service behavior.
type IUserLoginDtoService[TDto any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error)
	GetByEmail(ctx context.Context, email string) (*TDto, error)
	AuthenticateDefault(ctx context.Context, username string, password string) (*TDto, error)
	RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error)
	Create(ctx context.Context, model entities.UserLogin) (uint64, error)
	Update(ctx context.Context, model entities.UserLogin) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

type userLoginDtoService[TDto any] struct {
	shared IUserLoginService
}

func NewUserLoginDtoService[TDto any](shared IUserLoginService) IUserLoginDtoService[TDto] {
	return &userLoginDtoService[TDto]{
		shared: shared,
	}
}

func (m *userLoginDtoService[TDto]) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error) {
	res, totalCnt, err := m.shared.Get(ctx, limit, offset, filters, sorters)
	return sharedservices.ProjectSliceResult[TDto](res, totalCnt, err)
}

func (m *userLoginDtoService[TDto]) GetByEmail(ctx context.Context, email string) (*TDto, error) {
	res, err := m.shared.GetByEmail(ctx, email)
	return sharedservices.ProjectOne[TDto](res, err)
}

func (m *userLoginDtoService[TDto]) AuthenticateDefault(ctx context.Context, username string, password string) (*TDto, error) {
	res, err := m.shared.AuthenticateDefault(ctx, username, password)
	return sharedservices.ProjectOne[TDto](res, err)
}

func (m *userLoginDtoService[TDto]) RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.shared.RegisterLocal(ctx, model)
}

func (m *userLoginDtoService[TDto]) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *userLoginDtoService[TDto]) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.shared.Update(ctx, model)
}

func (m *userLoginDtoService[TDto]) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.shared.Delete(ctx, id)
}
