package services

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mymatasan/models"
)

// IHomeService interface
type IHomeService interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentProp, uint64, error)
}

// ICameraService interface
type ICameraService interface {
	GetMjpegStream(ctx context.Context, videoStream chan []byte) error
}
