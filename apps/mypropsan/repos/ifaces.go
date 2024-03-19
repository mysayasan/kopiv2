package repos

import "github.com/mysayasan/kopiv2/apps/mypropsan/models"

// IIHomeRepo interface
type IHomeRepo interface {
	GetLatest(limit uint64, offset uint64) ([]*models.ResidentPropListModel, uint64, error)
}
