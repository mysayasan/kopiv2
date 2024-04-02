package controllers

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/google/uuid"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// StorageApi struct
type storageApi struct {
	auth middlewares.AuthMiddleware
	serv services.IStorageService
}

// Create StorageApi
func NewStorageApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	serv services.IStorageService) {
	handler := &storageApi{
		auth: auth,
		serv: serv,
	}

	group := router.Group("storage")
	group.Post("/upload", auth.JwtHandler(), handler.upload).Name("upload")
}

func (m *storageApi) upload(c *fiber.Ctx) error {
	// user := c.Locals("user").(*jwt.Token)

	// claims := &middlewares.JwtCustomClaimsModel{}
	// tmp, _ := json.Marshal(user.Claims)
	// _ = json.Unmarshal(tmp, claims)

	// name := claims.Name

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
		case "image/jpeg", "image/png":
			{
				cnt += 1
				break
			}
		default:
			{
				continue
			}
		}
		// => "tutorial.pdf" 360641 "application/pdf"

		// Save the files to disk:
		err := c.SaveFile(file, fmt.Sprintf("./%s", file.Filename))

		// Check for errors
		if err != nil {
			return err
		}
	}

	log.Info(cnt)

	if cnt == 0 {
		return c.Status(fiber.StatusUnprocessableEntity).SendString("files not compatible")
	}

	var model entity.StorageEntity
	model.Title = "test"
	model.Description = "test"
	model.Guid = uuid.New().String()

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := m.serv.Add(ctx, model)
	if err != nil {
		return err
	}

	return controllers.SendJSON(c, res, 0, 0, 0)
}
