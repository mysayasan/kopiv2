package apis

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
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
	group := router.PathPrefix("/file-storage").Subrouter()
	group.Use(auth.Middleware)

	// Group Handlers
	group.HandleFunc("/upload", rbac.RbacHandler(handler.upload)).Methods("POST")
	group.HandleFunc("/download", rbac.RbacHandler(handler.download)).Methods("GET")

	// group := router.Group("file-storage")
	// group.Post("/upload", auth.JwtHandler(), rbac.ApiHandler(), handler.upload).Name("upload")
	// group.Get("/download", auth.JwtHandler(), rbac.ApiHandler(), handler.download).Name("download")
}

func (m *fileStorageApi) download(w http.ResponseWriter, r *http.Request) {
	guid := r.URL.Query().Get("guid")

	fileInfo, err := m.serv.GetByGuid(r.Context(), guid)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	fmt.Printf("%v", fileInfo)

	// open input file
	fi, err := os.Open(fmt.Sprintf("%s/%s", m.path, guid))
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	// close fi on exit and check for its returned error
	defer func() {
		if err := fi.Close(); err != nil {
			return
		}
	}()

	content, err := io.ReadAll(fi)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	if len(content) > 0 {
		w.Header().Set("Content-Disposition", "attachment; filename=WHATEVER_YOU_WANT")
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		io.Copy(w, bytes.NewReader(content))
	}
}

func (m *fileStorageApi) upload(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)

	// Parse the multipart form:
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	// Get all files from "documents" key:
	files := r.MultipartForm.File["documents"]

	uploadedFiles := make([]*entities.FileStorage, 0)
	failedUploads := make([]string, 0)

	// Loop through files:
	for _, file := range files {
		file := file
		fmt.Println(file.Filename, file.Size, file.Header["Content-Type"][0])
		switch file.Header["Content-Type"][0] {
		case "image/jpeg", "image/png", "application/pdf":
			{
				break
			}
		default:
			{
				failedUploads = append(failedUploads, fmt.Sprintf("error %s : file type is not supported", file.Filename))
				continue
			}
		}

		buf, err := file.Open()
		if err != nil {
			failedUploads = append(failedUploads, file.Filename)
		}

		content, err := io.ReadAll(buf)
		if err != nil {
			failedUploads = append(failedUploads, file.Filename)
		}

		hasher := sha1.New()
		hasher.Write(content)
		sha := base64.URLEncoding.EncodeToString(hasher.Sum(nil))

		var model entities.FileStorage
		model.Title = file.Filename
		model.Description = file.Filename
		model.Guid = uuid.New().String()
		model.MimeType = file.Header["Content-Type"][0]
		model.VrPath = "/"
		model.Sha1Chksum = sha
		model.CreatedBy = claims.Id
		model.CreatedAt = time.Now().UTC().Unix()

		// Create dir if not exists
		if _, err := os.Stat(m.path); os.IsNotExist(err) {
			err := os.Mkdir(m.path, os.ModePerm)
			if err != nil {
				failedUploads = append(failedUploads, file.Filename)
			}
		}

		// open and copy files
		newfile, err := os.OpenFile(fmt.Sprintf("%s/%s", m.path, model.Guid), os.O_WRONLY|os.O_CREATE, os.ModePerm)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("something went wrong")
			return
		}
		defer newfile.Close()

		// Save the files to disk:
		// err = c.SaveFile(file, fmt.Sprintf("%s/%s", m.path, model.Guid))
		_, err = io.Copy(newfile, buf)
		if err != nil {
			failedUploads = append(failedUploads, file.Filename)
		}

		res, err := m.serv.Create(r.Context(), model)
		if err != nil {
			_ = os.Remove(fmt.Sprintf("%s/%s", m.path, model.Guid))
			failedUploads = append(failedUploads, file.Filename)
		}

		model.Id = int64(res)
		uploadedFiles = append(uploadedFiles, &model)
	}

	if len(uploadedFiles) != len(files) {
		controllers.SendError(w, controllers.ErrUplodFailed, "some file(s) failed to upload", failedUploads)
		return
	}

	controllers.SendPagingResult(w, uploadedFiles, 0, 0, uint64(len(uploadedFiles)))
}
