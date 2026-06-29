package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/fleetca"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

const (
	fleetKeySettingKey = "pairing.fleetKey"
	fleetKeyBytes      = 32
	adoptCallTimeout   = 10 * time.Second
)

// Errors surfaced to the API layer.
var (
	ErrFleetKeyUnset    = errors.New("fleet key is not configured")
	ErrNodeAlreadyKnown = errors.New("node is already adopted")
	ErrAdoptRejected    = errors.New("node rejected adoption")
	ErrNodeRevoked      = errors.New("node certificate is revoked")
	ErrNodeUnknown      = errors.New("unknown node")
)

// DiscoveredNode is a node found on the LAN, annotated with whether this control
// plane has already adopted it.
type DiscoveredNode struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	IP        string `json:"ip"`
	HTTPSPort int    `json:"httpsPort"`
	Adopted   bool   `json:"adopted"`
}

// AdoptInput binds a node. IP+HTTPSPort locate it; ClaimCode authorises the bind
// (the operator reads it from the node). NodeID/Name are optional hints from a
// prior scan.
type AdoptInput struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	HTTPSPort int    `json:"httpsPort"`
	ClaimCode string `json:"claimCode"`
	// OwnerRoleId / OwnerUserId identify the adopting operator (set server-side from
	// the session, never the client). OwnerRoleId becomes the node's owning role,
	// which gets default full access; OwnerUserId is recorded for audit.
	OwnerRoleId int64 `json:"-"`
	OwnerUserId int64 `json:"-"`
}

// INodeRegistry owns fleet-key management, LAN discovery, the adopt/release
// lifecycle of managed nodes, certificate enrollment (fleet CA), and heartbeat
// reconciliation.
type INodeRegistry interface {
	FleetKey(ctx context.Context) (string, error)
	SetFleetKey(ctx context.Context, key string) error
	GenerateFleetKey(ctx context.Context) (string, error)
	List(ctx context.Context) ([]*entities.ManagedNode, error)
	Scan(ctx context.Context, timeout time.Duration) ([]DiscoveredNode, error)
	Adopt(ctx context.Context, in AdoptInput) (*entities.ManagedNode, error)
	Release(ctx context.Context, nodeID string) error
	MarkSelfDropped(ctx context.Context, nodeID, nonce string, ts int64, assertion string) error
	// Enroll signs a node's CSR (token-authenticated) and returns its cert + the CA
	// root. Called by the node right after adoption and on renewal.
	Enroll(ctx context.Context, nodeID, token string, csrPEM []byte) (nodeCertPEM, caRootPEM []byte, err error)
	// Heartbeat probes every adopted node over mTLS and reconciles its status.
	Heartbeat(ctx context.Context)
	// ParentServerTLS returns the mTLS server config for the parent's control-channel
	// listener (presents the parent's fleet leaf, requires a node client cert).
	ParentServerTLS(ctx context.Context) (*tls.Config, error)
	// AcceptControlConn validates a node connecting on the control channel (known +
	// not revoked) and marks it online, returning the node record. The TLS layer has
	// already proven the caller holds nodeID's fleet cert.
	AcceptControlConn(ctx context.Context, nodeID string) (*entities.ManagedNode, error)
}

// NodeRegistryConfig configures the registry's network identity and timings.
type NodeRegistryConfig struct {
	MulticastAddr     string
	ParentID          string
	ParentName        string
	ParentBaseURL     string
	MTLSPort          int
	CertTTL           time.Duration
	HeartbeatInterval time.Duration
}

type nodeRegistry struct {
	nodes         dbsql.IGenericRepo[entities.ManagedNode]
	settings      dbsql.IGenericRepo[entities.ControlSetting]
	ca            *fleetCA
	cfg           NodeRegistryConfig
	parentBaseURL string
	bootstrapHTTP *http.Client
}

// NewNodeRegistry builds the registry. ParentBaseURL is this control plane's own
// externally reachable URL, recorded on the node so it can call back (enroll /
// release / self-drop). The bootstrap HTTP client tolerates the node's self-signed
// cert (the PSK-authenticated adopt/enroll leg); ongoing management uses mTLS via
// the fleet CA.
func NewNodeRegistry(db dbsql.IDbCrud, cfg NodeRegistryConfig) INodeRegistry {
	return newNodeRegistry(
		dbsql.NewGenericRepo[entities.ManagedNode](db),
		dbsql.NewGenericRepo[entities.ControlSetting](db),
		cfg,
	)
}

