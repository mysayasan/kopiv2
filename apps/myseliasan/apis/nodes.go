package apis

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type nodesApi struct {
	registry services.INodeRegistry
	audit    services.IAuditService
	rejects  rejectTracker        // set post-startup via SetRejectTracker; may be nil
	logf     func(string, ...any) // server-log sink; may be nil
}

// NewNodesApi registers control-plane node-management endpoints.
//
// Operator routes (require a myseliasan session):
//
//	GET  /nodes               — list adopted nodes
//	GET  /nodes/fleet-status  — fleet liveness + cert-health rollup (counts)
//	POST /nodes/scan          — discover unpaired nodes on the LAN
//	POST /nodes/adopt         — adopt a node (by ip+port+claim code)
//	POST /nodes/{id}/release  — release an adopted node
//	GET  /nodes/fleet-key     — show the fleet key
//	POST /nodes/fleet-key     — generate (rotate) the fleet key
//
// Public route (called by a node, authenticated by a fleet-key assertion):
//
//	POST /nodes/self-dropped  — a node reports it unpaired itself
//
// rejectTracker exposes the control server's list of refused (stranded) node connections and
// a way to forget one. Satisfied by *services.ControlServer, injected after it is built.
type rejectTracker interface {
	Unrecognized() []services.RejectedNode
	ForgetRejected(nodeID string)
}

func NewNodesApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, registry services.INodeRegistry, audit services.IAuditService, logf func(string, ...any)) *nodesApi {
	h := &nodesApi{registry: registry, audit: audit, logf: logf}

	// Public self-drop notice — node has no session, carries its own signature.
	router.HandleFunc("/nodes/self-dropped", h.selfDropped).Methods("POST")
	// Public certificate enrollment — node-initiated, authenticated by its pairing
	// token (issued at adoption). Signs the node's CSR and returns its cert + CA root.
	router.HandleFunc("/nodes/enroll", h.enroll).Methods("POST")

	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	// Axis-1 RBAC: viewers can list nodes (GET) but not adopt/release/rotate keys.
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/fleet-status", h.fleetStatus).Methods("GET")
	// Stranded/refused nodes dialing the control channel with a valid cert but no record.
	// Registered before the "/{id}" routes; distinct literal segment, so no conflict.
	g.HandleFunc("/unrecognized", h.listUnrecognized).Methods("GET")
	g.HandleFunc("/scan", h.scan).Methods("POST")
	g.HandleFunc("/adopt", h.adopt).Methods("POST")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}/position", h.updatePosition).Methods("PUT")
	// Assign (or clear) the building an appliance resides in — the building-first map's alternative
	// to placing a node's own pin.
	g.HandleFunc("/{id}/building", h.updateBuilding).Methods("PUT")
	// Toggle a node's certificate auto-renew gate. Off (default) lets the cert lapse.
	g.HandleFunc("/{id}/auto-renew", h.setAutoRenew).Methods("PUT")
	g.HandleFunc("/fleet-key", h.fleetKey).Methods("GET")
	g.HandleFunc("/fleet-key", h.generateFleetKey).Methods("POST")
	g.HandleFunc("/{id}/release", h.release).Methods("POST")
	// Block a stranded node: revoke its cert so it can no longer enroll or connect. Forget
	// removes it from the unrecognized list without revoking (operator dismissed it).
	g.HandleFunc("/{id}/block", h.blockNode).Methods("POST")
	g.HandleFunc("/{id}/forget", h.forgetNode).Methods("POST")
	return h
}

// SetRejectTracker wires the control server's rejected-connection view in after it is built
// (the control server is constructed later than this API in app startup).
func (a *nodesApi) SetRejectTracker(rt rejectTracker) { a.rejects = rt }

// recordNodeAction writes an audit entry for a node-targeted operator action,
// attributing it to the caller's session identity. Best-effort (never blocks the
// request); no-op if auditing isn't wired.
func (a *nodesApi) recordNodeAction(r *http.Request, action, nodeID, outcome, detail string, meta map[string]any) {
	if a.audit == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "node",
		TargetId:   nodeID,
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   meta,
		ClientIp:   clientIP(r),
	})
}

func (a *nodesApi) list(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.registry.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, nodes, "succeed")
}

// fleetStatus returns a rollup of the fleet's liveness and certificate health so the
// dashboard can show "X online / Y lost / Z certs expiring" at a glance.
func (a *nodesApi) fleetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := a.registry.FleetStatus(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, status, "succeed")
}

