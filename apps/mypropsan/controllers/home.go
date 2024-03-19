package controllers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/infra/middlewares"
)

// HomeApi struct
type homeApi struct {
	auth middlewares.AuthMiddleware
	serv services.IHomeService
}

// Create HomeApi
func NewHomeApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	serv services.IHomeService) {
	handler := &homeApi{
		auth: auth,
		serv: serv,
	}

	group := router.Group("home")
	group.Get("/latest", handler.latest).Name("latest")
}

func (m *homeApi) latest(c *fiber.Ctx) error {
	limit, _ := strconv.ParseUint(c.Query("limit"), 10, 64)
	offset, _ := strconv.ParseUint(c.Query("offset"), 10, 64)

	res, _, err := m.serv.GetLatest(limit, offset)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString(err.Error())
	}

	return c.JSON(res)
}
