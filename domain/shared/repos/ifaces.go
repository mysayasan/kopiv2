package repos

import (
	"context"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// IGenericRepo interface
type IGenericRepo[T any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter) ([]*T, uint64, error)
	GetByIds(ctx context.Context, ids ...any) (*T, error)
	GetByUids(ctx context.Context, uids ...any) (*T, error)
	Create(ctx context.Context, model T) (uint64, error)
	Update(ctx context.Context, model T) (uint64, error)
	Delete(ctx context.Context, model T) (uint64, error)
}
