package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type profilesApi struct {
	profiles *services.ProfileService
	ingest   *services.Ingest
}

// NewProfilesApi registers the device-type catalog.
func NewProfilesApi(router *mux.Router, profiles *services.ProfileService, ingest *services.Ingest) {
	h := &profilesApi{profiles: profiles, ingest: ingest}
	g := router.PathPrefix("/profiles").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/{id}", h.detail).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
}

func (a *profilesApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.profiles.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *profilesApi) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	detail, err := a.profiles.Detail(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, detail, "succeed")
}

func (a *profilesApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.SaveProfileRequest
	if !decode(w, r, &body) {
		return
	}
	detail, err := a.profiles.Create(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, detail, "succeed")
}

func (a *profilesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.SaveProfileRequest
	if !decode(w, r, &body) {
		return
	}
	detail, err := a.profiles.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// An edited deadband must take effect on the NEXT message, not the next restart.
	a.ingest.InvalidateProfile(id)
	controllers.SendResult(w, detail, "succeed")
}

func (a *profilesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.profiles.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.ingest.InvalidateProfile(id)
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func parseInt(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}
