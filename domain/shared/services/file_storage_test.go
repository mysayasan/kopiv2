package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	filestorageenums "github.com/mysayasan/kopiv2/domain/enums/filestorage"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type fakeTxDb struct {
	insertErr      error
	inserted       bool
	committed      bool
	rolledBack     bool
	existingByGuid map[string]entities.FileStorage
}

type fakeRootDb struct {
	tx *fakeTxDb
}

type fakeOperationJobRepo struct {
	jobs      map[int64]*entities.OperationJob
	nextID    int64
	getErr    error
	getCalled int
}

type fakeFileStorageRepo struct {
	byID   map[int64]entities.FileStorage
	byGuid map[string]entities.FileStorage
}

type fakeFileStorageUserLoginRepo struct {
	byID map[int64]entities.UserLogin
}

type fakeFileStorageUserRoleRepo struct {
	byID map[int64]entities.UserRole
}

func (f *fakeRootDb) BeginScopedTx(context.Context) (dbsql.IDbCrud, error) {
	return f.tx, nil
}

func (f *fakeRootDb) Select(context.Context, interface{}, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, string, ...string) ([]map[string]interface{}, uint64, error) {
	return nil, 0, nil
}
func (f *fakeRootDb) SelectSingle(context.Context, interface{}, []sqldataenums.Filter, string) (map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeRootDb) SelectById(context.Context, interface{}, string, uint64) (map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeRootDb) SelectByUnique(context.Context, interface{}, string, string, ...any) (map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeRootDb) SelectByForeign(context.Context, interface{}, string, string, ...any) ([]map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeRootDb) Insert(context.Context, interface{}, string) (uint64, error) { return 0, nil }
func (f *fakeRootDb) UpdateById(context.Context, interface{}, string) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) UpdateByUnique(context.Context, interface{}, string, string) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) UpdateByForeign(context.Context, interface{}, string, string) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) Delete(context.Context, interface{}, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) DeleteById(context.Context, interface{}, string, uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) DeleteByUnique(context.Context, interface{}, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) DeleteByForeign(context.Context, interface{}, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeRootDb) Ping(context.Context) error    { return nil }
func (f *fakeRootDb) BeginTx(context.Context) error { return nil }
func (f *fakeRootDb) RollbackTx() error             { return nil }
func (f *fakeRootDb) CommitTx() error               { return nil }

func (f *fakeTxDb) Select(context.Context, interface{}, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, string, ...string) ([]map[string]interface{}, uint64, error) {
	return nil, 0, nil
}
func (f *fakeTxDb) SelectSingle(context.Context, interface{}, []sqldataenums.Filter, string) (map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeTxDb) SelectById(context.Context, interface{}, string, uint64) (map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeTxDb) SelectByUnique(_ context.Context, _ interface{}, _ string, keyGroup string, keys ...any) (map[string]interface{}, error) {
	if keyGroup != "guid" || len(keys) == 0 || f.existingByGuid == nil {
		return nil, errors.New("not found")
	}
	guid, _ := keys[0].(string)
	model, ok := f.existingByGuid[guid]
	if !ok {
		return nil, errors.New("not found")
	}
	return map[string]interface{}{
		"Id":          model.Id,
		"Title":       model.Title,
		"Description": model.Description,
		"Guid":        model.Guid,
		"MimeType":    model.MimeType,
		"VrPath":      model.VrPath,
		"Sha1Chksum":  model.Sha1Chksum,
		"SecurityLvl": model.SecurityLvl,
		"ExpiredAt":   model.ExpiredAt,
		"CreatedBy":   model.CreatedBy,
		"CreatedAt":   model.CreatedAt,
		"UpdatedBy":   model.UpdatedBy,
		"UpdatedAt":   model.UpdatedAt,
	}, nil
}
func (f *fakeTxDb) SelectByForeign(context.Context, interface{}, string, string, ...any) ([]map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeTxDb) Insert(context.Context, interface{}, string) (uint64, error) {
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	f.inserted = true
	return 77, nil
}
func (f *fakeTxDb) UpdateById(context.Context, interface{}, string) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) UpdateByUnique(context.Context, interface{}, string, string) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) UpdateByForeign(context.Context, interface{}, string, string) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) Delete(context.Context, interface{}, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) DeleteById(context.Context, interface{}, string, uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) DeleteByUnique(context.Context, interface{}, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) DeleteByForeign(context.Context, interface{}, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeTxDb) Ping(context.Context) error    { return nil }
func (f *fakeTxDb) BeginTx(context.Context) error { return nil }
func (f *fakeTxDb) RollbackTx() error {
	f.rolledBack = true
	return nil
}
func (f *fakeTxDb) CommitTx() error {
	f.committed = true
	return nil
}

