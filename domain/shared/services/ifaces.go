package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	applog "github.com/mysayasan/kopiv2/infra/logging"
)

// IApiEndpointService interface
type IApiEndpointService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiEndpoint, uint64, error)
	Create(ctx context.Context, model entities.ApiEndpoint) (uint64, error)
	Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IAppRegistryService interface
type IAppRegistryService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.AppRegistry, uint64, error)
	Create(ctx context.Context, model entities.AppRegistry) (uint64, error)
	Update(ctx context.Context, model entities.AppRegistry) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IApiEndpointRbacService interface
type IApiEndpointRbacService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiEndpointRbac, uint64, error)
	GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*entities.ApiEndpointRbacJoinModel, uint64, error)
	Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error)
	Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
	Validate(ctx context.Context, host string, path string, userRoleId uint64) (*entities.ApiEndpointRbac, error)
}

// IApiLogService interface
type IApiLogService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiLog, uint64, error)
	Create(ctx context.Context, model entities.ApiLog) (uint64, error)
	DeleteByMonth(ctx context.Context, year int, month int) (uint64, error)
	DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error)
}

// IFileStorageService interface
type IFileStorageService interface {
	GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error)
	Create(ctx context.Context, model entities.FileStorage) (uint64, error)
	CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error)
	DownloadById(ctx context.Context, id uint64, actor *FileStorageDownloadActor) (*FileStorageDownload, error)
	DownloadByIds(ctx context.Context, ids []uint64, actor *FileStorageDownloadActor) ([]*FileStorageDownload, error)
	StoreUploads(ctx context.Context, uploads []FileStorageUpload) ([]*entities.FileStorage, error)
	EnqueueUploads(ctx context.Context, uploads []FileStorageUpload, idempotencyKey string) (*entities.OperationJob, error)
	GetUploadJob(ctx context.Context, id uint64) (*entities.OperationJob, error)
	ProcessUploadJobs(ctx context.Context, limit uint64) (uint64, error)
	RecoverStaleUploadJobs(ctx context.Context) (uint64, error)
	SweepExpiredFiles(ctx context.Context, nowUnix int64, limit uint64) (uint64, error)
}

// FileStorageUpload is one staged file ready for metadata insert and final move.
type FileStorageUpload struct {
	Model     entities.FileStorage
	TempPath  string
	FinalPath string
}

// FileStorageDownload is one stored file ready to stream to a client.
type FileStorageDownload struct {
	Model    entities.FileStorage
	Filename string
	MimeType string
	Content  []byte
}

// FileStorageDownloadActor is the caller identity used for file access checks.
type FileStorageDownloadActor struct {
	UserId   int64
	RoleId   int64
	IsSystem bool
}

// ICacheService interface
type ICacheService interface {
	ListKeys(ctx context.Context, prefix string, limit uint64, offset uint64) ([]string, uint64, error)
	WipeByPrefix(ctx context.Context, prefix string) (bool, error)
	WipeByKey(ctx context.Context, key string) (bool, error)
	Ping(ctx context.Context) (bool, error)
}

// IRuntimeLogService interface
type IRuntimeLogService interface {
	List(ctx context.Context, limit uint64, offset uint64) ([]applog.Entry, uint64, error)
	DeleteByMonth(ctx context.Context, year int, month int) (uint64, error)
	DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error)
}
