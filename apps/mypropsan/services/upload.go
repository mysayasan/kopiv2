package services

import (
	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
)

// uploadService struct
type uploadService struct {
	repo repos.IUploadRepo
}

// Create new IUploadService
func NewUploadService(repo repos.IUploadRepo) IUploadService {
	return &uploadService{
		repo: repo,
	}
}

func (m *uploadService) GetByGuid(guid string) (*entity.UploadEntity, error) {
	return m.repo.GetByGuid(guid)
}