func newFakeOperationJobRepo() *fakeOperationJobRepo {
	return &fakeOperationJobRepo{
		jobs:   map[int64]*entities.OperationJob{},
		nextID: 1,
	}
}

func newFakeFileStorageRepo(models ...entities.FileStorage) *fakeFileStorageRepo {
	repo := &fakeFileStorageRepo{
		byID:   map[int64]entities.FileStorage{},
		byGuid: map[string]entities.FileStorage{},
	}
	for _, model := range models {
		repo.byID[model.Id] = model
		repo.byGuid[model.Guid] = model
	}
	return repo
}

func (f *fakeFileStorageRepo) Get(_ context.Context, _ string, limit uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.FileStorage, uint64, error) {
	res := make([]*entities.FileStorage, 0)
	for _, model := range f.byID {
		if matchesFileFilters(model, filters) {
			clone := model
			res = append(res, &clone)
		}
	}
	total := uint64(len(res))
	if limit > 0 && uint64(len(res)) > limit {
		res = res[:limit]
	}
	if len(res) == 0 {
		return nil, 0, errors.New("select list failed: no result found")
	}
	return res, total, nil
}
func (f *fakeFileStorageRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeFileStorageRepo) GetSingle(context.Context, string, []sqldataenums.Filter) (*entities.FileStorage, error) {
	return nil, nil
}
func (f *fakeFileStorageRepo) GetById(_ context.Context, _ string, id uint64) (*entities.FileStorage, error) {
	model, ok := f.byID[int64(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &model, nil
}
func (f *fakeFileStorageRepo) GetByUnique(_ context.Context, _ string, keyGroup string, keys ...any) (*entities.FileStorage, error) {
	if keyGroup != "guid" || len(keys) == 0 {
		return nil, errors.New("not found")
	}
	guid, _ := keys[0].(string)
	model, ok := f.byGuid[guid]
	if !ok {
		return nil, errors.New("not found")
	}
	return &model, nil
}
func (f *fakeFileStorageRepo) GetByForeign(context.Context, string, string, ...any) ([]*entities.FileStorage, error) {
	return nil, nil
}
func (f *fakeFileStorageRepo) Create(context.Context, string, entities.FileStorage) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) CreateMultiple(context.Context, string, []entities.FileStorage) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) UpdateById(context.Context, string, entities.FileStorage) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) UpdateByUnique(context.Context, string, string, entities.FileStorage) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) UpdateByForeign(context.Context, string, string, entities.FileStorage) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) Delete(context.Context, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	model, ok := f.byID[int64(id)]
	if !ok {
		return 0, errors.New("not found")
	}
	delete(f.byID, int64(id))
	delete(f.byGuid, model.Guid)
	return 1, nil
}
func (f *fakeFileStorageRepo) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageRepo) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

func matchesFileFilters(model entities.FileStorage, filters []sqldataenums.Filter) bool {
	for _, filter := range filters {
		switch filter.FieldName {
		case "ExpiredAt":
			value, _ := filter.Value.(int64)
			switch filter.Compare {
			case sqldataenums.GreaterThan:
				if !(model.ExpiredAt > value) {
					return false
				}
			case sqldataenums.LessThan:
				if !(model.ExpiredAt < value) {
					return false
				}
			case sqldataenums.LessThanOrEqualTo:
				if !(model.ExpiredAt <= value) {
					return false
				}
			}
		}
	}
	return true
}

func newFakeFileStorageUserLoginRepo(users ...entities.UserLogin) *fakeFileStorageUserLoginRepo {
	repo := &fakeFileStorageUserLoginRepo{byID: map[int64]entities.UserLogin{}}
	for _, user := range users {
		repo.byID[user.Id] = user
	}
	return repo
}

