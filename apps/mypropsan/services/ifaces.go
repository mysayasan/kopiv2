package repos

import "github.com/mysayasan/kopiv2/apps/mypropsan/models"

// IHomeService interface
type IHomeService interface {
	GetLatest() ([]*models.ResidentPropListModel, uint64, error)
}
