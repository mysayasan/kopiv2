package apis

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type localAuthApi struct {
	cfg      LocalAuthConfig
	userServ services.ILocalUserService
}

// NewLocalAuthApi registers the authenticated-user self-service routes: the
// session probe the SPA reads to discover the forced-change flag, and the
// password-change endpoint that clears it. Both stay reachable while a user is
// gated by NewLocalBasicAuth (see isPasswordChangeAllowedPath).
func NewLocalAuthApi(router *mux.Router, cfg LocalAuthConfig, userServ services.ILocalUserService) {
	handler := &localAuthApi{cfg: cfg, userServ: userServ}
	group := router.PathPrefix("/auth").Subrouter()
	group.HandleFunc("/session", handler.session).Methods("GET")
	group.HandleFunc("/change-password", handler.changePassword).Methods("POST")
}

// session returns the current authenticated identity, including whether a forced
// password change is pending, so the SPA can route to the change screen.
func (a *localAuthApi) session(w http.ResponseWriter, r *http.Request) {
	user, ok := LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "not authenticated")
		return
	}
	controllers.SendResult(w, user, "succeed")
}

// changePassword verifies the current password and sets a new one for the
// authenticated user, rotating the session cookie so the old credential's
// session hash is invalidated.
func (a *localAuthApi) changePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "not authenticated")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body services.ChangeLocalUserPasswordRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	updated, err := a.userServ.ChangePassword(r.Context(), user.Id, body.CurrentPassword, body.NewPassword)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	setLocalAuthCookie(w, a.cfg, updated)
	controllers.SendResult(w, updated, "succeed")
}
