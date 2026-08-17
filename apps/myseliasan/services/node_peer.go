package services

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/control"
)

// The instance-to-instance hop.
//
// A node's commands can only be delivered by the instance holding its control channel, so
// an instance that does not hold it forwards the request to the one that does. That makes
// this a new inbound network surface, and it carries operator-authorized node commands —
// unlocking doors, changing settings — so it has to be authenticated as carefully as the
// operator-facing API.
//
// The credential is DERIVED from the signing secret every instance must already share
// rather than being a new secret of its own. That is a deliberate trade: a separate secret
// would isolate blast radius, but it would be one more value that has to be identical on
// every instance, and getting exactly that wrong is the failure this whole feature exists
// to catch. Deriving costs nothing to operate, and the derivation is one-way, so the token
// never reveals the signing secret. If the signing secret leaked, an attacker could already
// mint superadmin tokens for the public API — strictly more power than this endpoint gives
// — so this adds no new weakness.

// PeerForwardPath is the internal endpoint one instance calls on another. Under /api so it
// shares the TLS listener and the reset gate, but deliberately outside the session-auth
// middleware: its caller is a peer instance holding a derived token, not a signed-in user.
const PeerForwardPath = "/internal/cluster/node-forward"

// peerTokenInfo domain-separates the peer token from every other value derived from the
// signing secret (notably the boot-log fingerprint), so possessing one can never produce
// the other.
const peerTokenInfo = "kopiv2-cluster-peer-token-v1"

// peerTokenBytes is the derived token length. 32 bytes is well beyond brute force and the
// token never leaves the cluster's own network.
const peerTokenBytes = 32

// ErrNoOwner means no instance currently holds this node's control channel — it is offline
// everywhere, not merely elsewhere.
var ErrNoOwner = errors.New("no instance holds this node's control channel")

// peerTokenMatches checks a request's bearer token against the derived peer credential.
//
// Constant-time: a check that returns early on the first wrong byte leaks the token one
// byte at a time to anyone who can measure the reply. Shared by every peer endpoint so
// none of them can drift into a plain string comparison.
func peerTokenMatches(r *http.Request, token string) bool {
	presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(token)) == 1
}

// DerivePeerToken produces the shared peer credential from the signing secret. Every
// instance derives the same value from the same secret without any of them transmitting it.
func DerivePeerToken(jwtSecret string) string {
	if strings.TrimSpace(jwtSecret) == "" {
		return ""
	}
	out, err := hkdf.Key(sha256.New, []byte(jwtSecret), nil, peerTokenInfo, peerTokenBytes)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(out)
}

// peerForwardBody is the wire shape of a forwarded request. control.Request is carried
// field-by-field rather than embedded so a change to the internal frame type cannot
// silently alter this contract between two instances that may be on different builds
// mid-rollout.
type peerForwardBody struct {
	NodeID  string            `json:"nodeId"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Role    string            `json:"role"`
	Actor   string            `json:"actor"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type peerForwardReply struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// PeerClient forwards a tunneled request to the instance that owns the node.
type PeerClient struct {
	token  string
	client *http.Client
}

// NewPeerClient builds the forwarder. insecureSkipVerify exists because instances default
// to self-signed certificates; the request is authenticated by the derived token either
// way, so this trades transport privacy between instances, not admission.
func NewPeerClient(jwtSecret string, timeout time.Duration, insecureSkipVerify bool) *PeerClient {
	if timeout <= 0 {
		timeout = controlRequestTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // operator-declared, token-authenticated
	}
	return &PeerClient{
		token:  DerivePeerToken(jwtSecret),
		client: &http.Client{Timeout: timeout, Transport: transport},
	}
}

// Forward sends req to the owning instance and returns the node's reply.
func (p *PeerClient) Forward(ctx context.Context, ownerURL, nodeID string, req control.Request) (control.Response, error) {
	if p == nil || p.token == "" {
		return control.Response{}, errors.New("cluster forwarding is not configured on this instance")
	}
	payload, err := json.Marshal(peerForwardBody{
		NodeID: nodeID, Method: req.Method, Path: req.Path,
		Role: req.Role, Actor: req.Actor, Headers: req.Headers, Body: req.Body,
	})
	if err != nil {
		return control.Response{}, err
	}

	url := strings.TrimSuffix(ownerURL, "/") + "/api" + PeerForwardPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return control.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return control.Response{}, fmt.Errorf("forward to %s: %w", ownerURL, err)
	}
	defer resp.Body.Close()

	var reply peerForwardReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return control.Response{}, fmt.Errorf("forward to %s: decode reply: %w", ownerURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		if reply.Error != "" {
			return control.Response{}, fmt.Errorf("forward to %s: %s", ownerURL, reply.Error)
		}
		return control.Response{}, fmt.Errorf("forward to %s: peer returned %d", ownerURL, resp.StatusCode)
	}
	// A node-level failure the owner reported (offline, disconnected mid-flight) is
	// returned as an error so the caller's existing handling applies unchanged.
	if reply.Error != "" {
		return control.Response{}, errors.New(reply.Error)
	}
	return control.Response{Status: reply.Status, Headers: reply.Headers, Body: reply.Body}, nil
}