// newNodeRegistry is the repo-injecting constructor used by NewNodeRegistry and by
// tests (which supply in-memory repos).
func newNodeRegistry(
	nodes dbsql.IGenericRepo[entities.ManagedNode],
	settings dbsql.IGenericRepo[entities.ControlSetting],
	cfg NodeRegistryConfig,
) *nodeRegistry {
	return &nodeRegistry{
		nodes:         nodes,
		settings:      settings,
		ca:            newFleetCA(settings, cfg.ParentID, cfg.CertTTL),
		cfg:           cfg,
		parentBaseURL: strings.TrimRight(cfg.ParentBaseURL, "/"),
		bootstrapHTTP: &http.Client{
			Timeout:   adoptCallTimeout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
	}
}

func (s *nodeRegistry) FleetKey(ctx context.Context) (string, error) {
	row, err := s.settings.GetByUnique(ctx, "", "key", fleetKeySettingKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return "", nil
		}
		return "", err
	}
	if row == nil {
		return "", nil
	}
	return row.Value, nil
}

func (s *nodeRegistry) SetFleetKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if len(key) < 16 {
		return fmt.Errorf("fleet key must be at least 16 characters")
	}
	return s.upsertSetting(ctx, fleetKeySettingKey, key)
}

func (s *nodeRegistry) GenerateFleetKey(ctx context.Context) (string, error) {
	b := make([]byte, fleetKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(b)
	if err := s.upsertSetting(ctx, fleetKeySettingKey, key); err != nil {
		return "", err
	}
	return key, nil
}

func (s *nodeRegistry) List(ctx context.Context) ([]*entities.ManagedNode, error) {
	rows, _, err := s.nodes.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.ManagedNode{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *nodeRegistry) Scan(ctx context.Context, timeout time.Duration) ([]DiscoveredNode, error) {
	key, err := s.FleetKey(ctx)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrFleetKeyUnset
	}
	results, err := pairing.Discover(ctx, []byte(key), s.cfg.MulticastAddr, timeout)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	if nodes, err := s.List(ctx); err == nil {
		for _, n := range nodes {
			known[n.NodeId] = true
		}
	}
	out := make([]DiscoveredNode, 0, len(results))
	for _, r := range results {
		out = append(out, DiscoveredNode{
			NodeID:    r.NodeID,
			Name:      r.Name,
			Version:   r.Version,
			IP:        r.IP,
			HTTPSPort: r.HTTPSPort,
			Adopted:   known[r.NodeID],
		})
	}
	return out, nil
}

func (s *nodeRegistry) Adopt(ctx context.Context, in AdoptInput) (*entities.ManagedNode, error) {
	key, err := s.FleetKey(ctx)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrFleetKeyUnset
	}
	ip := strings.TrimSpace(in.IP)
	if ip == "" {
		return nil, fmt.Errorf("node IP is required")
	}
	port := in.HTTPSPort
	if port <= 0 {
		return nil, fmt.Errorf("node HTTPS port is required")
	}
	baseURL := fmt.Sprintf("https://%s:%d", ip, port)

	// Build the signed adopt request: assertion proves we hold the fleet key.
	nonce, err := randHex(16)
	if err != nil {
		return nil, err
	}
	ts := time.Now().Unix()
	assertion := pairing.SignAssertion([]byte(key), s.cfg.ParentID, nonce, strconv.FormatInt(ts, 10))
	reqBody, _ := json.Marshal(map[string]any{
		"parentId":      s.cfg.ParentID,
		"parentName":    s.cfg.ParentName,
		"parentBaseUrl": s.parentBaseURL,
		"claimCode":     strings.TrimSpace(in.ClaimCode),
		"nonce":         nonce,
		"ts":            ts,
		"assertion":     assertion,
	})

	res, err := s.postNode(ctx, baseURL+"/api/pairing/adopt", reqBody)
	if err != nil {
		return nil, err
	}

	nodeID := firstNonEmpty(res.NodeID, in.NodeID)
	// Re-adopting a previously released node: clear any stale revocation so its new
	// enrollment can succeed.
	_ = s.ca.Unrevoke(ctx, nodeID)

	now := time.Now().Unix()
	node := entities.ManagedNode{
		NodeId:      nodeID,
		Name:        firstNonEmpty(res.Name, in.Name),
		BaseUrl:     baseURL,
		IP:          ip,
		HTTPSPort:   port,
		MTLSPort:    s.cfg.MTLSPort,
		Token:       res.Token,
		Status:      "online",
		OwnerRoleId: in.OwnerRoleId,
		AdoptedAt:   now,
		LastSeenAt:  now,
		CreatedBy:   in.OwnerUserId,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.upsertNode(ctx, node); err != nil {
		return nil, err
	}
	saved, _ := s.nodes.GetByUnique(ctx, "", "node_id", node.NodeId)
	return saved, nil
}

// Enroll signs a node's CSR after verifying the pairing token matches the adopted
// node. Returns the issued node cert + the CA root. Idempotent — used for both
// initial enrollment and renewal.
func (s *nodeRegistry) Enroll(ctx context.Context, nodeID, token string, csrPEM []byte) ([]byte, []byte, error) {
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil || node == nil {
		return nil, nil, ErrNodeUnknown
	}
	if node.Token == "" || node.Token != token {
		return nil, nil, ErrAdoptRejected
	}
	certPEM, caRootPEM, err := s.ca.SignNodeCSR(ctx, nodeID, csrPEM)
	if err != nil {
		return nil, nil, err
	}
	node.CertExpiresAt = time.Now().Add(s.certTTL()).Unix()
	node.Status = "online"
	node.LastSeenAt = time.Now().Unix()
	node.UpdatedAt = time.Now().Unix()
	_, _ = s.nodes.UpdateById(ctx, "", *node)
	return certPEM, caRootPEM, nil
}

func (s *nodeRegistry) certTTL() time.Duration {
	if s.cfg.CertTTL > 0 {
		return s.cfg.CertTTL
	}
	return defaultCertTTL
}

func (s *nodeRegistry) Release(ctx context.Context, nodeID string) error {
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil
		}
		return err
	}
	if node == nil {
		return nil
	}
	// Revoke first so a node that re-enrolls before we finish can't get a fresh cert.
	_ = s.ca.Revoke(ctx, nodeID)
	// Best-effort: tell the node to release over mTLS (preferred) or the token leg.
	// Even if unreachable, we drop our record — the node falls out of trust as its
	// cert ages out and renewal is now refused.
	if !s.releaseOverMTLS(ctx, node) {
		body, _ := json.Marshal(map[string]string{"token": node.Token})
		if _, err := s.postNode(ctx, node.BaseUrl+"/api/pairing/release", body); err != nil {
			_ = err
		}
	}
	_, err = s.nodes.DeleteById(ctx, "", uint64(node.Id))
	return err
}

