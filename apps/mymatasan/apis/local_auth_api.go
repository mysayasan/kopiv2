package apis

import (
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
)

// NewLocalAuthApi registers the authenticated-user self-service routes (the session probe
// the SPA reads to discover the forced-change flag, and the password-change endpoint that
// clears it) on the shared appliance implementation, bound to mymatasan's session cookie.
func NewLocalAuthApi(router *mux.Router, userServ services.ILocalUserService) {
	sharedapis.NewLocalAuthApi(router, localAuthConfig(), userServ)
}
