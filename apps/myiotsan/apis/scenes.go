package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type scenesApi struct {
	scenes *services.SceneService
}

// NewScenesApi registers scenes: named groups of device commands, and running them.
//
// Reading and authoring a scene are ordinary CRUD; RUNNING one is actuation, and it is admin-only
// for the same reason a single command is — running a scene commands real devices, and the RBAC
// matrix (services.Policy) denies /api/scenes/*/run to everyone below admin.
func NewScenesApi(router *mux.Router, scenes *services.SceneService) {
	h := &scenesApi{scenes: scenes}

	g := router.PathPrefix("/scenes").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/{id}", h.detail).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/{id}/run", h.run).Methods("POST")
}

func (a *scenesApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.scenes.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *scenesApi) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	detail, err := a.scenes.Detail(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, detail, "succeed")
}

func (a *scenesApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.SaveSceneRequest
	if !decode(w, r, &body) {
		return
	}
	scene, err := a.scenes.Create(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, scene, "succeed")
}

func (a *scenesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.SaveSceneRequest
	if !decode(w, r, &body) {
		return
	}
	scene, err := a.scenes.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, scene, "succeed")
}

func (a *scenesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.scenes.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

// run fans the scene out through the command path. It returns a per-action report; a partial
// failure is a 200 with some actions failed, not an error — the operator needs to see WHICH acted.
func (a *scenesApi) run(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	result, err := a.scenes.Run(r.Context(), id, actorId(r), actorName(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}
