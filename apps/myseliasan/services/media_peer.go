package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Live camera video across instances.
//
// A node's camera RTP arrives on its MEDIA channel, which — like the control channel —
// terminates on exactly one instance. Only that instance can answer a browser's WebRTC
// offer for one of its cameras, so behind a load balancer a viewer had a 1-in-N chance of
// landing somewhere that could serve them, and otherwise got "node media channel is not
// connected" for a camera that was streaming perfectly well.
//
// The fix is to forward the NEGOTIATION, not the media. The offer is passed to the owning
// instance, which builds the answer with its own WebRTC engine; the answer carries that
// instance's own ICE candidates, so the browser then peers with it DIRECTLY and the video
// never crosses the load balancer or a second instance. Relaying the RTP itself would have
// doubled bandwidth and added a hop of latency to every frame, to move bytes that are
// already designed to go peer-to-peer.
//
// This is why each instance needs its own reachable address and UDP port for live video —
// the operator checklist has said so since the deployment mode was introduced, because the
// media path was always going to be direct.

// PeerMediaOfferPath is the internal endpoint that answers a forwarded WebRTC offer.
const PeerMediaOfferPath = "/internal/cluster/media-offer"

// MediaOfferRequest is a browser's offer, forwarded to the instance holding the node's
// media channel.
type MediaOfferRequest struct {
	NodeID   string `json:"nodeId"`
	CameraID uint64 `json:"cameraId"`
	Type     string `json:"type"`
	SDP      string `json:"sdp"`
}

// MediaOfferReply carries the owning instance's WebRTC answer back.
type MediaOfferReply struct {
	Type  string `json:"type,omitempty"`
	SDP   string `json:"sdp,omitempty"`
	Error string `json:"error,omitempty"`
}

// ErrMediaNotConnected means no instance holds this node's media channel.
var ErrMediaNotConnected = errors.New("node media channel is not connected")

// ForwardMediaOffer sends a browser's offer to the owning instance and returns its answer.
func (p *PeerClient) ForwardMediaOffer(ctx context.Context, ownerURL string, req MediaOfferRequest) (MediaOfferReply, error) {
	if p == nil || p.token == "" {
		return MediaOfferReply{}, errors.New("cluster forwarding is not configured on this instance")
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return MediaOfferReply{}, err
	}
	url := strings.TrimSuffix(ownerURL, "/") + "/api" + PeerMediaOfferPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return MediaOfferReply{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return MediaOfferReply{}, fmt.Errorf("media offer to %s: %w", ownerURL, err)
	}
	defer resp.Body.Close()

	var reply MediaOfferReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return MediaOfferReply{}, fmt.Errorf("media offer to %s: decode reply: %w", ownerURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		if reply.Error != "" {
			return MediaOfferReply{}, fmt.Errorf("media offer to %s: %s", ownerURL, reply.Error)
		}
		return MediaOfferReply{}, fmt.Errorf("media offer to %s: peer returned %d", ownerURL, resp.StatusCode)
	}
	if reply.Error != "" {
		return MediaOfferReply{}, errors.New(reply.Error)
	}
	return reply, nil
}

// PeerMediaOfferHandler answers a forwarded offer using THIS instance's media connection
// and WebRTC engine.
//
// Authorization is deliberately NOT repeated here. The forwarding instance already
// resolved the operator's live role and their per-node grant before forwarding; this
// endpoint is reachable only with the derived peer token, which is a cluster-internal
// credential, and re-resolving the grant here would mean trusting a role id carried on the
// wire instead of the session that produced it.
type PeerMediaOfferHandler struct {
	token  string
	answer func(ctx context.Context, req MediaOfferRequest) (MediaOfferReply, error)
	logf   func(string, ...any)
}

// NewPeerMediaOfferHandler builds the receiving half. answer performs the local WebRTC
// negotiation (it is supplied by the API layer, which owns the engine).
func NewPeerMediaOfferHandler(jwtSecret string, answer func(context.Context, MediaOfferRequest) (MediaOfferReply, error), logf func(string, ...any)) *PeerMediaOfferHandler {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &PeerMediaOfferHandler{token: DerivePeerToken(jwtSecret), answer: answer, logf: logf}
}

func (h *PeerMediaOfferHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.token == "" || h.answer == nil {
		writeMediaErr(w, http.StatusServiceUnavailable, "cluster media forwarding is not configured on this instance")
		return
	}
	if !peerTokenMatches(r, h.token) {
		writeMediaErr(w, http.StatusUnauthorized, "peer authentication failed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var req MediaOfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMediaErr(w, http.StatusBadRequest, "invalid offer")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || req.CameraID == 0 {
		writeMediaErr(w, http.StatusBadRequest, "invalid node or camera")
		return
	}

	reply, err := h.answer(r.Context(), req)
	if err != nil {
		h.logf("peer-forwarded media offer for node %s failed here: %v", req.NodeID, err)
		// 200 with an error field: the HOP succeeded, the negotiation did not.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MediaOfferReply{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(reply)
}

func writeMediaErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(MediaOfferReply{Error: msg})
}
