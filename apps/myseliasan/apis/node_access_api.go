package apis

import (
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
	access services.INodeAccessService
}

// NewNodeAccessApi registers per-node access-grant management. A node's grants may
// only be viewed or changed by the node's owning role (the role that adopted it):
//
//	GET    /api/nodes/access?nodeId=ID  — list the grants on a node
//	POST   /api/nodes/access            — create/update a grant {roleId,nodeId,canRead,canWrite}
//	DELETE /api/nodes/access/{id}       — remove a grant
//
// Registered as its own /nodes subrouter; mux matches these before the proxy
// catch-all falls through.
func NewNodeAccessApi(router *mux.Router, auth middlewares.AuthMidware, access services.INodeAccessService) {
	h := &nodeAccessApi{access: access}
	g := router.PathPrefix("/nodes/access").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.upsert).Methods("POST")
	g.HandleFunc("/{id}", h.delete).Methods("DELETE")
}

func (a *nodeAccessApi) list(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(r.URL.Query().Get("nodeId"))
	if nodeID == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "nodeId is required")
		return
	}
	if !a.requireOwner(w, r, nodeID) {
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
		RoleId   int64  `json:"roleId"`
		NodeId   string `json:"nodeId"`
		CanRead  bool   `json:"canRead"`
		CanWrite bool   `json:"canWrite"`
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
	if !a.requireOwner(w, r, body.NodeId) {
		return
	}
	actorUserId := operatorUserId(r)
	grant, err := a.access.Set(r.Context(), entities.NodeAccessGrant{
		RoleId:    body.RoleId,
		NodeId:    body.NodeId,
		CanRead:   body.CanRead,
		CanWrite:  body.CanWrite,
		CreatedBy: actorUserId,
		UpdatedBy: actorUserId,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, grant, "succeed")
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
	if !a.requireOwner(w, r, grant.NodeId) {
		return
	}
	if err := a.access.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// requireOwner writes a 403 and returns false unless the caller's role owns nodeID.
func (a *nodeAccessApi) requireOwner(w http.ResponseWriter, r *http.Request, nodeID string) bool {
	if a.isOwner(r, nodeID) {
		return true
	}
	controllers.SendError(w, controllers.ErrLimitedAccess, "only the node owner may manage its access")
	return false
}

func (a *nodeAccessApi) isOwner(r *http.Request, nodeID string) bool {
	claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if !ok || claims == nil {
		return false
	}
	owns, err := a.access.OwnsNode(r.Context(), claims.RoleId, nodeID)
	return err == nil && owns
}

func operatorUserId(r *http.Request) int64 {
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		return claims.Id
	}
	return 0
}
