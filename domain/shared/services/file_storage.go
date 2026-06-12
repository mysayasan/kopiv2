package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	filestorageenums "github.com/mysayasan/kopiv2/domain/enums/filestorage"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/coordination"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const (
	operationTypeFileStorageUpload = "file-storage-upload"
	jobStatusQueued                = "queued"
	jobStatusRunning               = "running"
	jobStatusSucceeded             = "succeeded"
	jobStatusFailed                = "failed"
	jobStatusRetrying              = "retrying"
)

// fileStorageService struct
type fileStorageService struct {
	repo             dbsql.IGenericRepo[entities.FileStorage]
	jobRepo          dbsql.IGenericRepo[entities.OperationJob]
	userRepo         dbsql.IGenericRepo[entities.UserLogin]
	roleRepo         dbsql.IGenericRepo[entities.UserRole]
	cache            cache.Store
	db               dbsql.IDbCrud
	locker           coordination.Locker
	storagePath      string
	operationTimeout time.Duration
	maxAttempts      int64
}

type FileStorageServiceOption func(*fileStorageService)

// Create new IFileStorageService
func NewFileStorageService(
	repo dbsql.IGenericRepo[entities.FileStorage],
	cacheStore cache.Store,
	options ...FileStorageServiceOption,
) IFileStorageService {
	service := &fileStorageService{
		repo:  repo,
		cache: cacheStore,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithFileStorageJobRepo(repo dbsql.IGenericRepo[entities.OperationJob]) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.jobRepo = repo
	}
}

func WithFileStorageUserRepo(repo dbsql.IGenericRepo[entities.UserLogin]) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.userRepo = repo
	}
}

func WithFileStorageRoleRepo(repo dbsql.IGenericRepo[entities.UserRole]) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.roleRepo = repo
	}
}

func WithFileStorageTransaction(db dbsql.IDbCrud) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.db = db
	}
}

func WithFileStorageLocker(locker coordination.Locker) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.locker = locker
	}
}

func WithFileStorageOperationTimeout(timeout time.Duration) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.operationTimeout = timeout
	}
}

func WithFileStorageMaxAttempts(maxAttempts int64) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.maxAttempts = maxAttempts
	}
}

func WithFileStoragePath(path string) FileStorageServiceOption {
	return func(service *fileStorageService) {
		service.storagePath = path
	}
}

func (m *fileStorageService) GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error) {
	return m.repo.GetByUnique(ctx, "", "guid", guid)
}

func (m *fileStorageService) Create(ctx context.Context, model entities.FileStorage) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *fileStorageService) CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error) {
	return m.repo.CreateMultiple(ctx, "", model)
}

func (m *fileStorageService) DownloadById(ctx context.Context, id uint64, actor *FileStorageDownloadActor) (*FileStorageDownload, error) {
	model, err := m.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if err := m.authorizeDownload(ctx, *model, actor); err != nil {
		return nil, err
	}
	return m.downloadModel(*model)
}

func (m *fileStorageService) DownloadByIds(ctx context.Context, ids []uint64, actor *FileStorageDownloadActor) ([]*FileStorageDownload, error) {
	if len(ids) == 0 {
		return nil, errors.New("ids is required")
	}

	downloads := make([]*FileStorageDownload, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, errors.New("valid ids are required")
		}
		download, err := m.DownloadById(ctx, id, actor)
		if err != nil {
			return nil, fmt.Errorf("download file id %d: %w", id, err)
		}
		downloads = append(downloads, download)
	}
	return downloads, nil
}

func (m *fileStorageService) StoreUploads(ctx context.Context, uploads []FileStorageUpload) ([]*entities.FileStorage, error) {
	return m.storeUploads(ctx, uploads, true)
}

