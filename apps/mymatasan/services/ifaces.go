package services

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/models"
)

// IHomeService interface
type IHomeService interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentProp, uint64, error)
}

// // ICameraService interface
// type ICameraService interface {
// 	ReadMjpeg(ctx context.Context, uri string, videoStream chan []byte) error
// }

// ICameraStreamService interface
type ICameraStreamService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.CameraStream, uint64, error)
	GetById(ctx context.Context, groupId uint64) (*entities.CameraStream, error)
	Create(ctx context.Context, model entities.CameraStream) (uint64, error)
	Update(ctx context.Context, model entities.CameraStream) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
	ReadMjpeg(ctx context.Context, uri string, vidStream chan []byte) error
}
