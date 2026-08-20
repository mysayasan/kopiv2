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

type fleetPolicyApi struct {
	policies   services.IFleetPolicyService
	reconciler *services.FleetPolicyReconciler
	session    *middlewares.AccessSessionMidware
	audit      services.IAuditService
}

// NewFleetPolicyApi registers fleet configuration policy: what the estate's node settings
// ought to be, and how far each node currently is from it.
//
// Reading the compliance report is available to any role that can reach the fleet — it is
// a health view and hiding it helps nobody. WRITING a policy is a superadmin power, on the
// same reasoning as a correlation rule: a policy is an estate-wide control, and whoever
// can write one can write one that turns every monitor in the fleet off.
func NewFleetPolicyApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, policies services.IFleetPolicyService, reconciler *services.FleetPolicyReconciler, audit services.IAuditService) {
	h := &fleetPolicyApi{policies: policies, reconciler: reconciler, session: session, audit: audit}
	g := router.PathPrefix("/fleet-policies").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	// The catalog: what CAN be governed. Served rather than hardcoded in the SPA so the
	// screen can never offer a field the server would refuse — the two would drift, and
	// the operator would meet the drift as a save error on a form they already filled in.
	g.HandleFunc("/catalog", h.catalog).Methods("GET")
	g.HandleFunc("/compliance", h.compliance).Methods("GET")
	g.HandleFunc("/compliance/refresh", h.requireSuper(h.refresh)).Methods("POST")
	g.HandleFunc("/compliance/{nodeId}", h.requireSuper(h.checkNode)).Methods("POST")
	g.HandleFunc("", h.requireSuper(h.save)).Methods("POST")
	g.HandleFunc("/{id}", h.requireSuper(h.remove)).Methods("DELETE")
}

func (a *fleetPolicyApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmins only")
			return
		}
		next(w, r)
	}
}

func (a *fleetPolicyApi) list(w http.ResponseWriter, r *http.Request) {
	items, err := a.policies.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *fleetPolicyApi) catalog(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{
		"sections":  services.PolicySections(),
		"nodeKinds": services.PolicyNodeKinds(),
	}, "succeed")
}

// compliance serves the LAST completed pass rather than sweeping on demand.
//
// A sweep is one tunneled round trip per section per node; doing it inside a page load
// would make the screen as slow as the slowest appliance in the estate, and a fleet with
// one wedged node would appear broken. The refresh endpoint exists for when the operator
// genuinely wants to wait.
func (a *fleetPolicyApi) compliance(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.reconciler.Last(), "succeed")
}

func (a *fleetPolicyApi) refresh(w http.ResponseWriter, r *http.Request) {
	out, err := a.reconciler.ReconcileAll(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, out, "succeed")
}

func (a *fleetPolicyApi) checkNode(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["nodeId"]
	out, err := a.reconciler.ReconcileNode(r.Context(), nodeID)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, out, "succeed")
}

func (a *fleetPolicyApi) save(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var body services.SaveFleetPolicyRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	actorID, _, _ := auditActor(r)
	saved, err := a.policies.Save(r.Context(), body, actorID)
	if err != nil {
		a.record(r, services.ActionPolicySave, body.Name, services.OutcomeError, err.Error(), nil)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Enforce is the field worth being able to reconstruct later: it is the difference
	// between a policy that reported and one that reached out and changed fifty machines.
	a.record(r, services.ActionPolicySave, saved.Policy.Name, services.OutcomeSuccess,
		"fleet policy saved", map[string]any{
			"policyId": saved.Policy.Id,
			"scope":    saved.Policy.Scope,
			"target":   saved.Policy.TargetId,
			"nodeKind": saved.Policy.NodeKind,
			"enforce":  saved.Policy.Enforce,
			"enabled":  saved.Policy.Enabled,
			"settings": len(saved.Items),
		})
	controllers.SendResult(w, saved, "succeed")
}

func (a *fleetPolicyApi) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	if err := a.policies.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, services.ActionPolicyDelete, strconv.FormatInt(id, 10), services.OutcomeSuccess,
		"fleet policy deleted", map[string]any{"policyId": id})
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *fleetPolicyApi) record(r *http.Request, action, target, outcome, detail string, meta map[string]any) {
	if a.audit == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "policy",
		TargetId:   target,
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   meta,
	})
}
