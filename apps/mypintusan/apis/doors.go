// Package apis is mypintusan's HTTP surface.
//
// It is deliberately small at this stage: doors and their live state, people and their badges, the
// access log, and lockdown. Everything here is authorised by the shared accessrbac matrix declared
// in services.Policy(), which is deny-by-default — a route that is not in that catalog is a route
// nobody can see they are not granting.
package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Unlocker is the runtime seam the API uses to open a door. It is an interface so the HTTP layer
// cannot reach the actuator directly — every unlock has to pass through the controller's audited
// chokepoint.
type Unlocker interface {
	Unlock(ctx context.Context, door entities.Door, actor int64, actorName string) error
	SetLockdown(ctx context.Context, on bool)
	Lockdown() bool
}

type doorApi struct {
	store  *services.SQLStore
	rt     Unlocker
	doors  dbsql.IGenericRepo[entities.Door]
	reader dbsql.IGenericRepo[entities.Reader]
}

// NewDoorApi registers the door surface.
func NewDoorApi(router *mux.Router, store *services.SQLStore, rt Unlocker, db dbsql.IDbCrud) {
	a := &doorApi{
		store:  store,
		rt:     rt,
		doors:  dbsql.NewGenericRepo[entities.Door](db),
		reader: dbsql.NewGenericRepo[entities.Reader](db),
	}

	g := router.PathPrefix("/doors").Subrouter()
	g.HandleFunc("", a.list).Methods("GET")
	g.HandleFunc("/{id:[0-9]+}", a.get).Methods("GET")
	// The unlock path is its OWN matrix entry, separate from /api/doors, because seeing a door and
	// opening it are different powers held by different people.
	g.HandleFunc("/{id:[0-9]+}/unlock", a.unlock).Methods("POST")

	r := router.PathPrefix("/readers").Subrouter()
	r.HandleFunc("", a.listReaders).Methods("GET")
}

func (a *doorApi) list(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.doors.Get(r.Context(), "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, uint64(len(rows)), 0, total)
}

func (a *doorApi) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad door id")
		return
	}
	door, err := a.store.Door(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	if door == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such door")
		return
	}
	controllers.SendResult(w, door)
}

func (a *doorApi) listReaders(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.reader.Get(r.Context(), "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, uint64(len(rows)), 0, total)
}

// unlock opens a door remotely.
//
// The actor is taken from the SESSION, never from the request body. A remote unlock with an
// attacker-supplied actor name would put a chosen name in the audit log next to a door opening,
// which is worse than no name at all — it is a forged record.
func (a *doorApi) unlock(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "sign in first")
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad door id")
		return
	}

	door, err := a.store.Door(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	if door == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such door")
		return
	}

	if err := a.rt.Unlock(r.Context(), *door, user.Id, user.Username); err != nil {
		// The refusal is already in the access log by the time this returns — the controller
		// records it either way, so a denied remote unlock is as auditable as a granted one.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"doorId": door.Id, "unlocked": true})
}

type lockdownApi struct {
	rt Unlocker
}

// NewLockdownApi registers the site-seal control.
func NewLockdownApi(router *mux.Router, rt Unlocker) {
	a := &lockdownApi{rt: rt}
	g := router.PathPrefix("/lockdown").Subrouter()
	g.HandleFunc("", a.state).Methods("GET")
	g.HandleFunc("", a.set).Methods("POST")
}

func (a *lockdownApi) state(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"lockdown": a.rt.Lockdown()})
}

// set seals or releases the site.
//
// Admin-only on top of the matrix. Lockdown is the one control that stops a building working: it
// cannot trap anybody, because egress is hardware and no software here can override it, but an
// operator who triggers it during a fire drill has turned a nuisance into an incident.
func (a *lockdownApi) set(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}
	var body struct {
		Lockdown bool `json:"lockdown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "expected {\"lockdown\": true|false}")
		return
	}
	a.rt.SetLockdown(r.Context(), body.Lockdown)
	controllers.SendResult(w, map[string]any{"lockdown": a.rt.Lockdown()})
}
