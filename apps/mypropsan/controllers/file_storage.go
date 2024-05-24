package controllers

import (
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

	cnt := 0
	// Loop through files:
	for _, file := range files {
		fmt.Println(file.Filename, file.Size, file.Header["Content-Type"][0])
		switch file.Header["Content-Type"][0] {
		case "image/jpeg", "image/png", "application/pdf":
			{
				cnt += 1
				break
			}
		default:
			{
				continue
			}
		}

		buf, err := file.Open()
		if err != nil {
			return err
		}

		content, err := io.ReadAll(buf)
		if err != nil {
			return err
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
		model.CreatedBy = claims.Email
		model.CreatedOn = time.Now().UTC().Unix()

		// Create dir if not exists
		if _, err := os.Stat(m.path); os.IsNotExist(err) {
			err := os.Mkdir(m.path, os.ModePerm)
			if err != nil {
				return err
			}
		}

		// Save the files to disk:
		err = c.SaveFile(file, fmt.Sprintf("%s/%s", m.path, model.Guid))

		// Check for errors
		if err != nil {
			return err
		}

		ctx := c.UserContext()
		if ctx == nil {
			ctx = context.Background()
		}
		_, err = m.serv.Add(ctx, model)
		if err != nil {
			return err
		}
	}

	log.Info(cnt)

	if cnt == 0 {
		return c.Status(fiber.StatusUnprocessableEntity).SendString("files not compatible")
	}

	return controllers.SendJSON(c, cnt, 0, 0, 0)
}
