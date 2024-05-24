package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
	// => *multipart.Form

	// Get all files from "documents" key:
	files := form.File["documents"]
	// => []*multipart.FileHeader

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

		guid := uuid.New().String()

		// Create dir if not exists
		if _, err := os.Stat(m.path); os.IsNotExist(err) {
			err := os.Mkdir(m.path, os.ModePerm)
			if err != nil {
				return err
			}
		}

		// Save the files to disk:
		err := c.SaveFile(file, fmt.Sprintf("%s/%s", m.path, guid))

		// Check for errors
		if err != nil {
			return err
		}

		var model entity.FileStorageEntity
		model.Title = file.Filename
		model.Description = file.Filename
		model.Guid = guid
		model.MimeType = file.Header["Content-Type"][0]
		model.VrPath = "/"

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