func newFakeFileStorageUserRoleRepo(roles ...entities.UserRole) *fakeFileStorageUserRoleRepo {
	repo := &fakeFileStorageUserRoleRepo{byID: map[int64]entities.UserRole{}}
	for _, role := range roles {
		repo.byID[role.Id] = role
	}
	return repo
}

func (f *fakeFileStorageUserLoginRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error) {
	return nil, 0, nil
}
func (f *fakeFileStorageUserLoginRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeFileStorageUserLoginRepo) GetSingle(context.Context, string, []sqldataenums.Filter) (*entities.UserLogin, error) {
	return nil, nil
}
func (f *fakeFileStorageUserLoginRepo) GetById(_ context.Context, _ string, id uint64) (*entities.UserLogin, error) {
	user, ok := f.byID[int64(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &user, nil
}
func (f *fakeFileStorageUserLoginRepo) GetByUnique(context.Context, string, string, ...any) (*entities.UserLogin, error) {
	return nil, nil
}
func (f *fakeFileStorageUserLoginRepo) GetByForeign(context.Context, string, string, ...any) ([]*entities.UserLogin, error) {
	return nil, nil
}
func (f *fakeFileStorageUserLoginRepo) Create(context.Context, string, entities.UserLogin) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) CreateMultiple(context.Context, string, []entities.UserLogin) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) UpdateById(context.Context, string, entities.UserLogin) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) UpdateByUnique(context.Context, string, string, entities.UserLogin) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) UpdateByForeign(context.Context, string, string, entities.UserLogin) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) Delete(context.Context, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) DeleteById(context.Context, string, uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserLoginRepo) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

func (f *fakeFileStorageUserRoleRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.UserRole, uint64, error) {
	return nil, 0, nil
}
func (f *fakeFileStorageUserRoleRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeFileStorageUserRoleRepo) GetSingle(context.Context, string, []sqldataenums.Filter) (*entities.UserRole, error) {
	return nil, nil
}
func (f *fakeFileStorageUserRoleRepo) GetById(_ context.Context, _ string, id uint64) (*entities.UserRole, error) {
	role, ok := f.byID[int64(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &role, nil
}
func (f *fakeFileStorageUserRoleRepo) GetByUnique(context.Context, string, string, ...any) (*entities.UserRole, error) {
	return nil, nil
}
func (f *fakeFileStorageUserRoleRepo) GetByForeign(context.Context, string, string, ...any) ([]*entities.UserRole, error) {
	return nil, nil
}
func (f *fakeFileStorageUserRoleRepo) Create(context.Context, string, entities.UserRole) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) CreateMultiple(context.Context, string, []entities.UserRole) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) UpdateById(context.Context, string, entities.UserRole) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) UpdateByUnique(context.Context, string, string, entities.UserRole) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) UpdateByForeign(context.Context, string, string, entities.UserRole) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) Delete(context.Context, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) DeleteById(context.Context, string, uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeFileStorageUserRoleRepo) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

