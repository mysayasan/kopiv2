package services

import "github.com/mysayasan/kopiv2/apps/mypropsan/models"

// IHomeService interface
type IHomeService interface {
	GetLatest(limit uint64, offset uint64) ([]*models.ResidentPropViewModel, uint64, error)
}
