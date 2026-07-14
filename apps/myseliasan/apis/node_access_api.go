package apis

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type nodeAccessApi struct {
	access  services.INodeAccessService
	session *middlewares.AccessSessionMidware
	audit   services.IAuditService
}

// NewNodeAccessApi registers per-node DEVICE access-grant management (distinct from the
// /api/nodes/* path matrix, which gates myseliasan's own endpoints). Grants may be viewed
// or changed by the node's owning role (the role that adopted it) OR by any superadmin —
// the latter drives the central RBAC node-access matrix where a superadmin assigns each role
// one of THREE levels on a node, mirroring mymatasan's own roles:
//
//	viewer    canRead     watch live, see that an alert fired
//	operator  canOperate  + review footage, acknowledge alerts, PTZ, talk-back
//	admin     canWrite    everything, including deleting footage
//
// The rungs escalate (admin implies operator implies viewer) and are normalised on save.
// canOperate is new: without it a control-plane user was either a viewer or an admin at the
// node, so a fleet operator who should have been able to review footage but not delete it
// had to be given the power to delete it.
//
//	GET    /api/nodes/access?nodeId=ID  — list the grants on a node (owner or superadmin)
//	GET    /api/nodes/access?roleId=ID  — list a role's grants across nodes (superadmin)
//	POST   /api/nodes/access            — create/update a grant {roleId,nodeId,canRead,canOperate,canWrite}
//	DELETE /api/nodes/access/{id}       — remove a grant
//
// Registered as its own /nodes subrouter; mux matches these before the proxy
// catch-all falls through.
func NewNodeAccessApi(router *mux.Router, auth middlewares.AuthMidware, access services.INodeAccessService, session *middlewares.AccessSessionMidware, audit services.IAuditService) {
	h := &nodeAccessApi{access: access, session: session, audit: audit}
	g := router.PathPrefix("/nodes/access").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.upsert).Methods("POST")
	g.HandleFunc("/{id}", h.delete).Methods("DELETE")
}

func (a *nodeAccessApi) list(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(r.URL.Query().Get("nodeId"))
	roleParam := strings.TrimSpace(r.URL.Query().Get("roleId"))

	// roleId lens — a role's grants across every node — powers the central matrix and
	// is superadmin-only.
	if nodeID == "" && roleParam != "" {
		if !a.requireSuperadmin(w, r) {
			return
		}
		roleId, err := strconv.ParseInt(roleParam, 10, 64)
		if err != nil || roleId <= 0 {
			controllers.SendError(w, controllers.ErrBadRequest, "invalid roleId")
			return
		}
		grants, err := a.access.ListForRole(r.Context(), roleId)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		controllers.SendResult(w, grants, "succeed")
		return
	}

	if nodeID == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "nodeId or roleId is required")
		return
	}
	if !a.requireManager(w, r, nodeID) {
		return
	}
	grants, err := a.access.ListForNode(r.Context(), nodeID)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, grants, "succeed")
}

func (a *nodeAccessApi) upsert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RoleId     int64  `json:"roleId"`
		NodeId     string `json:"nodeId"`
		CanRead    bool   `json:"canRead"`
		CanOperate bool   `json:"canOperate"`
		CanWrite   bool   `json:"canWrite"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	body.NodeId = strings.TrimSpace(body.NodeId)
	if body.RoleId <= 0 || body.NodeId == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "roleId and nodeId are required")
		return
	}
	if !a.requireManager(w, r, body.NodeId) {
		return
	}
	actorUserId := operatorUserId(r)
	grant, err := a.access.Set(r.Context(), entities.NodeAccessGrant{
		RoleId:     body.RoleId,
		NodeId:     body.NodeId,
		CanRead:    body.CanRead,
		CanOperate: body.CanOperate,
		CanWrite:   body.CanWrite,
		CreatedBy:  actorUserId,
		UpdatedBy:  actorUserId,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// The audit trail records the RESOLVED grant, not the raw request: the ladder is
	// normalised on save, so what was asked for and what was stored can differ.
	a.recordGrantAction(r, "node_access.set", body.NodeId, "success",
		fmt.Sprintf("set role %d access on node %s (read=%v operate=%v write=%v)", body.RoleId, body.NodeId, grant.CanRead, grant.CanOperate, grant.CanWrite),
		map[string]any{"roleId": body.RoleId, "canRead": grant.CanRead, "canOperate": grant.CanOperate, "canWrite": grant.CanWrite})
	controllers.SendResult(w, grant, "succeed")
}

// recordGrantAction audits a node-access grant change.
func (a *nodeAccessApi) recordGrantAction(r *http.Request, action, nodeID, outcome, detail string, meta map[string]any) {
	if a.audit == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "node-access",
		TargetId:   nodeID,
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   meta,
		ClientIp:   clientIP(r),
	})
}

func (a *nodeAccessApi) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid grant id")
		return
	}
	// Authorize against the grant's node BEFORE deleting.
	grant, err := a.access.GrantById(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if grant == nil {
		controllers.SendResult(w, map[string]any{"deleted": true}, "succeed") // already gone
		return
	}
	if !a.requireManager(w, r, grant.NodeId) {
		return
	}
	if err := a.access.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.recordGrantAction(r, "node_access.revoke", grant.NodeId, "success",
		fmt.Sprintf("revoked role %d access on node %s", grant.RoleId, grant.NodeId),
		map[string]any{"roleId": grant.RoleId, "grantId": id})
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// requireManager writes a 403 and returns false unless the caller may manage nodeID's
// access — i.e. a superadmin (central RBAC matrix) or the node's owning role.
func (a *nodeAccessApi) requireManager(w http.ResponseWriter, r *http.Request, nodeID string) bool {
	if a.isSuperadmin(r) || a.isOwner(r, nodeID) {
		return true
	}
	controllers.SendError(w, controllers.ErrLimitedAccess, "only a superadmin or the node owner may manage its access")
	return false
}

// requireSuperadmin writes a 403 and returns false unless the caller is a superadmin.
func (a *nodeAccessApi) requireSuperadmin(w http.ResponseWriter, r *http.Request) bool {
	if a.isSuperadmin(r) {
		return true
	}
	controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin only")
	return false
}

// isSuperadmin resolves the caller's role LIVE from the user store (not the token's
// baked roleId), so a just-demoted account can no longer manage node access with a
// stale superadmin session.
func (a *nodeAccessApi) isSuperadmin(r *http.Request) bool {
	return a.session != nil && a.session.IsSuperadmin(r)
}

func (a *nodeAccessApi) isOwner(r *http.Request, nodeID string) bool {
	if a.session == nil {
		return false
	}
	principal, err := a.session.CurrentPrincipal(r)
	if err != nil || principal == nil {
		return false
	}
	owns, err := a.access.OwnsNode(r.Context(), principal.RoleId, nodeID)
	return err == nil && owns
}

func operatorUserId(r *http.Request) int64 {
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		return claims.Id
	}
	return 0
}
