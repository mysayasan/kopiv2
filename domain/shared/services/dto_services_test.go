package services

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	applog "github.com/mysayasan/kopiv2/infra/logging"
)

type sharedAdapterDTO struct {
	Id         int64                     `json:"id"`
	Title      string                    `json:"title"`
	Email      string                    `json:"email"`
	Path       string                    `json:"path"`
	Host       string                    `json:"host"`
	AccessTier apiaccessenums.AccessTier `json:"accessTier"`
	Guid       string                    `json:"guid"`
	Type       string                    `json:"type"`
	Message    string                    `json:"message"`
}

type fakeApiEndpointCoreService struct {
	endpoints []*entities.ApiEndpoint
}

func (m *fakeApiEndpointCoreService) Get(context.Context, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.ApiEndpoint, uint64, error) {
	return m.endpoints, uint64(len(m.endpoints)), nil
}

func (m *fakeApiEndpointCoreService) Create(context.Context, entities.ApiEndpoint) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointCoreService) Update(context.Context, entities.ApiEndpoint) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointCoreService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func TestApiEndpointDtoServiceGetReturnsSuppliedDTO(t *testing.T) {
	service := NewApiEndpointDtoService[sharedAdapterDTO](&fakeApiEndpointCoreService{
		endpoints: []*entities.ApiEndpoint{{Id: 3, Title: "health", Host: "*", Path: "/api/health", AccessTier: apiaccessenums.Public}},
	})

	res, totalCnt, err := service.Get(context.Background(), 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 || res[0].Path != "/api/health" || res[0].AccessTier != apiaccessenums.Public {
		t.Fatalf("unexpected dto result total=%d res=%+v", totalCnt, res)
	}
}

type fakeApiEndpointRbacCoreService struct {
	rbac     []*entities.ApiEndpointRbac
	rbacList []*entities.ApiEndpointRbacListModel
	join     []*entities.ApiEndpointRbacJoinModel
}

func (m *fakeApiEndpointRbacCoreService) Get(context.Context, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.ApiEndpointRbac, uint64, error) {
	return m.rbac, uint64(len(m.rbac)), nil
}

func (m *fakeApiEndpointRbacCoreService) GetList(context.Context, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.ApiEndpointRbacListModel, uint64, error) {
	return m.rbacList, uint64(len(m.rbacList)), nil
}

func (m *fakeApiEndpointRbacCoreService) GetApiEpByUserRole(context.Context, uint64) ([]*entities.ApiEndpointRbacJoinModel, uint64, error) {
	return m.join, uint64(len(m.join)), nil
}

func (m *fakeApiEndpointRbacCoreService) Create(context.Context, entities.ApiEndpointRbac) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointRbacCoreService) Update(context.Context, entities.ApiEndpointRbac) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointRbacCoreService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointRbacCoreService) Validate(context.Context, string, string, uint64) (*entities.ApiEndpointRbac, error) {
	if len(m.rbac) == 0 {
		return nil, nil
	}
	return m.rbac[0], nil
}