func (f *fakeOperationJobRepo) Get(_ context.Context, _ string, limit uint64, _ uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.OperationJob, uint64, error) {
	f.getCalled++
	if f.getErr != nil {
		return nil, 0, f.getErr
	}
	res := make([]*entities.OperationJob, 0)
	for _, job := range f.jobs {
		if matchesJobFilters(*job, filters) {
			clone := *job
			res = append(res, &clone)
		}
	}
	if len(sorters) > 0 {
		// Test data is inserted in desired FIFO order.
	}
	total := uint64(len(res))
	if limit > 0 && uint64(len(res)) > limit {
		res = res[:limit]
	}
	return res, total, nil
}
func (f *fakeOperationJobRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeOperationJobRepo) GetSingle(context.Context, string, []sqldataenums.Filter) (*entities.OperationJob, error) {
	return nil, nil
}
func (f *fakeOperationJobRepo) GetById(_ context.Context, _ string, id uint64) (*entities.OperationJob, error) {
	job := f.jobs[int64(id)]
	if job == nil {
		return nil, errors.New("not found")
	}
	clone := *job
	return &clone, nil
}
func (f *fakeOperationJobRepo) GetByUnique(_ context.Context, _ string, _ string, keys ...any) (*entities.OperationJob, error) {
	if len(keys) == 0 {
		return nil, errors.New("not found")
	}
	key, _ := keys[0].(string)
	for _, job := range f.jobs {
		if job.IdempotencyKey == key {
			clone := *job
			return &clone, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeOperationJobRepo) GetByForeign(context.Context, string, string, ...any) ([]*entities.OperationJob, error) {
	return nil, nil
}
func (f *fakeOperationJobRepo) Create(_ context.Context, _ string, model entities.OperationJob) (uint64, error) {
	model.Id = f.nextID
	f.nextID++
	clone := model
	f.jobs[model.Id] = &clone
	return uint64(model.Id), nil
}
func (f *fakeOperationJobRepo) CreateMultiple(context.Context, string, []entities.OperationJob) (uint64, error) {
	return 0, nil
}
func (f *fakeOperationJobRepo) UpdateById(_ context.Context, _ string, model entities.OperationJob) (uint64, error) {
	clone := model
	f.jobs[model.Id] = &clone
	return 1, nil
}
func (f *fakeOperationJobRepo) UpdateByUnique(context.Context, string, string, entities.OperationJob) (uint64, error) {
	return 0, nil
}
func (f *fakeOperationJobRepo) UpdateByForeign(context.Context, string, string, entities.OperationJob) (uint64, error) {
	return 0, nil
}
func (f *fakeOperationJobRepo) Delete(context.Context, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeOperationJobRepo) DeleteById(context.Context, string, uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeOperationJobRepo) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeOperationJobRepo) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

func matchesJobFilters(job entities.OperationJob, filters []sqldataenums.Filter) bool {
	for _, filter := range filters {
		switch filter.FieldName {
		case "Type":
			if job.Type != filter.Value {
				return false
			}
		case "Status":
			if job.Status != filter.Value {
				return false
			}
		case "DeadlineAt":
			value, _ := filter.Value.(int64)
			if filter.Compare == sqldataenums.LessThan && !(job.DeadlineAt < value) {
				return false
			}
		}
	}
	return true
}

func TestFileStorageStoreUploadsMovesFileAndCommits(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "staged")
	finalPath := filepath.Join(dir, "final")
	if err := os.WriteFile(tempPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	tx := &fakeTxDb{}
	service := NewFileStorageService(nil, nil,
		WithFileStorageTransaction(&fakeRootDb{tx: tx}),
		WithFileStorageOperationTimeout(time.Second),
	)

	res, err := service.StoreUploads(context.Background(), []FileStorageUpload{{
		Model:     entities.FileStorage{Guid: "guid"},
		TempPath:  tempPath,
		FinalPath: finalPath,
	}})
	if err != nil {
		t.Fatalf("store uploads failed: %v", err)
	}
	if len(res) != 1 || res[0].Id != 77 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !tx.inserted || !tx.committed || tx.rolledBack {
		t.Fatalf("unexpected tx state inserted=%t committed=%t rolledBack=%t", tx.inserted, tx.committed, tx.rolledBack)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file should be moved, err=%v", err)
	}
}

func TestFileStorageStoreUploadsRollsBackAndCleansTempOnInsertFailure(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "staged")
	finalPath := filepath.Join(dir, "final")
	if err := os.WriteFile(tempPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	tx := &fakeTxDb{insertErr: errors.New("insert failed")}
	service := NewFileStorageService(nil, nil,
		WithFileStorageTransaction(&fakeRootDb{tx: tx}),
		WithFileStorageOperationTimeout(time.Second),
	)

	_, err := service.StoreUploads(context.Background(), []FileStorageUpload{{
		Model:     entities.FileStorage{Guid: "guid"},
		TempPath:  tempPath,
		FinalPath: finalPath,
	}})
	if err == nil {
		t.Fatal("expected insert failure")
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("unexpected tx state committed=%t rolledBack=%t", tx.committed, tx.rolledBack)
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file should be removed, err=%v", err)
	}
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final file should not exist, err=%v", err)
	}
}