func (m *fileStorageService) EnqueueUploads(ctx context.Context, uploads []FileStorageUpload, idempotencyKey string) (*entities.OperationJob, error) {
	if len(uploads) == 0 {
		return nil, errors.New("uploads is required")
	}
	if m.jobRepo == nil {
		return nil, errors.New("operation job repository is not configured")
	}

	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	if existing, err := m.jobRepo.GetByUnique(ctx, "", "idempotency", idempotencyKey); err == nil && existing != nil && existing.Id > 0 {
		return existing, nil
	}

	payload, err := encodeJobPayload(uploads)
	if err != nil {
		return nil, fmt.Errorf("encode upload job payload: %w", err)
	}

	now := time.Now().UTC().Unix()
	maxAttempts := m.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	job := entities.OperationJob{
		Type:           operationTypeFileStorageUpload,
		ResourceKey:    "file-storage",
		IdempotencyKey: idempotencyKey,
		Status:         jobStatusQueued,
		Attempt:        0,
		MaxAttempts:    maxAttempts,
		Payload:        payload,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	id, err := m.jobRepo.Create(ctx, "", job)
	if err != nil {
		return nil, fmt.Errorf("create upload job: %w", err)
	}
	job.Id = int64(id)
	return &job, nil
}

func (m *fileStorageService) GetUploadJob(ctx context.Context, id uint64) (*entities.OperationJob, error) {
	if m.jobRepo == nil {
		return nil, errors.New("operation job repository is not configured")
	}
	return m.jobRepo.GetById(ctx, "", id)
}

func (m *fileStorageService) ProcessUploadJobs(ctx context.Context, limit uint64) (uint64, error) {
	if m.jobRepo == nil {
		return 0, errors.New("operation job repository is not configured")
	}
	if limit == 0 {
		limit = 10
	}

	total := uint64(0)
	for _, status := range []string{jobStatusQueued, jobStatusRetrying} {
		jobs, _, err := m.jobRepo.Get(ctx, "", limit, 0, []sqldataenums.Filter{
			{FieldName: "Type", Compare: sqldataenums.Equal, Value: operationTypeFileStorageUpload},
			{FieldName: "Status", Compare: sqldataenums.Equal, Value: status},
		}, []sqldataenums.Sorter{
			{FieldName: "CreatedAt", Sort: 1},
			{FieldName: "Id", Sort: 1},
		})
		if err != nil {
			if isRepoNotFoundError(err) {
				continue
			}
			return total, fmt.Errorf("list %s upload jobs: %w", status, err)
		}

		for _, job := range jobs {
			if job == nil {
				continue
			}
			if err := m.processUploadJob(ctx, *job); err != nil {
				return total, err
			}
			total++
			if total >= limit {
				return total, nil
			}
		}
		if total > 0 {
			return total, nil
		}
	}

	return total, nil
}

func (m *fileStorageService) RecoverStaleUploadJobs(ctx context.Context) (uint64, error) {
	if m.jobRepo == nil {
		return 0, errors.New("operation job repository is not configured")
	}

	now := time.Now().UTC().Unix()
	jobs, _, err := m.jobRepo.Get(ctx, "", 100, 0, []sqldataenums.Filter{
		{FieldName: "Type", Compare: sqldataenums.Equal, Value: operationTypeFileStorageUpload},
		{FieldName: "Status", Compare: sqldataenums.Equal, Value: jobStatusRunning},
		{FieldName: "DeadlineAt", Compare: sqldataenums.LessThan, Value: now},
	}, []sqldataenums.Sorter{
		{FieldName: "DeadlineAt", Sort: 1},
	})
	if err != nil {
		if isRepoNotFoundError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("list stale upload jobs: %w", err)
	}

	recovered := uint64(0)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		maxAttempts := job.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
		job.LockedBy = ""
		job.StartedAt = 0
		job.DeadlineAt = 0
		job.UpdatedAt = now
		if job.Attempt >= maxAttempts {
			job.Status = jobStatusFailed
			job.LastError = "operation deadline exceeded"
			m.cleanupJobPayload(ctx, *job)
		} else {
			job.Status = jobStatusRetrying
			job.LastError = "operation deadline exceeded; requeued"
		}
		if _, err := m.jobRepo.UpdateById(ctx, "", *job); err != nil {
			return recovered, fmt.Errorf("recover upload job %d: %w", job.Id, err)
		}
		recovered++
	}

	return recovered, nil
}

