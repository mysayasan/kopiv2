package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares/timeout"
)

// ApiEndpointRbacApi struct
type apiEndpointRbacApi struct {
	auth middlewares.AuthMiddleware
	serv services.IApiEndpointRbacService
}

// Create ApiEndpointRbacApi
func NewApiEndpointRbacApi(
	router fiber.Router,
	auth middlewares.AuthMiddleware,
	serv services.IApiEndpointRbacService) {
	handler := &apiEndpointRbacApi{
		auth: auth,
		serv: serv,
	}

	Rbac := *middlewares.NewRbac()

	group := router.Group("endpoint-rbac")
	group.Get("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.get, 60*1000*time.Millisecond)).Name("get")
	group.Get("validate/me", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.getValidate, 60*1000*time.Millisecond)).Name("get_validate")
	group.Post("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.post, 60*1000*time.Millisecond)).Name("create")
	group.Put("/", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.put, 60*1000*time.Millisecond)).Name("update")
	group.Delete("/:id", auth.JwtHandler(), Rbac.ApiHandler(), timeout.NewWithContext(handler.delete, 60*1000*time.Millisecond)).Name("delete")
}

func (m *apiEndpointRbacApi) get(c *fiber.Ctx) error {

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

func (m *apiEndpointRbacApi) getValidate(c *fiber.Ctx) error {

	user := c.Locals("user").(*jwt.Token)
	claims := &middlewares.JwtCustomClaimsModel{}
	tmp, err := json.Marshal(user.Claims)
	if err != nil {
		return controllers.SendError(c, controllers.ErrParseFailed, err.Error())
	}
	_ = json.Unmarshal(tmp, claims)

	host := c.Query("host")
	path := c.Query("path")

	ctx := c.UserContext()
	if ctx == nil {
		ctx = context.Background()
	}

	res, err := m.serv.Validate(ctx, host, path, uint64(claims.RoleId))
	if err != nil {
		return controllers.SendError(c, controllers.ErrNotFound, err.Error())
	}

	return controllers.SendResult(c, res, "succeed")
}

func (m *apiEndpointRbacApi) post(c *fiber.Ctx) error {
	body := new(entities.ApiEndpointRbac)

	if err := c.BodyParser(body); err != nil {
		return controllers.SendError(c, controllers.ErrParseFailed, err.Error())
	}

	log.Info(fmt.Sprintf("%v", body))

	res, err := m.serv.Create(c.Context(), *body)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendResult(c, res, "succeed")
}

func (m *apiEndpointRbacApi) put(c *fiber.Ctx) error {
	body := new(entities.ApiEndpointRbac)

	if err := c.BodyParser(body); err != nil {
		return controllers.SendError(c, controllers.ErrParseFailed, err.Error())
	}

	log.Info(fmt.Sprintf("%v", body))

	res, err := m.serv.Update(c.Context(), *body)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendResult(c, res, "succeed")
}

func (m *apiEndpointRbacApi) delete(c *fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	log.Info(id)
	// param := entities.ApiEndpointRbac{}
	// c.ParamsParser(&param)

	res, err := m.serv.Delete(c.Context(), id)
	if err != nil {
		return controllers.SendError(c, controllers.ErrInternalServerError, err.Error())
	}

	return controllers.SendResult(c, res, "succeed")
}