func (a *nodesApi) scan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TimeoutMs int `json:"timeoutMs"`
	}
	_ = decodeJSON(w, r, &body)
	timeout := time.Duration(body.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	found, err := a.registry.Scan(r.Context(), timeout)
	if err != nil {
		if errors.Is(err, services.ErrFleetKeyUnset) {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, found, "succeed")
}

func (a *nodesApi) adopt(w http.ResponseWriter, r *http.Request) {
	var in services.AdoptInput
	if err := decodeJSON(w, r, &in); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// The adopting operator's role owns the node (default full access); recorded
	// server-side from the session, never from the request body.
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		in.OwnerRoleId = claims.RoleId
		in.OwnerUserId = claims.Id
	}
	node, err := a.registry.Adopt(r.Context(), in)
	if err != nil {
		a.recordNodeAction(r, "node.adopt", in.NodeID, "error", "adopt failed: "+err.Error(), map[string]any{"ip": in.IP})
		// Log the RAW error to the server log too (the client gets a friendlier message, and
		// the audit trail is in the DB) so an operator reading the log file sees the actual
		// cause — e.g. "insert failed: ... no such column: lat".
		if a.logf != nil {
			a.logf("adopt failed for ip=%s: %v", in.IP, err)
		}
		switch {
		case errors.Is(err, services.ErrFleetKeyUnset):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrAdoptPersist):
			// The node paired but we couldn't save its record (and we've rolled the node back
			// so it can be re-adopted). Give an actionable message, not the raw DB string.
			controllers.SendError(w, controllers.ErrInternalServerError,
				"The node paired but its record could not be saved on the control plane, so the pairing was rolled back. Check the control-plane database, then adopt again with a fresh claim code.")
		case errors.Is(err, services.ErrAdoptRejected):
			// The node itself refused (already paired elsewhere, or an expired/used claim
			// code). Pass the node's reason through — it is already human-readable.
			controllers.SendError(w, controllers.ErrConflict, err.Error())
		default:
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		}
		return
	}
	a.recordNodeAction(r, "node.adopt", node.NodeId, "success",
		fmt.Sprintf("adopted node %q (%s)", node.Name, node.NodeId), map[string]any{"ip": node.IP, "name": node.Name})
	controllers.SendResult(w, node, "succeed")
}

// update edits an adopted node's operator-facing fields (display name, description, nav
// icon) — the editable side of what adoption captured. Identity/trust fields are untouched.
func (a *nodesApi) update(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	var updatedBy int64
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		updatedBy = claims.Id
	}
	node, err := a.registry.UpdateMeta(r.Context(), nodeID, body.Name, body.Description, body.Icon, updatedBy)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, node, "succeed")
}

// updatePosition sets a node's geographic map coordinates (from an operator dragging the
// pin) or clears it off the map (placed=false). Separate from the meta update so a drag
// never has to round-trip name/description/icon.
func (a *nodesApi) updatePosition(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	var body struct {
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
		Placed *bool   `json:"placed"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// Absent "placed" means "I'm positioning it" — the common drag case. Only an explicit
	// false unplaces a node.
	placed := true
	if body.Placed != nil {
		placed = *body.Placed
	}
	if placed && (body.Lat < -90 || body.Lat > 90 || body.Lon < -180 || body.Lon > 180) {
		controllers.SendError(w, controllers.ErrBadRequest, "coordinates out of range")
		return
	}
	var updatedBy int64
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		updatedBy = claims.Id
	}
	node, err := a.registry.UpdatePosition(r.Context(), nodeID, body.Lat, body.Lon, placed, updatedBy)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, node, "succeed")
}

// updateBuilding assigns a node to the building it resides in (siteId), or clears it (siteId 0).
func (a *nodesApi) updateBuilding(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	var body struct {
		SiteId int64 `json:"siteId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	var updatedBy int64
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		updatedBy = claims.Id
	}
	node, err := a.registry.UpdateNodeSite(r.Context(), nodeID, body.SiteId, updatedBy)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, node, "succeed")
}