func TestFileStorageStoreUploadsSkipsExistingCommittedGuid(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "staged")
	finalPath := filepath.Join(dir, "final")
	if err := os.WriteFile(tempPath, []byte("retry-content"), 0644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}
	if err := os.WriteFile(finalPath, []byte("committed-content"), 0644); err != nil {
		t.Fatalf("write final file: %v", err)
	}

	tx := &fakeTxDb{
		existingByGuid: map[string]entities.FileStorage{
			"guid": {Id: 42, Guid: "guid"},
		},
	}
	service := NewFileStorageService(nil, nil,
		WithFileStorageTransaction(&fakeRootDb{tx: tx}),
		WithFileStorageOperationTimeout(time.Second),
	)

	res, err := service.StoreUploads(context.Background(), []FileStorageUpload{{
		Model:     entities.FileStorage{Guid: "guid"},
		TempPath:  tempPath,
		FinalPath: finalPath,
	}})
	if err != nil {
		t.Fatalf("store uploads failed: %v", err)
	}
	if len(res) != 1 || res[0].Id != 42 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if tx.inserted {
		t.Fatal("existing guid should not insert duplicate metadata")
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("unexpected tx state committed=%t rolledBack=%t", tx.committed, tx.rolledBack)
	}
	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(content) != "committed-content" {
		t.Fatalf("final file should not be overwritten, got %q", content)
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file should be removed after success, err=%v", err)
	}
}

func TestFileStorageDownloadByIdReadsStoredFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-1"), []byte("download-content"), 0644); err != nil {
		t.Fatalf("write stored file: %v", err)
	}

	service := NewFileStorageService(newFakeFileStorageRepo(entities.FileStorage{
		Id:          1,
		Title:       "receipt.png",
		Guid:        "guid-1",
		MimeType:    "image/png",
		SecurityLvl: int32(filestorageenums.Public),
	}), nil, WithFileStoragePath(dir))

	download, err := service.DownloadById(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("download by id failed: %v", err)
	}
	if download.Filename != "receipt.png" || download.MimeType != "image/png" {
		t.Fatalf("unexpected download metadata: %+v", download)
	}
	if string(download.Content) != "download-content" {
		t.Fatalf("unexpected content: %q", download.Content)
	}
}

func TestFileStorageDownloadByIdsPreservesRequestedOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-1"), []byte("one"), 0644); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guid-2"), []byte("two"), 0644); err != nil {
		t.Fatalf("write second file: %v", err)
	}

	service := NewFileStorageService(newFakeFileStorageRepo(
		entities.FileStorage{Id: 1, Title: "one.txt", Guid: "guid-1", MimeType: "text/plain", SecurityLvl: int32(filestorageenums.Public)},
		entities.FileStorage{Id: 2, Title: "two.txt", Guid: "guid-2", MimeType: "text/plain", SecurityLvl: int32(filestorageenums.Public)},
	), nil, WithFileStoragePath(dir))

	downloads, err := service.DownloadByIds(context.Background(), []uint64{2, 1}, nil)
	if err != nil {
		t.Fatalf("download by ids failed: %v", err)
	}
	if len(downloads) != 2 {
		t.Fatalf("expected two downloads, got %d", len(downloads))
	}
	if downloads[0].Model.Id != 2 || string(downloads[0].Content) != "two" {
		t.Fatalf("unexpected first download: %+v", downloads[0])
	}
	if downloads[1].Model.Id != 1 || string(downloads[1].Content) != "one" {
		t.Fatalf("unexpected second download: %+v", downloads[1])
	}
}

func TestFileStorageDownloadSystemOnlyRequiresSystemActor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-system"), []byte("system"), 0644); err != nil {
		t.Fatalf("write stored file: %v", err)
	}

	service := NewFileStorageService(newFakeFileStorageRepo(entities.FileStorage{
		Id:          1,
		Title:       "system.txt",
		Guid:        "guid-system",
		MimeType:    "text/plain",
		SecurityLvl: int32(filestorageenums.SystemOnly),
	}), nil, WithFileStoragePath(dir))

	if _, err := service.DownloadById(context.Background(), 1, nil); err == nil {
		t.Fatal("expected public API actor to be rejected")
	}

	download, err := service.DownloadById(context.Background(), 1, &FileStorageDownloadActor{IsSystem: true})
	if err != nil {
		t.Fatalf("system actor should download: %v", err)
	}
	if string(download.Content) != "system" {
		t.Fatalf("unexpected content: %q", download.Content)
	}
}

