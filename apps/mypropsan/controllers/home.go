package controllers

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/middlewares"
	"github.com/mysayasan/kopiv2/infra/middlewares/timeout"
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
	group.Get("/latest", timeout.NewWithContext(handler.latest, 2*time.Second)).Name("latest")
}

func (m *homeApi) latest(c *fiber.Ctx) error {
	limit, _ := strconv.ParseUint(c.Query("limit"), 10, 64)
	offset, _ := strconv.ParseUint(c.Query("offset"), 10, 64)

	res, totalCnt, err := m.serv.GetLatest(limit, offset)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString(err.Error())
	}

	c.Response().Header.Add("X-Rows", fmt.Sprintf("%d", totalCnt))

	return controllers.SendJSON(c, res, limit, offset, totalCnt)
}