func (m *fileStorageService) SweepExpiredFiles(ctx context.Context, nowUnix int64, limit uint64) (uint64, error) {
	if limit == 0 {
		limit = 100
	}
	if nowUnix <= 0 {
		nowUnix = time.Now().UTC().Unix()
	}

	files, _, err := m.repo.Get(ctx, "", limit, 0, []sqldataenums.Filter{
		{FieldName: "ExpiredAt", Compare: sqldataenums.GreaterThan, Value: int64(0)},
		{FieldName: "ExpiredAt", Compare: sqldataenums.LessThanOrEqualTo, Value: nowUnix},
	}, []sqldataenums.Sorter{
		{FieldName: "ExpiredAt", Sort: 1},
		{FieldName: "Id", Sort: 1},
	})
	if err != nil {
		if isRepoNotFoundError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("list expired files: %w", err)
	}

	deleted := uint64(0)
	for _, file := range files {
		if file == nil || file.Id <= 0 {
			continue
		}
		path, err := m.storedFilePath(file.Guid)
		if err != nil {
			return deleted, fmt.Errorf("resolve expired file %d: %w", file.Id, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return deleted, fmt.Errorf("remove expired file %d: %w", file.Id, err)
		}
		if _, err := m.repo.DeleteById(ctx, "", uint64(file.Id)); err != nil {
			return deleted, fmt.Errorf("delete expired file metadata %d: %w", file.Id, err)
		}
		deleted++
	}

	return deleted, nil
}

func (m *fileStorageService) storeUploads(ctx context.Context, uploads []FileStorageUpload, cleanupOnFailure bool) ([]*entities.FileStorage, error) {
	if len(uploads) == 0 {
		return []*entities.FileStorage{}, nil
	}
	if m.db == nil {
		return nil, errors.New("file storage transaction database is not configured")
	}

	if m.operationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.operationTimeout)
		defer cancel()
	}

	var lock coordination.Lock
	var err error
	if m.locker != nil {
		lock, err = m.locker.Lock(ctx, "file-storage")
		if err != nil {
			return nil, fmt.Errorf("acquire file storage transaction lock: %w", err)
		}
		defer lock.Release(context.Background())
	}

	txStarter, ok := m.db.(dbsql.ScopedTxStarter)
	if !ok {
		return nil, errors.New("database adapter does not support scoped transactions")
	}

	txCrud, err := txStarter.BeginScopedTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin file storage transaction: %w", err)
	}
	txRepo := dbsql.NewGenericRepo[entities.FileStorage](txCrud)

	committed := false
	movedFiles := make([]string, 0, len(uploads))
	defer func() {
		if committed {
			return
		}
		_ = txCrud.RollbackTx()
		for _, finalPath := range movedFiles {
			_ = os.Remove(finalPath)
		}
		if !cleanupOnFailure {
			return
		}
		for _, upload := range uploads {
			_ = os.Remove(upload.TempPath)
		}
	}()

	results := make([]*entities.FileStorage, 0, len(uploads))
	for _, upload := range uploads {
		model := upload.Model
		copyFinal := true
		existing, err := txRepo.GetByUnique(ctx, "", "guid", model.Guid)
		if err == nil && existing != nil && existing.Id > 0 {
			model = *existing
			if _, err := os.Stat(upload.FinalPath); err == nil {
				copyFinal = false
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("check final file storage path: %w", err)
			}
		} else {
			if err != nil && !isRepoNotFoundError(err) {
				return nil, fmt.Errorf("check file storage metadata by guid: %w", err)
			}
			id, err := txRepo.Create(ctx, "", model)
			if err != nil {
				return nil, fmt.Errorf("insert file storage metadata: %w", err)
			}
			model.Id = int64(id)
		}

		if copyFinal {
			if err := copyFileAtomic(upload.TempPath, upload.FinalPath); err != nil {
				return nil, fmt.Errorf("write uploaded file into final storage: %w", err)
			}
			movedFiles = append(movedFiles, upload.FinalPath)
		}

		results = append(results, &model)
	}

	if err := txCrud.CommitTx(); err != nil {
		return nil, fmt.Errorf("commit file storage transaction: %w", err)
	}
	committed = true
	for _, upload := range uploads {
		_ = os.Remove(upload.TempPath)
	}

	return results, nil
}

