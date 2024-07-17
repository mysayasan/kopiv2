package apis

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

	rbac := *middlewares.NewRbac()

	group := router.Group("home")
	group.Get("/latest", rbac.ApiHandler(), timeout.NewWithContext(handler.latest, 60*1000*time.Millisecond)).Name("latest")
	group.Post("/new", auth.JwtHandler(), rbac.ApiHandler(), timeout.NewWithContext(handler.new, 60*1000*time.Millisecond)).Name("new")
}

func (m *homeApi) latest(c *fiber.Ctx) error {

	limit, _ := strconv.ParseUint(c.Query("limit"), 10, 64)
	offset, _ := strconv.ParseUint(c.Query("offset"), 10, 64)

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}
	res, totalCnt, err := m.serv.GetLatest(ctx, limit, offset)
	if err != nil {
		return controllers.SendError(c, controllers.ErrNotFound, err.Error())
	}

	c.Response().Header.Add("X-Rows", fmt.Sprintf("%d", totalCnt))

	return controllers.SendPagingResult(c, res, limit, offset, totalCnt)
}

func (m *homeApi) new(c *fiber.Ctx) error {
	return controllers.SendPagingResult(c, "ok", 0, 0, 1)
}