func TestFileStorageDownloadGroupAllowsActorInOwnerGroup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-group"), []byte("group"), 0644); err != nil {
		t.Fatalf("write stored file: %v", err)
	}

	service := NewFileStorageService(
		newFakeFileStorageRepo(entities.FileStorage{
			Id:          1,
			Title:       "group.txt",
			Guid:        "guid-group",
			MimeType:    "text/plain",
			SecurityLvl: int32(filestorageenums.Group),
			CreatedBy:   10,
		}),
		nil,
		WithFileStoragePath(dir),
		WithFileStorageUserRepo(newFakeFileStorageUserLoginRepo(
			entities.UserLogin{Id: 10, UserRoleId: 100},
		)),
		WithFileStorageRoleRepo(newFakeFileStorageUserRoleRepo(
			entities.UserRole{Id: 100, GroupId: 7},
			entities.UserRole{Id: 101, GroupId: 7},
			entities.UserRole{Id: 200, GroupId: 8},
		)),
	)

	if _, err := service.DownloadById(context.Background(), 1, &FileStorageDownloadActor{UserId: 20, RoleId: 200}); err == nil {
		t.Fatal("expected different group to be rejected")
	}

	download, err := service.DownloadById(context.Background(), 1, &FileStorageDownloadActor{UserId: 21, RoleId: 101})
	if err != nil {
		t.Fatalf("same group actor should download: %v", err)
	}
	if string(download.Content) != "group" {
		t.Fatalf("unexpected content: %q", download.Content)
	}
}

func TestFileStorageDownloadRoleAllowsOwnerRoleAncestors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-role"), []byte("role"), 0644); err != nil {
		t.Fatalf("write stored file: %v", err)
	}

	service := NewFileStorageService(
		newFakeFileStorageRepo(entities.FileStorage{
			Id:          1,
			Title:       "role.txt",
			Guid:        "guid-role",
			MimeType:    "text/plain",
			SecurityLvl: int32(filestorageenums.Role),
			CreatedBy:   10,
		}),
		nil,
		WithFileStoragePath(dir),
		WithFileStorageUserRepo(newFakeFileStorageUserLoginRepo(
			entities.UserLogin{Id: 10, UserRoleId: 300},
		)),
		WithFileStorageRoleRepo(newFakeFileStorageUserRoleRepo(
			entities.UserRole{Id: 100, ParentId: 0, GroupId: 7},
			entities.UserRole{Id: 200, ParentId: 100, GroupId: 7},
			entities.UserRole{Id: 300, ParentId: 200, GroupId: 7},
			entities.UserRole{Id: 400, ParentId: 0, GroupId: 7},
		)),
	)

	if _, err := service.DownloadById(context.Background(), 1, &FileStorageDownloadActor{UserId: 20, RoleId: 400}); err == nil {
		t.Fatal("expected unrelated role to be rejected")
	}

	download, err := service.DownloadById(context.Background(), 1, &FileStorageDownloadActor{UserId: 21, RoleId: 200})
	if err != nil {
		t.Fatalf("ancestor role actor should download: %v", err)
	}
	if string(download.Content) != "role" {
		t.Fatalf("unexpected content: %q", download.Content)
	}
}

func TestFileStorageDownloadRejectsExpiredFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-expired"), []byte("expired"), 0644); err != nil {
		t.Fatalf("write stored file: %v", err)
	}

	service := NewFileStorageService(newFakeFileStorageRepo(entities.FileStorage{
		Id:          1,
		Title:       "expired.txt",
		Guid:        "guid-expired",
		MimeType:    "text/plain",
		SecurityLvl: int32(filestorageenums.Public),
		ExpiredAt:   time.Now().UTC().Add(-time.Second).Unix(),
	}), nil, WithFileStoragePath(dir))

	if _, err := service.DownloadById(context.Background(), 1, nil); err == nil {
		t.Fatal("expected expired file to be rejected")
	}
}

