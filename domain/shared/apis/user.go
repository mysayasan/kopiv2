package apis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares/timeout"
)

// UserApi struct
type userApi struct {
	auth middlewares.AuthMiddleware
	serv services.IUserService
}

// Create UserApi
func NewUserApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	serv services.IUserService) {
	handler := &userApi{
		auth: auth,
		serv: serv,
	}

	Rbac := *middlewares.NewRbac()

	group := router.Group("user")
	group.Get("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.getAll, 60*1000*time.Millisecond)).Name("get_all")
	group.Put("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.update, 60*1000*time.Millisecond)).Name("update")
	group.Delete("/:id", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.delete, 60*1000*time.Millisecond)).Name("delete")
}

func (m *userApi) getAll(c *fiber.Ctx) error {

	limit, _ := strconv.ParseUint(c.Query("limit"), 10, 64)
	offset, _ := strconv.ParseUint(c.Query("offset"), 10, 64)

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}
	res, totalCnt, err := m.serv.ReadAll(ctx, limit, offset)
	if err != nil {
		return controllers.SendError(c, controllers.ErrNotFound, err.Error())
	}

	c.Response().Header.Add("X-Rows", fmt.Sprintf("%d", totalCnt))

	return controllers.SendPagingResult(c, res, limit, offset, totalCnt)
}

func (m *userApi) update(c *fiber.Ctx) error {
	body := new(entities.UserLogin)

	if err := c.BodyParser(body); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("%v", body))

	res, err := m.serv.Update(c.Context(), *body)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendSingleResult(c, res, "succeed")
}

func (m *userApi) delete(c *fiber.Ctx) error {
	param := entities.UserLogin{}
	c.ParamsParser(&param)

	res, err := m.serv.Delete(c.Context(), param)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendSingleResult(c, res, "succeed")
}
