package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type eventApi struct {
	events dbsql.IGenericRepo[entities.AccessEvent]
}

// NewEventApi exposes the access log.
//
// READ ONLY, and there is no delete route anywhere in this app. The log is the product: "the fire
// door opened at 02:14 and nobody badged" is a fact somebody may want gone, and a system that can
// quietly rewrite it is not evidence of anything. Retention, when it lands, has to be a scheduled
// policy with its own audit trail rather than a button.
func NewEventApi(router *mux.Router, db dbsql.IDbCrud) {
	a := &eventApi{events: dbsql.NewGenericRepo[entities.AccessEvent](db)}
	g := router.PathPrefix("/events").Subrouter()
	g.HandleFunc("", a.list).Methods("GET")
}

// list returns access decisions newest-first, optionally narrowed by door, holder or outcome.
//
// The denials matter as much as the grants — a denied unknown credential at 03:00 on a perimeter
// door is the single most valuable row this table holds — so nothing here filters them out by
// default.
func (a *eventApi) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := uint64(200)
	if v, err := strconv.ParseUint(q.Get("limit"), 10, 64); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	var offset uint64
	if v, err := strconv.ParseUint(q.Get("offset"), 10, 64); err == nil {
		offset = v
	}

	var filters []sqldataenums.Filter
	if v, err := strconv.ParseInt(q.Get("doorId"), 10, 64); err == nil && v > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "DoorId", Compare: sqldataenums.Equal, Value: v})
	}
	if v, err := strconv.ParseInt(q.Get("holderId"), 10, 64); err == nil && v > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "HolderId", Compare: sqldataenums.Equal, Value: v})
	}
	switch q.Get("decision") {
	case entities.DecisionGranted:
		filters = append(filters, sqldataenums.Filter{FieldName: "Decision", Compare: sqldataenums.Equal, Value: entities.DecisionGranted})
	case entities.DecisionDenied:
		filters = append(filters, sqldataenums.Filter{FieldName: "Decision", Compare: sqldataenums.Equal, Value: entities.DecisionDenied})
	}
	if reason := q.Get("reason"); reason != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "Reason", Compare: sqldataenums.Equal, Value: reason})
	}
	if v, err := strconv.ParseInt(q.Get("since"), 10, 64); err == nil && v > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "At", Compare: sqldataenums.GreaterThanOrEqualTo, Value: v})
	}

	rows, total, err := a.events.Get(r.Context(), "", limit, offset, filters,
		[]sqldataenums.Sorter{{FieldName: "At", Sort: sqldataenums.DESC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, limit, offset, total)
}

type setupApi struct {
	handlers *sharedapis.SetupHandlers
}

// NewSetupApi registers the first-run wizard flag on the same shared `setup.state` contract the
// other three apps use — a property of the INSTALL, not of a browser's localStorage.
func NewSetupApi(router *mux.Router, setup sharedservices.ISetupStateService) {
	a := &setupApi{handlers: sharedapis.NewSetupHandlers(setup)}
	g := router.PathPrefix("/setup").Subrouter()
	g.HandleFunc("/state", a.handlers.State).Methods("GET")
	g.HandleFunc("/complete", a.complete).Methods("POST")
}

func (a *setupApi) complete(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}
	a.handlers.Complete(w, r)
}
