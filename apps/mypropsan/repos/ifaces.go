package repos

import "github.com/mysayasan/kopiv2/apps/mypropsan/models"

// IIHomeRepo interface
type IHomeRepo interface {
	GetLatest() ([]*models.ResidentPropListModel, uint64, error)
}