// setAutoRenew turns a node's certificate auto-renew gate on or off. With it off (the
// default for a newly adopted node) the node's cert is allowed to lapse when it expires,
// dropping the node out of the fleet; with it on the node's automatic re-enrollment before
// expiry is honoured. No certificate is issued here.
func (a *nodesApi) setAutoRenew(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	var body struct {
		AutoRenew bool `json:"autoRenew"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	var updatedBy int64
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		updatedBy = claims.Id
	}
	node, err := a.registry.SetAutoRenew(r.Context(), nodeID, body.AutoRenew, updatedBy)
	if err != nil {
		a.recordNodeAction(r, "node.auto_renew", nodeID, "error", "set auto-renew failed: "+err.Error(), nil)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	state := "disabled"
	if body.AutoRenew {
		state = "enabled"
	}
	a.recordNodeAction(r, "node.auto_renew", nodeID, "success", "certificate auto-renew "+state+" for node "+nodeID,
		map[string]any{"autoRenew": body.AutoRenew})
	controllers.SendResult(w, node, "succeed")
}

func (a *nodesApi) release(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	if err := a.registry.Release(r.Context(), nodeID); err != nil {
		a.recordNodeAction(r, "node.release", nodeID, "error", "release failed: "+err.Error(), nil)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.recordNodeAction(r, "node.release", nodeID, "success", "released node "+nodeID+" (certificate revoked)", nil)
	controllers.SendResult(w, map[string]any{"released": true}, "succeed")
}

// listUnrecognized returns nodes that keep dialing the control channel but are refused (no
// managed record, or a revoked cert) — the stranded nodes an operator otherwise can't see.
func (a *nodesApi) listUnrecognized(w http.ResponseWriter, r *http.Request) {
	if a.rejects == nil {
		controllers.SendResult(w, []services.RejectedNode{}, "succeed")
		return
	}
	controllers.SendResult(w, a.rejects.Unrecognized(), "succeed")
}

// blockNode revokes a stranded node's certificate so it can no longer enroll or hold a
// control channel, then drops it from the unrecognized list. This is the control-plane-side
// "remove" for a node that has no managed record to release — the node still needs a factory
// reset on its own side to stop dialing entirely, which the UI explains.
func (a *nodesApi) blockNode(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	if err := a.registry.RevokeNode(r.Context(), nodeID); err != nil {
		a.recordNodeAction(r, "node.block", nodeID, "error", "block failed: "+err.Error(), nil)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if a.rejects != nil {
		a.rejects.ForgetRejected(nodeID)
	}
	a.recordNodeAction(r, "node.block", nodeID, "success", "blocked stranded node "+nodeID+" (certificate revoked)", nil)
	controllers.SendResult(w, map[string]any{"blocked": true}, "succeed")
}

// forgetNode drops a node from the unrecognized list WITHOUT revoking (operator dismissed the
// warning). It reappears if the node dials and is refused again.
func (a *nodesApi) forgetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	if a.rejects != nil {
		a.rejects.ForgetRejected(nodeID)
	}
	a.recordNodeAction(r, "node.forget", nodeID, "success", "dismissed unrecognized node "+nodeID, nil)
	controllers.SendResult(w, map[string]any{"forgotten": true}, "succeed")
}

func (a *nodesApi) fleetKey(w http.ResponseWriter, r *http.Request) {
	key, err := a.registry.FleetKey(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"fleetKey": key, "set": key != ""}, "succeed")
}

func (a *nodesApi) generateFleetKey(w http.ResponseWriter, r *http.Request) {
	key, err := a.registry.GenerateFleetKey(r.Context())
	if err != nil {
		a.recordFleetAction(r, "fleet.key_rotate", "error", "fleet-key rotation failed: "+err.Error())
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// The key itself is never recorded in the audit trail — only that it was rotated.
	a.recordFleetAction(r, "fleet.key_rotate", "success", "rotated the fleet key")
	controllers.SendResult(w, map[string]any{"fleetKey": key, "set": true}, "succeed")
}

// recordFleetAction audits a fleet-wide (non-node-specific) operator action.
func (a *nodesApi) recordFleetAction(r *http.Request, action, outcome, detail string) {
	if a.audit == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "fleet",
		Outcome:    outcome,
		Detail:     detail,
		ClientIp:   clientIP(r),
	})
}

func (a *nodesApi) enroll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"nodeId"`
		Token  string `json:"token"`
		CSR    string `json:"csr"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	certPEM, caRootPEM, err := a.registry.Enroll(r.Context(), body.NodeID, body.Token, []byte(body.CSR))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrNodeRevoked), errors.Is(err, services.ErrRenewNotAuthorized):
			// Revoked or renewal-not-authorized: the node is known but must not get a fresh
			// cert. A distinct non-5xx status keeps the node's retry loop quiet-ish and lets
			// it recover the moment an operator enables auto-renew.
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		case errors.Is(err, services.ErrNodeUnknown), errors.Is(err, services.ErrAdoptRejected):
			controllers.SendError(w, controllers.ErrPermission, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}
	controllers.SendResult(w, map[string]any{"nodeCert": string(certPEM), "caRoot": string(caRootPEM)}, "succeed")
}

func (a *nodesApi) selfDropped(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID    string `json:"nodeId"`
		Nonce     string `json:"nonce"`
		Timestamp int64  `json:"ts"`
		Assertion string `json:"assertion"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.registry.MarkSelfDropped(r.Context(), body.NodeID, body.Nonce, body.Timestamp, body.Assertion); err != nil {
		controllers.SendError(w, controllers.ErrPermission, err.Error())
		return
	}
	// Node-initiated (fleet-key authenticated, no operator session): attribute to the node.
	if a.audit != nil {
		a.audit.Record(r.Context(), services.AuditEntry{
			Action:     "node.self_dropped",
			ActorEmail: "node:" + body.NodeID,
			TargetType: "node",
			TargetId:   body.NodeID,
			Detail:     "node " + body.NodeID + " reported it unpaired itself",
			ClientIp:   clientIP(r),
		})
	}
	controllers.SendResult(w, map[string]any{"acknowledged": true}, "succeed")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