// Heartbeat probes every adopted node over mTLS and reconciles its status: a
// reachable node is marked online (LastSeenAt bumped); an unreachable one is marked
// lost. A node that self-dropped (status already "self-dropped") is left as-is.
func (s *nodeRegistry) Heartbeat(ctx context.Context) {
	nodes, err := s.List(ctx)
	if err != nil {
		return
	}
	for _, node := range nodes {
		if node.Status == "self-dropped" {
			continue
		}
		alive := s.probeOverMTLS(ctx, node)
		now := time.Now().Unix()
		if alive {
			node.Status = "online"
			node.LastSeenAt = now
		} else {
			node.Status = "lost"
		}
		node.UpdatedAt = now
		_, _ = s.nodes.UpdateById(ctx, "", *node)
	}
}

// ParentServerTLS returns the mTLS server config for the parent control listener.
func (s *nodeRegistry) ParentServerTLS(ctx context.Context) (*tls.Config, error) {
	return s.ca.ParentServerTLS(ctx)
}

// AcceptControlConn validates a node connecting on the control channel and marks
// it online. The mTLS handshake already proved the caller holds nodeID's fleet
// cert; here we reject nodes we never adopted or have since revoked, and bump
// liveness (socket presence is a stronger signal than the heartbeat poll).
func (s *nodeRegistry) AcceptControlConn(ctx context.Context, nodeID string) (*entities.ManagedNode, error) {
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil || node == nil {
		return nil, ErrNodeUnknown
	}
	if revoked, _ := s.ca.IsRevoked(ctx, nodeID); revoked {
		return nil, ErrNodeRevoked
	}
	now := time.Now().Unix()
	node.Status = "online"
	node.LastSeenAt = now
	node.UpdatedAt = now
	_, _ = s.nodes.UpdateById(ctx, "", *node)
	return node, nil
}