func (m *fileStorageService) processUploadJob(ctx context.Context, job entities.OperationJob) error {
	uploads, err := decodeJobPayload(job.Payload)
	if err != nil {
		return m.finishJob(ctx, job, jobStatusFailed, nil, fmt.Errorf("decode upload job payload: %w", err), true)
	}

	now := time.Now().UTC().Unix()
	timeout := m.operationTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	job.Status = jobStatusRunning
	job.Attempt++
	job.LockedBy = uuid.NewString()
	job.StartedAt = now
	job.DeadlineAt = now + int64(timeout.Seconds())
	job.UpdatedAt = now
	job.LastError = ""
	if _, err := m.jobRepo.UpdateById(ctx, "", job); err != nil {
		return fmt.Errorf("mark upload job running: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := m.storeUploads(runCtx, uploads, false)
	if err != nil {
		retryable := job.Attempt < job.MaxAttempts
		status := jobStatusFailed
		if retryable {
			status = jobStatusRetrying
		}
		return m.finishJob(ctx, job, status, nil, err, !retryable)
	}

	return m.finishJob(ctx, job, jobStatusSucceeded, result, nil, false)
}

func (m *fileStorageService) finishJob(ctx context.Context, job entities.OperationJob, status string, result []*entities.FileStorage, jobErr error, cleanupPayload bool) error {
	now := time.Now().UTC().Unix()
	job.Status = status
	job.LockedBy = ""
	job.StartedAt = 0
	job.DeadlineAt = 0
	job.UpdatedAt = now
	if status == jobStatusSucceeded || status == jobStatusFailed {
		job.CompletedAt = now
	}
	if jobErr != nil {
		job.LastError = trimJobString(jobErr.Error(), 1000)
	} else {
		job.LastError = ""
	}
	if result != nil {
		encoded, err := encodeJobResult(result)
		if err != nil {
			return fmt.Errorf("encode upload job result: %w", err)
		}
		job.Result = encoded
	}
	if cleanupPayload {
		m.cleanupJobPayload(ctx, job)
	}

	if _, err := m.jobRepo.UpdateById(ctx, "", job); err != nil {
		return fmt.Errorf("finish upload job %d: %w", job.Id, err)
	}
	return nil
}

func (m *fileStorageService) cleanupJobPayload(_ context.Context, job entities.OperationJob) {
	uploads, err := decodeJobPayload(job.Payload)
	if err != nil {
		return
	}
	for _, upload := range uploads {
		_ = os.Remove(upload.TempPath)
		_ = os.Remove(upload.FinalPath)
	}
}

func encodeJobPayload(uploads []FileStorageUpload) (string, error) {
	return encodeJSONBase64(uploads)
}

func decodeJobPayload(payload string) ([]FileStorageUpload, error) {
	var uploads []FileStorageUpload
	if err := decodeJSONBase64(payload, &uploads); err != nil {
		return nil, err
	}
	return uploads, nil
}

func encodeJobResult(result []*entities.FileStorage) (string, error) {
	return encodeJSONBase64(result)
}

func encodeJSONBase64(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func decodeJSONBase64(value string, dest any) error {
	b, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

func copyFileAtomic(srcPath string, finalPath string) error {
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return err
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	temp, err := os.CreateTemp(filepath.Dir(finalPath), ".commit-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(temp, src); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	_ = os.Remove(finalPath)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}
	cleanupTemp = false
	return nil
}

func (m *fileStorageService) downloadModel(model entities.FileStorage) (*FileStorageDownload, error) {
	path, err := m.storedFilePath(model.Guid)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read stored file: %w", err)
	}

	filename := strings.TrimSpace(model.Title)
	if filename == "" {
		filename = model.Guid
	}
	mimeType := strings.TrimSpace(model.MimeType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return &FileStorageDownload{
		Model:    model,
		Filename: filename,
		MimeType: mimeType,
		Content:  content,
	}, nil
}

func (m *fileStorageService) authorizeDownload(ctx context.Context, model entities.FileStorage, actor *FileStorageDownloadActor) error {
	if model.ExpiredAt > 0 && model.ExpiredAt <= time.Now().UTC().Unix() {
		return errors.New("file is expired")
	}
	if !filestorageenums.IsValidSecurityLevel(model.SecurityLvl) {
		return errors.New("file security level is invalid")
	}

	switch filestorageenums.SecurityLevel(model.SecurityLvl) {
	case filestorageenums.Public:
		return nil
	case filestorageenums.SystemOnly:
		if actor != nil && actor.IsSystem {
			return nil
		}
		return errors.New("file access is restricted to system services")
	case filestorageenums.Group:
		return m.authorizeGroupDownload(ctx, model, actor)
	case filestorageenums.Role:
		return m.authorizeRoleDownload(ctx, model, actor)
	default:
		return errors.New("file security level is invalid")
	}
}

func (m *fileStorageService) authorizeGroupDownload(ctx context.Context, model entities.FileStorage, actor *FileStorageDownloadActor) error {
	ownerRole, actorRole, err := m.resolveOwnerAndActorRoles(ctx, model, actor)
	if err != nil {
		return err
	}
	if ownerRole.GroupId > 0 && ownerRole.GroupId == actorRole.GroupId {
		return nil
	}
	return errors.New("file access is restricted to the owner's group")
}

func (m *fileStorageService) authorizeRoleDownload(ctx context.Context, model entities.FileStorage, actor *FileStorageDownloadActor) error {
	ownerRole, actorRole, err := m.resolveOwnerAndActorRoles(ctx, model, actor)
	if err != nil {
		return err
	}
	if actorRole.Id <= 0 {
		return errors.New("file access requires a role")
	}
	for role := ownerRole; role.Id > 0; {
		if role.Id == actorRole.Id {
			return nil
		}
		if role.ParentId <= 0 || role.ParentId == role.Id {
			break
		}
		parent, err := m.roleRepo.GetById(ctx, "", uint64(role.ParentId))
		if err != nil {
			return fmt.Errorf("load parent role %d: %w", role.ParentId, err)
		}
		role = *parent
	}
	return errors.New("file access is restricted to the owner's role hierarchy")
}

func (m *fileStorageService) resolveOwnerAndActorRoles(ctx context.Context, model entities.FileStorage, actor *FileStorageDownloadActor) (entities.UserRole, entities.UserRole, error) {
	if actor == nil || actor.RoleId <= 0 {
		return entities.UserRole{}, entities.UserRole{}, errors.New("file access requires authentication")
	}
	if m.userRepo == nil || m.roleRepo == nil {
		return entities.UserRole{}, entities.UserRole{}, errors.New("file access repositories are not configured")
	}
	owner, err := m.userRepo.GetById(ctx, "", uint64(model.CreatedBy))
	if err != nil {
		return entities.UserRole{}, entities.UserRole{}, fmt.Errorf("load file owner: %w", err)
	}
	if owner.UserRoleId <= 0 {
		return entities.UserRole{}, entities.UserRole{}, errors.New("file owner role is not configured")
	}
	ownerRole, err := m.roleRepo.GetById(ctx, "", uint64(owner.UserRoleId))
	if err != nil {
		return entities.UserRole{}, entities.UserRole{}, fmt.Errorf("load file owner role: %w", err)
	}
	actorRole, err := m.roleRepo.GetById(ctx, "", uint64(actor.RoleId))
	if err != nil {
		return entities.UserRole{}, entities.UserRole{}, fmt.Errorf("load actor role: %w", err)
	}
	return *ownerRole, *actorRole, nil
}

func (m *fileStorageService) storedFilePath(guid string) (string, error) {
	if strings.TrimSpace(m.storagePath) == "" {
		return "", errors.New("file storage path is not configured")
	}
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return "", errors.New("file guid is required")
	}
	if filepath.Base(guid) != guid {
		return "", errors.New("file guid is invalid")
	}
	return filepath.Join(m.storagePath, guid), nil
}

func trimJobString(value string, max int) string {
	if len(value) <= max {
		return strings.ReplaceAll(value, "'", "''")
	}
	return strings.ReplaceAll(value[:max], "'", "''")
}

func isRepoNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no rows") || strings.Contains(msg, "not found") || strings.Contains(msg, "no result found")
}
