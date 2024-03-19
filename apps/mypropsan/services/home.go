package services

import (
	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
)

// homeService struct
type homeService struct {
	repo repos.IHomeRepo
}

// Create new IHomeService
func NewHomeService(repo repos.IHomeRepo) IHomeService {
	return &homeService{
		repo: repo,
	}
}

func (m *homeService) GetLatest(limit uint64, offset uint64) ([]*models.ResidentPropListModel, uint64, error) {
	return m.repo.GetLatest(limit, offset)
}
