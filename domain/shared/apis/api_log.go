package apis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares/timeout"
)

// ApiLogApi struct
type apiLogApi struct {
	auth middlewares.AuthMiddleware
	rbac middlewares.RbacMiddleware
	serv services.IApiLogService
}

// Create ApiLogApi
func NewApiLogApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	rbac middlewares.RbacMiddleware,
	serv services.IApiLogService) {
	handler := &apiLogApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	group := router.Group("log")
	group.Get("/", auth.JwtHandler(), rbac.ApiHandler(), timeout.NewWithContext(handler.get, 60*1000*time.Millisecond)).Name("latest")
}

func (m *apiLogApi) get(c *fiber.Ctx) error {

	limit, _ := strconv.ParseUint(c.Query("limit"), 10, 64)
	offset, _ := strconv.ParseUint(c.Query("offset"), 10, 64)

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}
	res, totalCnt, err := m.serv.Get(ctx, limit, offset)
	if err != nil {
		return controllers.SendError(c, controllers.ErrNotFound, err.Error())
	}

	c.Response().Header.Add("X-Rows", fmt.Sprintf("%d", totalCnt))

	return controllers.SendPagingResult(c, res, limit, offset, totalCnt)
}
