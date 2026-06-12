package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

type IFileStorageDtoService[TDto any, TJobDto any] interface {
	GetByGuid(ctx context.Context, guid string) (*TDto, error)
	StoreUploads(ctx context.Context, uploads []FileStorageUpload) ([]*TDto, error)
	EnqueueUploads(ctx context.Context, uploads []FileStorageUpload, idempotencyKey string) (*TJobDto, error)
	GetUploadJob(ctx context.Context, id uint64) (*TJobDto, error)
	Create(ctx context.Context, model entities.FileStorage) (uint64, error)
	CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error)
	DownloadById(ctx context.Context, id uint64, actor *FileStorageDownloadActor) (*FileStorageDownload, error)
	DownloadByIds(ctx context.Context, ids []uint64, actor *FileStorageDownloadActor) ([]*FileStorageDownload, error)
	ProcessUploadJobs(ctx context.Context, limit uint64) (uint64, error)
	RecoverStaleUploadJobs(ctx context.Context) (uint64, error)
	SweepExpiredFiles(ctx context.Context, nowUnix int64, limit uint64) (uint64, error)
}

type fileStorageDtoService[TDto any, TJobDto any] struct {
	shared IFileStorageService
}

func NewFileStorageDtoService[TDto any, TJobDto any](shared IFileStorageService) IFileStorageDtoService[TDto, TJobDto] {
	return &fileStorageDtoService[TDto, TJobDto]{shared: shared}
}

func (m *fileStorageDtoService[TDto, TJobDto]) GetByGuid(ctx context.Context, guid string) (*TDto, error) {
	res, err := m.shared.GetByGuid(ctx, guid)
	return projectOne[TDto](res, err)
}

func (m *fileStorageDtoService[TDto, TJobDto]) StoreUploads(ctx context.Context, uploads []FileStorageUpload) ([]*TDto, error) {
	res, err := m.shared.StoreUploads(ctx, uploads)
	return projectSlice[TDto](res, err)
}

func (m *fileStorageDtoService[TDto, TJobDto]) EnqueueUploads(ctx context.Context, uploads []FileStorageUpload, idempotencyKey string) (*TJobDto, error) {
	res, err := m.shared.EnqueueUploads(ctx, uploads, idempotencyKey)
	return projectOne[TJobDto](res, err)
}

func (m *fileStorageDtoService[TDto, TJobDto]) GetUploadJob(ctx context.Context, id uint64) (*TJobDto, error) {
	res, err := m.shared.GetUploadJob(ctx, id)
	return projectOne[TJobDto](res, err)
}

func (m *fileStorageDtoService[TDto, TJobDto]) Create(ctx context.Context, model entities.FileStorage) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *fileStorageDtoService[TDto, TJobDto]) CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error) {
	return m.shared.CreateMultiple(ctx, model)
}

func (m *fileStorageDtoService[TDto, TJobDto]) DownloadById(ctx context.Context, id uint64, actor *FileStorageDownloadActor) (*FileStorageDownload, error) {
	return m.shared.DownloadById(ctx, id, actor)
}

func (m *fileStorageDtoService[TDto, TJobDto]) DownloadByIds(ctx context.Context, ids []uint64, actor *FileStorageDownloadActor) ([]*FileStorageDownload, error) {
	return m.shared.DownloadByIds(ctx, ids, actor)
}

func (m *fileStorageDtoService[TDto, TJobDto]) ProcessUploadJobs(ctx context.Context, limit uint64) (uint64, error) {
	return m.shared.ProcessUploadJobs(ctx, limit)
}

func (m *fileStorageDtoService[TDto, TJobDto]) RecoverStaleUploadJobs(ctx context.Context) (uint64, error) {
	return m.shared.RecoverStaleUploadJobs(ctx)
}

func (m *fileStorageDtoService[TDto, TJobDto]) SweepExpiredFiles(ctx context.Context, nowUnix int64, limit uint64) (uint64, error) {
	return m.shared.SweepExpiredFiles(ctx, nowUnix, limit)
}