// mtlsClient builds an mTLS HTTP client for one node, verifying the node's server
// cert CN == nodeID. Returns nil if the CA/parent cert can't be prepared.
func (s *nodeRegistry) mtlsClient(ctx context.Context, node *entities.ManagedNode) *http.Client {
	certPEM, keyPEM, caRootPEM, err := s.ca.ParentClientTLS(ctx, node.NodeId)
	if err != nil {
		return nil
	}
	tlsCfg, err := fleetca.ClientTLSConfig(certPEM, keyPEM, caRootPEM, node.NodeId)
	if err != nil {
		return nil
	}
	return &http.Client{Timeout: adoptCallTimeout, Transport: &http.Transport{TLSClientConfig: tlsCfg}}
}

func (s *nodeRegistry) mtlsURL(node *entities.ManagedNode, path string) string {
	port := node.MTLSPort
	if port <= 0 {
		port = s.cfg.MTLSPort
	}
	return fmt.Sprintf("https://%s:%d%s", node.IP, port, path)
}

func (s *nodeRegistry) probeOverMTLS(ctx context.Context, node *entities.ManagedNode) bool {
	client := s.mtlsClient(ctx, node)
	if client == nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.mtlsURL(node, "/heartbeat"), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (s *nodeRegistry) releaseOverMTLS(ctx context.Context, node *entities.ManagedNode) bool {
	client := s.mtlsClient(ctx, node)
	if client == nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.mtlsURL(node, "/release"), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (s *nodeRegistry) MarkSelfDropped(ctx context.Context, nodeID, nonce string, ts int64, assertion string) error {
	key, err := s.FleetKey(ctx)
	if err != nil {
		return err
	}
	// The notice is authenticated with the fleet key (which survives unpair), so a
	// random caller can't mark arbitrary nodes lost.
	if key == "" || !pairing.AssertionFresh(ts, 5*time.Minute) ||
		!pairing.VerifyAssertion([]byte(key), assertion, nodeID, nonce, strconv.FormatInt(ts, 10)) {
		return ErrAdoptRejected
	}
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil
		}
		return err
	}
	if node == nil {
		return nil
	}
	node.Status = "self-dropped"
	node.UpdatedAt = time.Now().Unix()
	_, err = s.nodes.UpdateById(ctx, "", *node)
	return err
}

// adoptResponse is the node's /adopt success payload (unwrapped from the standard
// {data:{result:…}} envelope).
type adoptResponse struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Token  string `json:"token"`
}

func (s *nodeRegistry) postNode(ctx context.Context, url string, body []byte) (adoptResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return adoptResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.bootstrapHTTP.Do(req)
	if err != nil {
		return adoptResponse{}, fmt.Errorf("node unreachable: %w", err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data struct {
			Result adoptResponse `json:"result"`
		} `json:"data"`
		Result  adoptResponse `json:"result"`
		Message string        `json:"message"`
	}
	dec := json.NewDecoder(resp.Body)
	_ = dec.Decode(&envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := envelope.Message
		if msg == "" {
			msg = resp.Status
		}
		return adoptResponse{}, fmt.Errorf("%w: %s", ErrAdoptRejected, msg)
	}
	out := envelope.Data.Result
	if out.NodeID == "" && envelope.Result.NodeID != "" {
		out = envelope.Result
	}
	return out, nil
}

func (s *nodeRegistry) upsertSetting(ctx context.Context, key, value string) error {
	now := time.Now().Unix()
	row, err := s.settings.GetByUnique(ctx, "", "key", key)
	if err != nil {
		if isNoResultFoundErr(err) {
			_, cerr := s.settings.Create(ctx, "", entities.ControlSetting{Key: key, Value: value, CreatedAt: now, UpdatedAt: now})
			return cerr
		}
		return err
	}
	row.Value = value
	row.UpdatedAt = now
	_, err = s.settings.UpdateById(ctx, "", *row)
	return err
}

func (s *nodeRegistry) upsertNode(ctx context.Context, node entities.ManagedNode) error {
	existing, err := s.nodes.GetByUnique(ctx, "", "node_id", node.NodeId)
	if err != nil && !isNoResultFoundErr(err) {
		return err
	}
	if existing != nil {
		node.Id = existing.Id
		node.CreatedAt = existing.CreatedAt
		_, err = s.nodes.UpdateById(ctx, "", node)
		return err
	}
	_, err = s.nodes.Create(ctx, "", node)
	return err
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func isNoResultFoundErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no result found")
}
