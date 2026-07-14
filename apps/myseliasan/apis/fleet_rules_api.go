package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type fleetRulesApi struct {
	correlator *services.Correlator
	session    *middlewares.AccessSessionMidware
}

// NewFleetRulesApi registers cross-domain correlation rules — the rules that span NODES rather
// than living on one.
//
// Writing them is a superadmin power. A correlation rule is a security control that spans the
// whole estate, and the person who can write one can also write one that never fires.
func NewFleetRulesApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, correlator *services.Correlator) {
	h := &fleetRulesApi{correlator: correlator, session: session}
	g := router.PathPrefix("/fleet-rules").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	// WRITING a correlation rule is a superadmin power. A rule that spans the whole estate is a
	// security control, and whoever can write one can also write one that never fires.
	g.HandleFunc("", h.requireSuper(h.save)).Methods("POST")
	g.HandleFunc("/{id}", h.requireSuper(h.remove)).Methods("DELETE")
}

func (a *fleetRulesApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmins only")
			return
		}
		next(w, r)
	}
}

func (a *fleetRulesApi) list(w http.ResponseWriter, r *http.Request) {
	rules, err := a.correlator.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rules}, "succeed")
}

func (a *fleetRulesApi) save(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var body services.SaveFleetRuleRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	rule, err := a.correlator.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, rule, "succeed")
}

func (a *fleetRulesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	if err := a.correlator.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}
