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

// UserRoleApi struct
type userRoleApi struct {
	auth middlewares.AuthMiddleware
	serv services.IUserRoleService
}

// Create UserRoleApi
func NewUserRoleApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	serv services.IUserRoleService) {
	handler := &userRoleApi{
		auth: auth,
		serv: serv,
	}

	Rbac := *middlewares.NewRbac()

	group := router.Group("user/role")
	group.Get("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.get, 60*1000*time.Millisecond)).Name("get")
	group.Get("/group/:id", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.getByGroup, 60*1000*time.Millisecond)).Name("get_by_group")
	group.Post("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.post, 60*1000*time.Millisecond)).Name("create")
	group.Put("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.put, 60*1000*time.Millisecond)).Name("update")
	group.Delete("/:id", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.delete, 60*1000*time.Millisecond)).Name("delete")
}

func (m *userRoleApi) get(c *fiber.Ctx) error {

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

func (m *userRoleApi) getByGroup(c *fiber.Ctx) error {
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := m.serv.GetByGroup(ctx, id)
	if err != nil {
		return controllers.SendError(c, controllers.ErrNotFound, err.Error())
	}

	return controllers.SendSingleResult(c, res)
}

func (m *userRoleApi) post(c *fiber.Ctx) error {
	body := new(entities.UserRole)

	if err := c.BodyParser(body); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("%v", body))

	res, err := m.serv.Create(c.Context(), *body)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendSingleResult(c, res, "succeed")
}

func (m *userRoleApi) put(c *fiber.Ctx) error {
	body := new(entities.UserRole)

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

func (m *userRoleApi) delete(c *fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	log.Info(id)
	// param := entities.UserRole{}
	// c.ParamsParser(&param)

	res, err := m.serv.Delete(c.Context(), id)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendSingleResult(c, res, "succeed")
}
