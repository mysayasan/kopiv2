package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// Video walls (W3-3b). See services/wall.go for what a wall is and why it is not a cookie.

type wallApi struct {
	serv  services.IWallService
	audit *Auditor
}

// NewWallApi mounts the wall routes under /walls.
//
//	GET  /api/walls                 every wall, plus the grids the server accepts
//	GET  /api/walls/{id}            one wall
//	POST /api/walls                 create
//	POST /api/walls/{id}            update
//	POST /api/walls/{id}/delete     remove
//
// DELETING IS A POST, and that is a considered exception rather than a copy of the case
// routes. The appliance role model keeps the DELETE verb out of every grantable level so
// that destroying FOOTAGE and RECORDS needs an administrator. A wall is neither: it is a
// list of camera ids describing how a room is arranged, rebuilding one takes a minute, and
// requiring an administrator to tidy away a stale wall is friction that buys no safety.
// Spelling it as a POST is what lets the operator who arranges the walls also tidy them,
// without widening a verb that protects something else. It is audited either way.
func NewWallApi(router *mux.Router, serv services.IWallService, audit *Auditor) {
	h := &wallApi{serv: serv, audit: audit}
	g := router.PathPrefix("/walls").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/{id}", h.detail).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("POST")
	g.HandleFunc("/{id}/delete", h.remove).Methods("POST")
}

type wallBody struct {
	Name           string  `json:"name"`
	Grid           string  `json:"grid"`
	CameraIds      []int64 `json:"cameraIds"`
	CycleSeconds   int     `json:"cycleSeconds"`
	AutoPopSeconds int     `json:"autoPopSeconds"`
	IsDefault      bool    `json:"isDefault"`
}

func (a *wallApi) list(w http.ResponseWriter, r *http.Request) {
	walls, err := a.serv.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// The grids ride along with the list so a client offers exactly what the server will
	// accept. The SPA has its own copy for rendering; this is what stops the two drifting
	// into a picker whose entries are refused on save.
	controllers.SendResult(w, map[string]any{"walls": walls, "grids": a.serv.Grids()}, "succeed")
}

func (a *wallApi) detail(w http.ResponseWriter, r *http.Request) {
	wall, err := a.serv.Get(r.Context(), caseIdVar(r, "id"))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, wall, "succeed")
}

func (a *wallApi) create(w http.ResponseWriter, r *http.Request) { a.save(w, r, 0) }

func (a *wallApi) update(w http.ResponseWriter, r *http.Request) { a.save(w, r, caseIdVar(r, "id")) }

func (a *wallApi) save(w http.ResponseWriter, r *http.Request, id int64) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body wallBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	actorID, actorName, _ := auditActor(r)
	wall, err := a.serv.Save(r.Context(), services.WallSave{
		Id: id, Name: body.Name, Grid: body.Grid, CameraIds: body.CameraIds,
		CycleSeconds: body.CycleSeconds, AutoPopSeconds: body.AutoPopSeconds,
		IsDefault: body.IsDefault,
		Actor:     services.CaseActor{Id: actorID, Name: actorName},
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	verb := "created"
	if id > 0 {
		verb = "changed"
	}
	// Audited because "who took that camera off the wall, and when" is a real question
	// after an incident — the wall decides what a room was looking at.
	a.audit.Success(r, services.ActionWallChange, services.TargetWall,
		strconv.FormatInt(wall.Id, 10),
		fmt.Sprintf("%s the %s video wall (%s, %d camera(s))", verb, wall.Name, wall.Grid, len(wall.CameraIds)),
		map[string]any{
			"name": wall.Name, "grid": wall.Grid, "cameras": wall.CameraIds,
			"cycleSeconds": wall.CycleSeconds, "autoPopSeconds": wall.AutoPopSeconds,
			"isDefault": wall.IsDefault,
		})
	controllers.SendResult(w, wall, "succeed")
}

func (a *wallApi) remove(w http.ResponseWriter, r *http.Request) {
	id := caseIdVar(r, "id")
	name := ""
	if existing, err := a.serv.Get(r.Context(), id); err == nil && existing != nil {
		name = existing.Name
	}
	if err := a.serv.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionWallDelete, services.TargetWall, strconv.FormatInt(id, 10),
		strings.TrimSpace(fmt.Sprintf("deleted the %s video wall", name)),
		map[string]any{"name": name})
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}
