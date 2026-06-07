package apis

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	filestorageenums "github.com/mysayasan/kopiv2/domain/enums/filestorage"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// FileStorageApi struct
type fileStorageApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.IFileStorageService
	path string
}

type fileStorageJobResponse struct {
	Id             int64  `json:"id"`
	Type           string `json:"type"`
	ResourceKey    string `json:"resourceKey"`
	IdempotencyKey string `json:"idempotencyKey"`
	Status         string `json:"status"`
	Attempt        int64  `json:"attempt"`
	MaxAttempts    int64  `json:"maxAttempts"`
	Result         string `json:"result,omitempty"`
	LastError      string `json:"lastError,omitempty"`
	StartedAt      int64  `json:"startedAt,omitempty"`
	DeadlineAt     int64  `json:"deadlineAt,omitempty"`
	CompletedAt    int64  `json:"completedAt,omitempty"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// Create FileStorageApi
func NewFileStorageApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.IFileStorageService,
	path string) {
	handler := &fileStorageApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
		path: path,
	}

	// Create api sub-router
	base := router.PathPrefix("/file-storage").Subrouter()
	base.HandleFunc("/download", handler.download).Methods("GET")

	group := base.PathPrefix("").Subrouter()
	group.Use(auth.Middleware)

	// Group Handlers
	group.HandleFunc("/upload", rbac.RbacHandler(handler.upload)).Methods("POST")
	group.HandleFunc("/upload-async", rbac.RbacHandler(handler.uploadAsync)).Methods("POST")
	group.HandleFunc("/job", rbac.RbacHandler(handler.job)).Methods("GET")

	// group := router.Group("file-storage")
	// group.Post("/upload", auth.JwtHandler(), rbac.ApiHandler(), handler.upload).Name("upload")
	// group.Get("/download", auth.JwtHandler(), rbac.ApiHandler(), handler.download).Name("download")
}

func (m *fileStorageApi) download(w http.ResponseWriter, r *http.Request) {
	if rawIDs := strings.TrimSpace(r.URL.Query().Get("ids")); rawIDs != "" {
		ids, err := parseDownloadIDs(rawIDs)
		if err != nil {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		downloads, err := m.serv.DownloadByIds(r.Context(), ids, m.downloadActor(r))
		if err != nil {
			sendDownloadError(w, err)
			return
		}
		if err := writeDownloadZip(w, downloads); err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		return
	}

	viewInline, err := parseOptionalBool(r.URL.Query().Get("view"), "view")
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	var (
		download *services.FileStorageDownload
	)
	if rawID := strings.TrimSpace(r.URL.Query().Get("id")); rawID != "" {
		id, parseErr := strconv.ParseUint(rawID, 10, 64)
		if parseErr != nil || id == 0 {
			controllers.SendError(w, controllers.ErrBadRequest, "valid id is required")
			return
		}
		download, err = m.serv.DownloadById(r.Context(), id, m.downloadActor(r))
	} else {
		controllers.SendError(w, controllers.ErrBadRequest, "id or ids is required")
		return
	}
	if err != nil {
		sendDownloadError(w, err)
		return
	}

	writeSingleDownload(w, download, viewInline)
}

func (m *fileStorageApi) upload(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	stagedUploads, failedUploads, err := m.stageUploads(r, claims)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	cleanupStaged := true
	defer func() {
		if !cleanupStaged {
			return
		}
		for _, upload := range stagedUploads {
			_ = os.Remove(upload.TempPath)
		}
	}()

	if len(failedUploads) > 0 {
		controllers.SendError(w, controllers.ErrUplodFailed, "some file(s) failed to upload", failedUploads)
		return
	}

	uploadedFiles, err := m.serv.StoreUploads(r.Context(), stagedUploads)
	if err != nil {
		controllers.SendError(w, controllers.ErrUplodFailed, err.Error())
		return
	}
	cleanupStaged = false

	controllers.SendPagingResult(w, uploadedFiles, 0, 0, uint64(len(uploadedFiles)))
}

func (m *fileStorageApi) uploadAsync(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	stagedUploads, failedUploads, err := m.stageUploads(r, claims)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	cleanupStaged := true
	defer func() {
		if !cleanupStaged {
			return
		}
		for _, upload := range stagedUploads {
			_ = os.Remove(upload.TempPath)
		}
	}()

	if len(failedUploads) > 0 {
		controllers.SendError(w, controllers.ErrUplodFailed, "some file(s) failed to upload", failedUploads)
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	job, err := m.serv.EnqueueUploads(r.Context(), stagedUploads, idempotencyKey)
	if err != nil {
		controllers.SendError(w, controllers.ErrUplodFailed, err.Error())
		return
	}
	cleanupStaged = false

	controllers.SendResult(w, newFileStorageJobResponse(job), "queued")
}

func (m *fileStorageApi) job(w http.ResponseWriter, r *http.Request) {
	rawId := r.URL.Query().Get("id")
	id, err := strconv.ParseUint(rawId, 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "valid id is required")
		return
	}

	job, err := m.serv.GetUploadJob(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendResult(w, newFileStorageJobResponse(job))
}

func (m *fileStorageApi) stageUploads(r *http.Request, claims *models.JwtCustomClaims) ([]services.FileStorageUpload, []string, error) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no multipart boundary") {
			return nil, nil, fmt.Errorf("multipart boundary is missing; remove the manual Content-Type header and let the browser or curl -F set it")
		}
		return nil, nil, err
	}

	files := r.MultipartForm.File["documents"]
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("documents is required")
	}
	securityLvl, err := parseSecurityLevel(r.FormValue("securityLvl"))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	expiredAt, err := parseExpiration(r.FormValue("expiredAt"), r.FormValue("expiresIn"), r.FormValue("expiresInUnit"), now)
	if err != nil {
		return nil, nil, err
	}

	failedUploads := make([]string, 0)
	stagedUploads := make([]services.FileStorageUpload, 0, len(files))

	if err := os.MkdirAll(m.path, 0755); err != nil {
		return nil, nil, err
	}

	stagingPath := filepath.Join(m.path, ".staging")
	if err := os.MkdirAll(stagingPath, 0755); err != nil {
		return nil, nil, err
	}

	for _, file := range files {
		file := file
		contentType := file.Header.Get("Content-Type")
		switch contentType {
		case "image/jpeg", "image/png", "application/pdf", "text/plain":
		default:
			failedUploads = append(failedUploads, fmt.Sprintf("error %s : file type is not supported", file.Filename))
			continue
		}

		src, err := file.Open()
		if err != nil {
			failedUploads = append(failedUploads, file.Filename)
			continue
		}

		tempFile, err := os.CreateTemp(stagingPath, "upload-*")
		if err != nil {
			_ = src.Close()
			failedUploads = append(failedUploads, file.Filename)
			continue
		}

		hasher := sha1.New()
		_, copyErr := io.Copy(tempFile, io.TeeReader(src, hasher))
		closeErr := tempFile.Close()
		_ = src.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tempFile.Name())
			failedUploads = append(failedUploads, file.Filename)
			continue
		}

		sha := base64.URLEncoding.EncodeToString(hasher.Sum(nil))

		model := entities.FileStorage{
			Title:       file.Filename,
			Description: file.Filename,
			Guid:        uuid.New().String(),
			MimeType:    contentType,
			VrPath:      "/",
			Sha1Chksum:  sha,
			SecurityLvl: securityLvl,
			ExpiredAt:   expiredAt,
			CreatedBy:   claims.Id,
			CreatedAt:   now.Unix(),
		}

		stagedUploads = append(stagedUploads, services.FileStorageUpload{
			Model:     model,
			TempPath:  tempFile.Name(),
			FinalPath: filepath.Join(m.path, model.Guid),
		})
	}

	if len(failedUploads) > 0 || len(stagedUploads) != len(files) {
		for _, upload := range stagedUploads {
			_ = os.Remove(upload.TempPath)
		}
	}

	return stagedUploads, failedUploads, nil
}

func newFileStorageJobResponse(job *entities.OperationJob) *fileStorageJobResponse {
	if job == nil {
		return nil
	}
	return &fileStorageJobResponse{
		Id:             job.Id,
		Type:           job.Type,
		ResourceKey:    job.ResourceKey,
		IdempotencyKey: job.IdempotencyKey,
		Status:         job.Status,
		Attempt:        job.Attempt,
		MaxAttempts:    job.MaxAttempts,
		Result:         job.Result,
		LastError:      job.LastError,
		StartedAt:      job.StartedAt,
		DeadlineAt:     job.DeadlineAt,
		CompletedAt:    job.CompletedAt,
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
	}
}

func (m *fileStorageApi) downloadActor(r *http.Request) *services.FileStorageDownloadActor {
	claims, err := m.auth.ClaimsFromRequest(r)
	if err != nil || claims == nil {
		return nil
	}
	return &services.FileStorageDownloadActor{
		UserId: claims.Id,
		RoleId: claims.RoleId,
	}
}

func parseSecurityLevel(raw string) (int32, error) {
	if strings.TrimSpace(raw) == "" {
		return int32(filestorageenums.SystemOnly), nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil || !filestorageenums.IsValidSecurityLevel(int32(value)) {
		return 0, fmt.Errorf("valid securityLvl is required")
	}
	return int32(value), nil
}

func parseOptionalUnix(raw string, name string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("valid %s is required", name)
	}
	return value, nil
}

func parseExpiration(rawExpiredAt string, rawExpiresIn string, rawExpiresInUnit string, now time.Time) (int64, error) {
	rawExpiredAt = strings.TrimSpace(rawExpiredAt)
	rawExpiresIn = strings.TrimSpace(rawExpiresIn)
	rawExpiresInUnit = strings.TrimSpace(rawExpiresInUnit)

	hasExpiredAt := rawExpiredAt != "" && rawExpiredAt != "0"
	hasExpiresIn := rawExpiresIn != "" && rawExpiresIn != "0"
	hasExpiresInUnit := strings.TrimSpace(rawExpiresInUnit) != ""
	if hasExpiredAt && (hasExpiresIn || hasExpiresInUnit) {
		return 0, fmt.Errorf("use either expiredAt or expiresIn with expiresInUnit, not both")
	}
	if hasExpiredAt {
		return parseOptionalUnix(rawExpiredAt, "expiredAt")
	}
	if rawExpiredAt != "" && rawExpiredAt != "0" {
		return 0, fmt.Errorf("valid expiredAt is required")
	}
	if !hasExpiresIn && !hasExpiresInUnit {
		return 0, nil
	}
	if !hasExpiresIn || !hasExpiresInUnit {
		return 0, fmt.Errorf("expiresIn and expiresInUnit are required together")
	}

	expiresIn, err := strconv.ParseInt(rawExpiresIn, 10, 64)
	if err != nil || expiresIn <= 0 {
		return 0, fmt.Errorf("valid expiresIn is required")
	}

	unit := strings.ToLower(strings.TrimSpace(rawExpiresInUnit))
	switch unit {
	case "second", "seconds", "sec", "secs", "s":
		return now.Add(time.Duration(expiresIn) * time.Second).Unix(), nil
	case "minute", "minutes", "min", "mins":
		return now.Add(time.Duration(expiresIn) * time.Minute).Unix(), nil
	case "hour", "hours", "hr", "hrs", "h":
		return now.Add(time.Duration(expiresIn) * time.Hour).Unix(), nil
	case "day", "days", "d":
		return now.AddDate(0, 0, int(expiresIn)).Unix(), nil
	case "week", "weeks", "w":
		return now.AddDate(0, 0, int(expiresIn)*7).Unix(), nil
	case "month", "months", "mo", "mos":
		return now.AddDate(0, int(expiresIn), 0).Unix(), nil
	case "year", "years", "yr", "yrs", "y":
		return now.AddDate(int(expiresIn), 0, 0).Unix(), nil
	default:
		return 0, fmt.Errorf("valid expiresInUnit is required")
	}
}

func parseOptionalBool(raw string, name string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("valid %s is required", name)
	}
	return value, nil
}

func sendDownloadError(w http.ResponseWriter, err error) {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "requires authentication"):
		controllers.SendError(w, controllers.ErrAuthFailed, err.Error())
	case strings.Contains(msg, "restricted"):
		controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
	case strings.Contains(msg, "expired"):
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
	default:
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
	}
}

func parseDownloadIDs(raw string) ([]uint64, error) {
	parts := strings.Split(raw, ",")
	ids := make([]uint64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("valid ids are required")
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("valid ids are required")
	}
	return ids, nil
}

func writeSingleDownload(w http.ResponseWriter, download *services.FileStorageDownload, viewInline bool) {
	if download == nil {
		controllers.SendError(w, controllers.ErrNotFound, "file not found")
		return
	}
	filename := safeDownloadFilename(download.Filename, download.Model.Guid)
	disposition := "attachment"
	if viewInline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, escapeHeaderFilename(filename)))
	w.Header().Set("Content-Type", download.MimeType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(download.Content))
}

func writeDownloadZip(w http.ResponseWriter, downloads []*services.FileStorageDownload) error {
	content, err := buildDownloadZip(downloads)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", `attachment; filename="file-storage.zip"`)
	w.Header().Set("Content-Type", "application/zip")
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, bytes.NewReader(content))
	return err
}

func buildDownloadZip(downloads []*services.FileStorageDownload) ([]byte, error) {
	if len(downloads) == 0 {
		return nil, fmt.Errorf("downloads is required")
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	usedNames := map[string]int{}
	for _, download := range downloads {
		if download == nil {
			continue
		}
		name := uniqueZipName(safeDownloadFilename(download.Filename, download.Model.Guid), usedNames)
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if download.Model.CreatedAt > 0 {
			header.SetModTime(time.Unix(download.Model.CreatedAt, 0).UTC())
		}
		writer, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := writer.Write(download.Content); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func safeDownloadFilename(filename string, fallback string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = strings.TrimSpace(fallback)
	}
	if filename == "" {
		filename = "download"
	}
	filename = filepath.Base(filename)
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")
	filename = replacer.Replace(filename)
	if strings.Trim(filename, ". ") == "" {
		return "download"
	}
	return filename
}

func uniqueZipName(name string, used map[string]int) string {
	count := used[name]
	used[name] = count + 1
	if count == 0 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", base, count+1, ext)
}

func escapeHeaderFilename(filename string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filename)
}
