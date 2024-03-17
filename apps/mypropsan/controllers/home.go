package controllers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
	"github.com/mysayasan/kopiv2/infra/middlewares"
)

// HomeApi struct
type homeApi struct {
	auth middlewares.AuthMiddleware
	repo repos.IHomeRepo
}

// Create HomeApi
func NewHomeApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	repo repos.IHomeRepo) {
	handler := &homeApi{
		auth: auth,
		repo: repo,
	}

	group := router.Group("home")
	group.Get("/latest", handler.latest).Name("latest")
}

func (m *homeApi) latest(c *fiber.Ctx) error {
	res, _, err := m.repo.GetLatest()
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString(err.Error())
	}

	return c.JSON(res)
}
