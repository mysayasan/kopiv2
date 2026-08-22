package apis

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type fleetRolloutApi struct {
	rollouts services.IFleetRolloutService
	session  *middlewares.AccessSessionMidware
	audit    services.IAuditService
}

// NewFleetRolloutApi registers staged version rollout (W2-5):
//
//	GET    /api/fleet-rollouts          — every rollout, newest first
//	POST   /api/fleet-rollouts          — plan one (draft)
//	GET    /api/fleet-rollouts/{id}     — one rollout with its per-node progress
//	POST   /api/fleet-rollouts/{id}/start
//	POST   /api/fleet-rollouts/{id}/cancel
//
// READING is open to any role that can reach the fleet: which appliances are on which
// version, and whether an upgrade is in trouble, is health information and hiding it helps
// nobody. WRITING is superadmin-only, on the same reasoning as a fleet policy — starting a
// rollout replaces the software on every recorder in the estate, and there is no undo.
func NewFleetRolloutApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, rollouts services.IFleetRolloutService, audit services.IAuditService) {
	h := &fleetRolloutApi{rollouts: rollouts, session: session, audit: audit}
	g := router.PathPrefix("/fleet-rollouts").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.requireSuper(h.plan)).Methods("POST")
	g.HandleFunc("/{id}", h.get).Methods("GET")
	g.HandleFunc("/{id}/start", h.requireSuper(h.start)).Methods("POST")
	g.HandleFunc("/{id}/cancel", h.requireSuper(h.cancel)).Methods("POST")
}

func (a *fleetRolloutApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmins only")
			return
		}
		next(w, r)
	}
}

func (a *fleetRolloutApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.rollouts.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows, "succeed")
}

func (a *fleetRolloutApi) get(w http.ResponseWriter, r *http.Request) {
	id, ok := a.rolloutID(w, r)
	if !ok {
		return
	}
	view, err := a.rollouts.Get(r.Context(), id)
	if err != nil {
		a.sendServiceError(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *fleetRolloutApi) plan(w http.ResponseWriter, r *http.Request) {
	var body services.RolloutPlanRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	actorID, _, _ := auditActor(r)
	view, err := a.rollouts.Plan(r.Context(), body, actorID)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *fleetRolloutApi) start(w http.ResponseWriter, r *http.Request) {
	id, ok := a.rolloutID(w, r)
	if !ok {
		return
	}
	view, err := a.rollouts.Start(r.Context(), id)
	if err != nil {
		a.sendServiceError(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *fleetRolloutApi) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := a.rolloutID(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
	view, err := a.rollouts.Cancel(r.Context(), id, strings.TrimSpace(body.Reason))
	if err != nil {
		a.sendServiceError(w, err)
		return
	}
	controllers.SendResult(w, view, "succeed")
}

func (a *fleetRolloutApi) rolloutID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid rollout id")
		return 0, false
	}
	return id, true
}

// sendServiceError keeps "no such rollout" a 404 rather than a 500, so a stale link in a
// browser tab reads as gone rather than as the control plane being broken.
func (a *fleetRolloutApi) sendServiceError(w http.ResponseWriter, err error) {
	if err == services.ErrRolloutNotFound {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendError(w, controllers.ErrBadRequest, err.Error())
}