func TestFileStorageSweepExpiredFilesRemovesFileAndMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guid-expired"), []byte("expired"), 0644); err != nil {
		t.Fatalf("write expired file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guid-active"), []byte("active"), 0644); err != nil {
		t.Fatalf("write active file: %v", err)
	}
	now := time.Now().UTC().Unix()
	repo := newFakeFileStorageRepo(
		entities.FileStorage{Id: 1, Guid: "guid-expired", ExpiredAt: now},
		entities.FileStorage{Id: 2, Guid: "guid-active", ExpiredAt: now + 60},
		entities.FileStorage{Id: 3, Guid: "guid-never", ExpiredAt: 0},
	)
	service := NewFileStorageService(repo, nil, WithFileStoragePath(dir))

	deleted, err := service.SweepExpiredFiles(context.Background(), now, 100)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one expired file deleted, got %d", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "guid-expired")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired physical file should be gone, err=%v", err)
	}
	if _, ok := repo.byID[1]; ok {
		t.Fatal("expired metadata should be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "guid-active")); err != nil {
		t.Fatalf("active physical file should remain: %v", err)
	}
	if _, ok := repo.byID[2]; !ok {
		t.Fatal("active metadata should remain")
	}
}

func TestFileStorageAsyncJobProcessesAndCleansStagedFile(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "staged")
	finalPath := filepath.Join(dir, "final")
	if err := os.WriteFile(tempPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	tx := &fakeTxDb{}
	jobRepo := newFakeOperationJobRepo()
	service := NewFileStorageService(nil, nil,
		WithFileStorageJobRepo(jobRepo),
		WithFileStorageTransaction(&fakeRootDb{tx: tx}),
		WithFileStorageOperationTimeout(time.Second),
		WithFileStorageMaxAttempts(3),
	)

	job, err := service.EnqueueUploads(context.Background(), []FileStorageUpload{{
		Model:     entities.FileStorage{Guid: "guid"},
		TempPath:  tempPath,
		FinalPath: finalPath,
	}}, "idem-1")
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if job.Status != jobStatusQueued {
		t.Fatalf("unexpected initial status: %+v", job)
	}

	processed, err := service.ProcessUploadJobs(context.Background(), 10)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed job, got %d", processed)
	}

	done := jobRepo.jobs[job.Id]
	if done.Status != jobStatusSucceeded || done.Result == "" {
		t.Fatalf("unexpected completed job: %+v", done)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file should be removed after success, err=%v", err)
	}
}

func TestFileStorageAsyncJobRetriesWithoutDeletingStagedFile(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "staged")
	finalPath := filepath.Join(dir, "final")
	if err := os.WriteFile(tempPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	tx := &fakeTxDb{insertErr: errors.New("insert failed")}
	jobRepo := newFakeOperationJobRepo()
	service := NewFileStorageService(nil, nil,
		WithFileStorageJobRepo(jobRepo),
		WithFileStorageTransaction(&fakeRootDb{tx: tx}),
		WithFileStorageOperationTimeout(time.Second),
		WithFileStorageMaxAttempts(2),
	)

	job, err := service.EnqueueUploads(context.Background(), []FileStorageUpload{{
		Model:     entities.FileStorage{Guid: "guid"},
		TempPath:  tempPath,
		FinalPath: finalPath,
	}}, "idem-2")
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	_, err = service.ProcessUploadJobs(context.Background(), 10)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	retrying := jobRepo.jobs[job.Id]
	if retrying.Status != jobStatusRetrying {
		t.Fatalf("expected retrying status, got %+v", retrying)
	}
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatalf("staged file should remain for retry: %v", err)
	}
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final file should not exist, err=%v", err)
	}
}

func TestFileStorageUploadWorkerIgnoresEmptyJobLists(t *testing.T) {
	jobRepo := newFakeOperationJobRepo()
	jobRepo.getErr = errors.New("select list failed: no result found")
	service := NewFileStorageService(nil, nil,
		WithFileStorageJobRepo(jobRepo),
	)

	recovered, err := service.RecoverStaleUploadJobs(context.Background())
	if err != nil {
		t.Fatalf("recover stale should ignore empty list: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("expected zero recovered jobs, got %d", recovered)
	}

	processed, err := service.ProcessUploadJobs(context.Background(), 10)
	if err != nil {
		t.Fatalf("process jobs should ignore empty list: %v", err)
	}
	if processed != 0 {
		t.Fatalf("expected zero processed jobs, got %d", processed)
	}
}
