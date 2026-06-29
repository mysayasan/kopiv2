package apis

import (
	"encoding/json"
	"errors"
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
}

// NewNodesApi registers control-plane node-management endpoints.
//
// Operator routes (require a myseliasan session):
//
//	GET  /nodes               — list adopted nodes
//	POST /nodes/scan          — discover unpaired nodes on the LAN
//	POST /nodes/adopt         — adopt a node (by ip+port+claim code)
//	POST /nodes/{id}/release  — release an adopted node
//	GET  /nodes/fleet-key     — show the fleet key
//	POST /nodes/fleet-key     — generate (rotate) the fleet key
//
// Public route (called by a node, authenticated by a fleet-key assertion):
//
//	POST /nodes/self-dropped  — a node reports it unpaired itself
func NewNodesApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, registry services.INodeRegistry) {
	h := &nodesApi{registry: registry}

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
	g.HandleFunc("/scan", h.scan).Methods("POST")
	g.HandleFunc("/adopt", h.adopt).Methods("POST")
	g.HandleFunc("/fleet-key", h.fleetKey).Methods("GET")
	g.HandleFunc("/fleet-key", h.generateFleetKey).Methods("POST")
	g.HandleFunc("/{id}/release", h.release).Methods("POST")
}

func (a *nodesApi) list(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.registry.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, nodes, "succeed")
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
		switch {
		case errors.Is(err, services.ErrFleetKeyUnset):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrAdoptRejected):
			controllers.SendError(w, controllers.ErrConflict, err.Error())
		default:
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		}
		return
	}
	controllers.SendResult(w, node, "succeed")
}

func (a *nodesApi) release(w http.ResponseWriter, r *http.Request) {
	nodeID := mux.Vars(r)["id"]
	if err := a.registry.Release(r.Context(), nodeID); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"released": true}, "succeed")
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
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"fleetKey": key, "set": true}, "succeed")
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
		case errors.Is(err, services.ErrNodeRevoked):
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
	controllers.SendResult(w, map[string]any{"acknowledged": true}, "succeed")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