// ForwardingSender resolves node commands across the whole deployment: local connections
// are served directly, and anything else is forwarded to the instance that owns it.
//
// It decorates ControlSender, which is the single interface both the node command proxy and
// the recording-stream reader already depend on — so both surfaces gain cluster awareness
// without either of them learning that instances exist.
type ForwardingSender struct {
	local  ControlSender
	owners *NodeOwnerRegistry
	peer   *PeerClient
	logf   func(string, ...any)
}

// NewForwardingSender wraps local so requests for nodes owned elsewhere are forwarded.
func NewForwardingSender(local ControlSender, owners *NodeOwnerRegistry, peer *PeerClient, logf func(string, ...any)) *ForwardingSender {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ForwardingSender{local: local, owners: owners, peer: peer, logf: logf}
}

// SendRequest delivers req to nodeID wherever its control channel happens to terminate.
func (f *ForwardingSender) SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error) {
	owner, isLocal := f.owners.OwnerOf(ctx, nodeID)
	if isLocal || owner == "" {
		// Owned here, or owned nowhere. Either way the local sender is the right answer —
		// and for "nowhere" its ErrNodeOffline is exactly what the caller should see.
		return f.local.SendRequest(ctx, nodeID, req)
	}
	if !f.owners.Enabled() || f.peer == nil {
		return f.local.SendRequest(ctx, nodeID, req)
	}
	f.logf("forwarding node %s command to %s", nodeID, owner)
	resp, err := f.peer.Forward(ctx, owner, nodeID, req)
	if err != nil {
		// A claim can outlive the connection it describes (an instance killed between
		// renewals). Reporting the node as offline is both true from here and the error
		// the caller already knows how to render.
		f.logf("forward to %s for node %s failed: %v", owner, nodeID, err)
		return control.Response{}, fmt.Errorf("%w: %v", ErrNodeOffline, err)
	}
	return resp, nil
}

// PeerForwardHandler serves forwarded requests from another instance: it authenticates the
// peer, then delivers the request over its own control channel.
//
// It MUST be constructed with the LOCAL sender (the ControlServer), never with a
// ForwardingSender. That is what makes this hop terminal: an instance named as the owner
// either has the connection or the claim is stale, and forwarding onward from here would
// let a stale claim bounce a request between instances instead of failing it.
type PeerForwardHandler struct {
	token string
	local ControlSender
	logf  func(string, ...any)
}

// NewPeerForwardHandler builds the receiving half of the hop.
func NewPeerForwardHandler(jwtSecret string, local ControlSender, logf func(string, ...any)) *PeerForwardHandler {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &PeerForwardHandler{token: DerivePeerToken(jwtSecret), local: local, logf: logf}
}

func (h *PeerForwardHandler) writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(peerForwardReply{Error: msg})
}

// ServeHTTP delivers a peer's forwarded request to the node this instance holds.
func (h *PeerForwardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.token == "" {
		h.writeErr(w, http.StatusServiceUnavailable, "cluster forwarding is not configured on this instance")
		return
	}
	if !peerTokenMatches(r, h.token) {
		h.writeErr(w, http.StatusUnauthorized, "peer authentication failed")
		return
	}

	var body peerForwardBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErr(w, http.StatusBadRequest, "malformed forward payload")
		return
	}
	if strings.TrimSpace(body.NodeID) == "" {
		h.writeErr(w, http.StatusBadRequest, "no node id")
		return
	}

	// Terminal hop. This instance was named as the owner, so if the node is not here the
	// claim is stale — forwarding onward could loop between instances chasing it.
	resp, err := h.local.SendRequest(r.Context(), body.NodeID, control.Request{
		Method: body.Method, Path: body.Path, Role: body.Role,
		Actor: body.Actor, Headers: body.Headers, Body: body.Body,
	})
	if err != nil {
		h.logf("peer-forwarded request for node %s failed here: %v", body.NodeID, err)
		// 200 with an error field: the HOP succeeded, the node call did not. Distinguishing
		// them keeps a node being offline from looking like the peer being unreachable.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(peerForwardReply{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(peerForwardReply{Status: resp.Status, Headers: resp.Headers, Body: resp.Body})
}