func TestApiEndpointRbacDtoServiceGetReturnsSuppliedDTOs(t *testing.T) {
	service := NewApiEndpointRbacDtoService[sharedAdapterDTO, sharedAdapterDTO, sharedAdapterDTO](&fakeApiEndpointRbacCoreService{
		rbac:     []*entities.ApiEndpointRbac{{Id: 4, ApiEndpointId: 3}},
		rbacList: []*entities.ApiEndpointRbacListModel{{Id: 4, ApiEndpointId: 3, EndpointPath: "/api/home"}},
		join:     []*entities.ApiEndpointRbacJoinModel{{Id: 4, Host: "*", Path: "/api/home"}},
	})

	res, totalCnt, err := service.Get(context.Background(), 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 || res[0].Id != 4 {
		t.Fatalf("unexpected dto result total=%d res=%+v", totalCnt, res)
	}

	join, joinTotal, err := service.GetApiEpByUserRole(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetApiEpByUserRole failed: %v", err)
	}
	if joinTotal != 1 || len(join) != 1 || join[0].Path != "/api/home" {
		t.Fatalf("unexpected join dto result total=%d res=%+v", joinTotal, join)
	}
}

type fakeApiLogCoreService struct {
	logs []*entities.ApiLog
}

func (m *fakeApiLogCoreService) Get(context.Context, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.ApiLog, uint64, error) {
	return m.logs, uint64(len(m.logs)), nil
}

func (m *fakeApiLogCoreService) Create(context.Context, entities.ApiLog) (uint64, error) {
	return 0, nil
}

func (m *fakeApiLogCoreService) DeleteByMonth(context.Context, int, int) (uint64, error) {
	return 0, nil
}

func (m *fakeApiLogCoreService) DeleteOlderThan(context.Context, int) (uint64, error) {
	return 0, nil
}

func TestApiLogDtoServiceGetReturnsSuppliedDTO(t *testing.T) {
	service := NewApiLogDtoService[sharedAdapterDTO](&fakeApiLogCoreService{
		logs: []*entities.ApiLog{{Id: 5, LogMsg: "ok"}},
	})

	res, totalCnt, err := service.Get(context.Background(), 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 || res[0].Id != 5 {
		t.Fatalf("unexpected dto result total=%d res=%+v", totalCnt, res)
	}
}

type fakeFileStorageCoreService struct {
	files []*entities.FileStorage
	job   *entities.OperationJob
}

func (m *fakeFileStorageCoreService) GetByGuid(context.Context, string) (*entities.FileStorage, error) {
	if len(m.files) == 0 {
		return nil, nil
	}
	return m.files[0], nil
}

func (m *fakeFileStorageCoreService) Create(context.Context, entities.FileStorage) (uint64, error) {
	return 0, nil
}

func (m *fakeFileStorageCoreService) CreateMultiple(context.Context, []entities.FileStorage) (uint64, error) {
	return 0, nil
}

func (m *fakeFileStorageCoreService) DownloadById(context.Context, uint64, *FileStorageDownloadActor) (*FileStorageDownload, error) {
	return nil, nil
}

func (m *fakeFileStorageCoreService) DownloadByIds(context.Context, []uint64, *FileStorageDownloadActor) ([]*FileStorageDownload, error) {
	return nil, nil
}

func (m *fakeFileStorageCoreService) StoreUploads(context.Context, []FileStorageUpload) ([]*entities.FileStorage, error) {
	return m.files, nil
}

func (m *fakeFileStorageCoreService) EnqueueUploads(context.Context, []FileStorageUpload, string) (*entities.OperationJob, error) {
	return m.job, nil
}

func (m *fakeFileStorageCoreService) GetUploadJob(context.Context, uint64) (*entities.OperationJob, error) {
	return m.job, nil
}

func (m *fakeFileStorageCoreService) ProcessUploadJobs(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func (m *fakeFileStorageCoreService) RecoverStaleUploadJobs(context.Context) (uint64, error) {
	return 0, nil
}

func (m *fakeFileStorageCoreService) SweepExpiredFiles(context.Context, int64, uint64) (uint64, error) {
	return 0, nil
}

func TestFileStorageDtoServiceReturnsSuppliedDTOs(t *testing.T) {
	service := NewFileStorageDtoService[sharedAdapterDTO, sharedAdapterDTO](&fakeFileStorageCoreService{
		files: []*entities.FileStorage{{Id: 6, Guid: "abc"}},
		job:   &entities.OperationJob{Id: 7, Type: "file-upload"},
	})

	file, err := service.GetByGuid(context.Background(), "abc")
	if err != nil {
		t.Fatalf("GetByGuid failed: %v", err)
	}
	if file.Guid != "abc" {
		t.Fatalf("unexpected file dto: %+v", file)
	}

	job, err := service.GetUploadJob(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetUploadJob failed: %v", err)
	}
	if job.Type != "file-upload" {
		t.Fatalf("unexpected job dto: %+v", job)
	}
}

type fakeRuntimeLogCoreService struct {
	entries []applog.Entry
}

func (m *fakeRuntimeLogCoreService) List(context.Context, uint64, uint64) ([]applog.Entry, uint64, error) {
	return m.entries, uint64(len(m.entries)), nil
}

func (m *fakeRuntimeLogCoreService) DeleteByMonth(context.Context, int, int) (uint64, error) {
	return 0, nil
}

func (m *fakeRuntimeLogCoreService) DeleteOlderThan(context.Context, int) (uint64, error) {
	return 0, nil
}

func TestRuntimeLogDtoServiceListReturnsSuppliedDTO(t *testing.T) {
	service := NewRuntimeLogDtoService[sharedAdapterDTO](&fakeRuntimeLogCoreService{
		entries: []applog.Entry{{Message: "hello"}},
	})

	res, totalCnt, err := service.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 || res[0].Message != "hello" {
		t.Fatalf("unexpected dto result total=%d res=%+v", totalCnt, res)
	}
}
