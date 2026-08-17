package apis

import (
	"context"
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
	hub     *services.MediaRelayHub
	access  services.INodeAccessService
	engine  *stream.WebRTCEngine
	ice     []stream.ICEServer
	session *middlewares.AccessSessionMidware
	// forward negotiates this offer on the instance that holds the node's media channel.
	// Nil for a single-instance deployment, where every connected node is connected HERE.
	forward func(context.Context, services.MediaOfferRequest) (services.MediaOfferReply, error)
}

// NewNodeMediaApi registers POST /api/nodes/{id}/cameras/{cam}/webrtc/offer. Must be
// registered before the proxy catch-all so its specific path wins.
func NewNodeMediaApi(router *mux.Router, auth middlewares.AuthMidware, hub *services.MediaRelayHub, access services.INodeAccessService, engine *stream.WebRTCEngine, ice []stream.ICEServer, session *middlewares.AccessSessionMidware, forward func(context.Context, services.MediaOfferRequest) (services.MediaOfferReply, error)) *nodeMediaApi {
	a := &nodeMediaApi{hub: hub, access: access, engine: engine, ice: ice, session: session, forward: forward}
	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("/{id}/cameras/{cam}/webrtc/offer", a.offer).Methods("POST")

	// Browser ICE config (STUN/TURN) so a cross-network browser can reach the parent.
	// Separate prefix to avoid colliding with /nodes/{id}.
	cfg := router.PathPrefix("/node-stream").Subrouter()
	cfg.Use(auth.Middleware)
	cfg.HandleFunc("/config", a.streamConfig).Methods("GET")
	return a
}

// AnswerLocalOffer negotiates an offer against THIS instance's media connection and WebRTC
// engine. It is what the peer endpoint calls when another instance forwards an offer here:
// the authorization already happened at the instance the browser talked to, against that
// operator's live session.
func (a *nodeMediaApi) AnswerLocalOffer(ctx context.Context, req services.MediaOfferRequest) (services.MediaOfferReply, error) {
	if !a.hub.IsConnected(req.NodeID) {
		return services.MediaOfferReply{}, services.ErrMediaNotConnected
	}
	manager := stream.NewManagerWithConnectorEngine(a.hub.Connector(req.NodeID), a.engine)
	answer, err := manager.CreateWebRTCAnswerWithOptions(ctx,
		stream.Source{ID: fmt.Sprintf("camera-%d", req.CameraID)},
		stream.SessionDescription{Type: req.Type, SDP: req.SDP},
		stream.Options{ICEServers: a.ice})
	if err != nil {
		return services.MediaOfferReply{}, err
	}
	return services.MediaOfferReply{Type: answer.Type, SDP: answer.SDP}, nil
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
	// Live role from the user store (not the token's baked roleId) gates media access.
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
	if acc.Role() == "" {
		controllers.SendError(w, controllers.ErrLimitedAccess, "no access to this node")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body webrtcOfferBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid offer")
		return
	}

	// The node's media channel terminates on exactly one instance, and only that instance
	// can answer for its cameras. Behind a load balancer this request lands on an arbitrary
	// one, so an offer for a node held elsewhere is negotiated THERE and its answer returned
	// verbatim — the answer carries that instance's own ICE candidates, so the browser then
	// peers with it directly and the video never crosses the balancer. Authorization has
	// already happened above, on this instance, against the operator's live session.
	if !a.hub.IsConnected(nodeID) {
		if a.forward != nil {
			answer, ferr := a.forward(r.Context(), services.MediaOfferRequest{
				NodeID: nodeID, CameraID: camID, Type: body.Type, SDP: body.SDP,
			})
			if ferr == nil {
				controllers.SendResult(w, map[string]any{"type": answer.Type, "sdp": answer.SDP}, "succeed")
				return
			}
			// Fall through to the not-connected answer below: from here the camera is
			// unreachable either way, and that is what the viewer needs to be told.
		}
		controllers.SendError(w, controllers.ErrNotFound, "node media channel is not connected")
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
