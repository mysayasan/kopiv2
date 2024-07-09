package apis

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// FileStorageApi struct
type fileStorageApi struct {
	auth middlewares.AuthMiddleware
	serv services.IFileStorageService
	path string
}

// Create FileStorageApi
func NewFileStorageApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	serv services.IFileStorageService,
	path string) {
	handler := &fileStorageApi{
		auth: auth,
		serv: serv,
		path: path,
	}

	group := router.Group("file-storage")
	group.Post("/upload", auth.JwtHandler(), handler.upload).Name("upload")
	group.Get("/download", auth.JwtHandler(), handler.download).Name("download")
}

func (m *fileStorageApi) download(c *fiber.Ctx) error {
	user := c.Locals("user").(*jwt.Token)

	claims := &middlewares.JwtCustomClaimsModel{}
	tmp, _ := json.Marshal(user.Claims)
	_ = json.Unmarshal(tmp, claims)

	name := claims.Name

	log.Info(name)

	guid := c.Query("guid")

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}

	fileInfo, err := m.serv.GetByGuid(ctx, guid)
	if err != nil {
		return err
	}

	log.Info((fileInfo))

	// open input file
	fi, err := os.Open(fmt.Sprintf("%s/%s", m.path, guid))
	if err != nil {
		return err
	}
	// close fi on exit and check for its returned error
	defer func() {
		if err := fi.Close(); err != nil {
			return
		}
	}()

	content, err := io.ReadAll(fi)
	if err != nil {
		return err
	}

	if len(content) > 0 {
		c.Set("Content-Type", fileInfo.MimeType)
		c.SendStream(bytes.NewReader((content)))
	}

	return nil

}

func (m *fileStorageApi) upload(c *fiber.Ctx) error {
	user := c.Locals("user").(*jwt.Token)

	claims := &middlewares.JwtCustomClaimsModel{}
	tmp, _ := json.Marshal(user.Claims)
	_ = json.Unmarshal(tmp, claims)

	name := claims.Name

	log.Info(name)

	// Parse the multipart form:
	form, err := c.MultipartForm()
	if err != nil {
		return err
	}

	// Get all files from "documents" key:
	files := form.File["documents"]

	uploadedFiles := make([]*entity.FileStorageEntity, 0)
	failedUploads := make([]string, 0)
	// Loop through files:
	for _, file := range files {
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

		var model entity.FileStorageEntity
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

		// Save the files to disk:
		err = c.SaveFile(file, fmt.Sprintf("%s/%s", m.path, model.Guid))

		// Check for errors
		if err != nil {
			failedUploads = append(failedUploads, file.Filename)
		}

		ctx := c.UserContext()
		if ctx == nil {
			ctx = context.Background()
		}
		res, err := m.serv.Add(ctx, model)
		if err != nil {
			_ = os.Remove(fmt.Sprintf("%s/%s", m.path, model.Guid))
			failedUploads = append(failedUploads, file.Filename)
		}

		model.Id = int64(res)
		uploadedFiles = append(uploadedFiles, &model)
	}

	if len(uploadedFiles) != len(files) {
		return controllers.SendError(c, controllers.ErrUplodFailed, "some file(s) failed to upload", failedUploads)
	}

	return controllers.SendPagingResult(c, uploadedFiles, 0, 0, uint64(len(uploadedFiles)))
}
