package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type settingsApi struct {
	serv     services.IRuntimeSettingsService
	userServ services.ILocalUserService
}

// NewSettingsApi registers runtime settings routes.
func NewSettingsApi(router *mux.Router, serv services.IRuntimeSettingsService, userServ services.ILocalUserService) {
	handler := &settingsApi{serv: serv, userServ: userServ}
	group := router.PathPrefix("/settings").Subrouter()

	group.HandleFunc("/runtime", handler.getRuntime).Methods("GET")
	group.HandleFunc("/runtime", handler.saveRuntime).Methods("PUT")
	group.HandleFunc("/runtime/reset", handler.resetRuntime).Methods("POST")
	group.HandleFunc("/users", handler.listUsers).Methods("GET")
	group.HandleFunc("/users", handler.createUser).Methods("POST")
	group.HandleFunc("/users/{id}", handler.updateUser).Methods("PUT")
	group.HandleFunc("/users/{id}/password", handler.resetUserPassword).Methods("POST")
	group.HandleFunc("/users/{id}", handler.deleteUser).Methods("DELETE")
}

func (a *settingsApi) getRuntime(w http.ResponseWriter, r *http.Request) {
	settings, err := a.serv.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) saveRuntime(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.RuntimeSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	settings, err := a.serv.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) resetRuntime(w http.ResponseWriter, r *http.Request) {
	settings, err := a.serv.Reset(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) listUsers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	limit, offset := readPaging(r)
	users, total, err := a.userServ.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": users,
		"total": total,
	}, "succeed")
}

func (a *settingsApi) createUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.CreateLocalUserRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	user, err := a.userServ.Create(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) updateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.UpdateLocalUserRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	user, err := a.userServ.Update(r.Context(), id, body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.ResetLocalUserPasswordRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	user, err := a.userServ.ResetPassword(r.Context(), id, body.Password)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	count, err := a.userServ.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}

func (a *settingsApi) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, ok := LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "admin user is required")
		return false
	}
	return true
}

func readPaging(r *http.Request) (uint64, uint64) {
	limit := uint64(100)
	offset := uint64(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
			offset = parsed
		}
	}
	return limit, offset
}

func readID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}
