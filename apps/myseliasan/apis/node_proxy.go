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
	audit   services.IAuditService
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
func NewNodeProxyApi(router *mux.Router, auth middlewares.AuthMidware, sender services.ControlSender, access services.INodeAccessService, session *middlewares.AccessSessionMidware, audit services.IAuditService) {
	h := &nodeProxyApi{sender: sender, access: access, session: session, audit: audit}
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

	// Authorize: the user's role must have access to this node. The grant is an escalation
	// ladder and each rung names the role the tunnel ASSERTS at the node — viewer, operator,
	// or admin. No read at all is forbidden here. The node's owning role (it adopted the
	// node) has full access without an explicit grant.
	//
	// What is sent is a role NAME, not a permission set. The node resolves that name against
	// its OWN roles and evaluates its OWN matrix, so a compromised control plane cannot
	// assert capabilities the node never granted. See mymatasan's control dispatcher.
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
		a.recordCommand(r, nodeID, nodePath, "error", 0)
		// Both "never connected" and "dropped mid-command" mean the node is unreachable
		// now; surface a fast, clear error instead of a generic 500 (or a 30s hang).
		if errors.Is(err, services.ErrNodeOffline) || errors.Is(err, services.ErrNodeDisconnected) {
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
	a.recordCommand(r, nodeID, nodePath, outcomeForStatus(status), status)
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// recordCommand audits a MUTATING command tunneled to a node (POST/PUT/PATCH/DELETE)
// — this is the single choke point through which remote wipe / factory-reset /
// settings changes reach a node. Read-only tunneled traffic (GET/HEAD) is not
// audited to avoid drowning the trail in routine page loads.
func (a *nodeProxyApi) recordCommand(r *http.Request, nodeID, nodePath, outcome string, status int) {
	if a.audit == nil {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return
	}
	// Strip any query string from the recorded path for a stable action detail.
	cleanPath := nodePath
	if i := strings.IndexByte(cleanPath, '?'); i >= 0 {
		cleanPath = cleanPath[:i]
	}
	actorID, actorLabel, roleID := auditActor(r)
	meta := map[string]any{"method": r.Method, "path": cleanPath}
	if status > 0 {
		meta["nodeStatus"] = status
	}
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     "node.command",
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "node",
		TargetId:   nodeID,
		Outcome:    outcome,
		Detail:     r.Method + " " + cleanPath,
		Metadata:   meta,
		ClientIp:   clientIP(r),
	})
}

// outcomeForStatus maps an HTTP status from the node into an audit outcome.
func outcomeForStatus(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "success"
	case status == http.StatusForbidden || status == http.StatusUnauthorized:
		return "denied"
	default:
		return "error"
	}
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
