package controllers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/apps/mypropsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares/timeout"
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
	group.Get("/latest", timeout.NewWithContext(handler.latest, 60*1000*time.Millisecond)).Name("latest")
}

func (m *homeApi) latest(c *fiber.Ctx) error {

	limit, _ := strconv.ParseUint(c.Query("limit"), 10, 64)
	offset, _ := strconv.ParseUint(c.Query("offset"), 10, 64)

	fmt.Printf("Request from URI : %s \n", c.Request().URI().Host())

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}
	res, totalCnt, err := m.serv.GetLatest(ctx, limit, offset)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString(err.Error())
	}

	c.Response().Header.Add("X-Rows", fmt.Sprintf("%d", totalCnt))

	return controllers.SendJSON(c, res, limit, offset, totalCnt)
}
