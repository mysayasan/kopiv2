package apis

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/control"
)

// maxProxyBodyBytes caps the request body the commander forwards to a node.
const maxProxyBodyBytes = 8 << 20

type nodeProxyApi struct {
	sender  services.ControlSender
	access  services.INodeAccessService
	session *middlewares.AccessSessionMidware
}

// NewNodeProxyApi registers the commander's reverse tunnel: any method under
//
//	/api/nodes/{id}/proxy/<node-path>
//
// is forwarded over the node's control channel to <node-path> on the node's own
// API (e.g. /api/nodes/abc/proxy/api/settings/users → PUT /api/settings/users on
// node abc). This gives myseliasan the node's exact capability surface; the node
// enforces its own authorization on the re-injected request.
//
// Registered as its own /nodes subrouter; it shares the session-auth middleware and
// is matched after NewNodesApi's specific routes (mux falls through to it when the
// path is /nodes/{id}/proxy/...).
func NewNodeProxyApi(router *mux.Router, auth middlewares.AuthMidware, sender services.ControlSender, access services.INodeAccessService, session *middlewares.AccessSessionMidware) {
	h := &nodeProxyApi{sender: sender, access: access, session: session}
	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	g.PathPrefix("/{id}/proxy").HandlerFunc(h.proxy)
}

func (a *nodeProxyApi) proxy(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]

	// The node path is everything after .../proxy, preserving any query string.
	prefix := "/api/nodes/" + nodeID + "/proxy"
	nodePath := strings.TrimPrefix(r.URL.Path, prefix)
	if nodePath == "" {
		nodePath = "/"
	}
	if r.URL.RawQuery != "" {
		nodePath += "?" + r.URL.RawQuery
	}

	// Authorize: the operator's role must have access to this node. read+write → the
	// node is driven as admin; read-only → viewer; no read → forbidden. The node's
	// owning role (it adopted the node) has full access without an explicit grant.
	roleId, actor := operatorIdentity(r)
	// Use the LIVE role from the user store (not the token's baked roleId) so a
	// just-demoted operator immediately loses node access without a re-login.
	if a.session != nil {
		if p, perr := a.session.CurrentPrincipal(r); perr == nil && p != nil {
			roleId = p.RoleId
		}
	}
	acc, err := a.access.Resolve(r.Context(), roleId, nodeID)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	role := acc.Role()
	if role == "" {
		controllers.SendError(w, controllers.ErrLimitedAccess, "no access to this node")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxProxyBodyBytes))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "request body too large")
		return
	}

	req := control.Request{
		Method:  r.Method,
		Path:    nodePath,
		Role:    role,
		Actor:   actor,
		Headers: map[string]string{},
		Body:    body,
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Headers["Content-Type"] = ct
	}

	resp, err := a.sender.SendRequest(r.Context(), nodeID, req)
	if err != nil {
		if errors.Is(err, services.ErrNodeOffline) {
			controllers.SendError(w, controllers.ErrNotFound, "node is not connected")
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// operatorIdentity returns the requesting operator's RoleId (for the per-node
// access decision) and a human actor string (forwarded to the node for audit
// attribution of tunneled writes).
func operatorIdentity(r *http.Request) (roleId int64, actor string) {
	claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if !ok || claims == nil {
		return 0, ""
	}
	roleId = claims.RoleId
	switch {
	case strings.TrimSpace(claims.Email) != "":
		actor = strings.TrimSpace(claims.Email)
	case strings.TrimSpace(claims.Name) != "":
		actor = strings.TrimSpace(claims.Name)
	default:
		actor = strconv.FormatInt(claims.Id, 10)
	}
	return roleId, actor
}
