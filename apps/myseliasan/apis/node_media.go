package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/stream"
)

// nodeMediaApi answers browser WebRTC offers for a node camera by re-broadcasting the
// RTP the node relays over its media channel. The browser talks only to myseliasan;
// the node→parent leg is the media channel, the parent→browser leg is WebRTC.
type nodeMediaApi struct {
	hub    *services.MediaRelayHub
	access services.INodeAccessService
	engine *stream.WebRTCEngine
	ice    []stream.ICEServer
}

// NewNodeMediaApi registers POST /api/nodes/{id}/cameras/{cam}/webrtc/offer. Must be
// registered before the proxy catch-all so its specific path wins.
func NewNodeMediaApi(router *mux.Router, auth middlewares.AuthMidware, hub *services.MediaRelayHub, access services.INodeAccessService, engine *stream.WebRTCEngine, ice []stream.ICEServer) {
	a := &nodeMediaApi{hub: hub, access: access, engine: engine, ice: ice}
	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("/{id}/cameras/{cam}/webrtc/offer", a.offer).Methods("POST")

	// Browser ICE config (STUN/TURN) so a cross-network browser can reach the parent.
	// Separate prefix to avoid colliding with /nodes/{id}.
	cfg := router.PathPrefix("/node-stream").Subrouter()
	cfg.Use(auth.Middleware)
	cfg.HandleFunc("/config", a.streamConfig).Methods("GET")
}

// streamConfig returns the ICE servers the browser should use when peering with the
// parent (empty for same-LAN; a TURN server when the parent is behind NAT).
func (a *nodeMediaApi) streamConfig(w http.ResponseWriter, r *http.Request) {
	servers := a.ice
	if servers == nil {
		servers = []stream.ICEServer{}
	}
	controllers.SendResult(w, map[string]any{"iceServers": servers}, "succeed")
}

type webrtcOfferBody struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

func (a *nodeMediaApi) offer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["id"]
	camID, err := strconv.ParseUint(vars["cam"], 10, 64)
	if err != nil || camID == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid camera id")
		return
	}

	// Authorize: watching a node camera only needs read access (viewer is enough).
	roleId, _ := operatorIdentity(r)
	acc, err := a.access.Resolve(r.Context(), roleId, nodeID)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if acc.Role() == "" {
		controllers.SendError(w, controllers.ErrLimitedAccess, "no access to this node")
		return
	}

	if !a.hub.IsConnected(nodeID) {
		controllers.SendError(w, controllers.ErrNotFound, "node media channel is not connected")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body webrtcOfferBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid offer")
		return
	}

	// One Manager per request, sharing the configured engine (public-IP/UDP mux) and
	// backed by the node's relay connector. CreateWebRTCAnswerWithOptions does all the
	// pion work; the node streams the camera on demand.
	manager := stream.NewManagerWithConnectorEngine(a.hub.Connector(nodeID), a.engine)
	answer, err := manager.CreateWebRTCAnswerWithOptions(r.Context(),
		stream.Source{ID: fmt.Sprintf("camera-%d", camID)},
		stream.SessionDescription{Type: body.Type, SDP: body.SDP},
		stream.Options{ICEServers: a.ice})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, answer, "succeed")
}
