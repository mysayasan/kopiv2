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
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
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
	// ErrAdoptPersist wraps a failure to save an adopted node's record AFTER the node itself
	// committed to the pairing. The node is rolled back so it can be re-adopted cleanly.
	ErrAdoptPersist = errors.New("adopted the node but failed to save its record")
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
	// Kind is what the node CLAIMS to be, from its discovery announce. It is an ADVISORY DISPLAY
	// HINT — unsigned, so a hostile host on the LAN could lie about it and show the wrong icon in
	// this list. It cannot make the control plane adopt anything, and the authoritative kind is
	// taken from the adopt reply instead. Never make a trust decision on this field.
	Kind string `json:"kind,omitempty"`
}

// AdoptInput binds a node. IP+HTTPSPort locate it; ClaimCode authorises the bind
// (the operator reads it from the node). NodeID/Name are optional hints from a
// prior scan. Name, when set, is the operator's chosen label (it overrides the node's
// reported hostname); Description is an optional operator note.
type AdoptInput struct {
	NodeID      string `json:"nodeId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	IP          string `json:"ip"`
	HTTPSPort   int    `json:"httpsPort"`
	ClaimCode   string `json:"claimCode"`
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
	// UpdateMeta edits an adopted node's operator-facing fields (display name,
	// description, nav icon). It never touches identity/trust fields.
	UpdateMeta(ctx context.Context, nodeID, name, description, icon string, updatedBy int64) (*entities.ManagedNode, error)
	// UpdatePosition sets a node's geographic map coordinates (from dragging its pin) and
	// marks it placed. Kept separate from UpdateMeta so frequent drag writes never race
	// with a name/description edit. Passing placed=false clears the node off the map.
	UpdatePosition(ctx context.Context, nodeID string, lat, lon float64, placed bool, updatedBy int64) (*entities.ManagedNode, error)
	Release(ctx context.Context, nodeID string) error
	// RevokeNode blocklists a node's certificate at the fleet CA without needing a managed
	// record. Used to shut down a stranded node that keeps dialing with a valid cert.
	RevokeNode(ctx context.Context, nodeID string) error
	MarkSelfDropped(ctx context.Context, nodeID, nonce string, ts int64, assertion string) error
	// Enroll signs a node's CSR (token-authenticated) and returns its cert + the CA
	// root. Called by the node right after adoption and on renewal.
	Enroll(ctx context.Context, nodeID, token string, csrPEM []byte) (nodeCertPEM, caRootPEM []byte, err error)
	// Heartbeat probes every adopted node over mTLS and reconciles its status.
	Heartbeat(ctx context.Context)
	// SetControlPresence injects the control-channel liveness oracle. A node holding a
	// live control connection is authoritatively online; the mTLS poll is only a
	// fallback. Set once at startup, after the control server is built.
	SetControlPresence(connected func(nodeID string) bool)
	// SetFleetEventSink injects the callback the registry invokes when it detects a
	// fleet-health transition during reconciliation (a node dropping to "lost",
	// recovering, or a certificate nearing expiry). Optional; nil-safe. Set once at
	// startup, before the heartbeat loop begins.
	SetFleetEventSink(sink FleetEventSink)
	// FleetStatus returns a rollup of the fleet's liveness and certificate health
	// (counts of online/lost/self-dropped nodes and certs expiring/expired).
	FleetStatus(ctx context.Context) (FleetStatus, error)
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
	// CertWarnBefore is how far ahead of a node certificate's expiry the registry
	// raises a fleet-health warning (a still-valid cert this close to expiry means
	// the node's automatic re-enrollment is overdue or failing). 0 = default (7d).
	CertWarnBefore time.Duration
	// SecretCipher, when set, encrypts the fleet PSK and the fleet CA private keys at
	// rest in the control-plane DB. Nil = plaintext (encryption disabled).
	SecretCipher *atrest.Cipher
}

// FleetEventKind classifies a fleet-health transition the registry detects while
// reconciling node liveness and certificate health.
type FleetEventKind string

const (
	// FleetEventNodeLost fires the moment a node transitions to "lost" (no control
	// channel and no mTLS contact past the grace window).
	FleetEventNodeLost FleetEventKind = "node-lost"
	// FleetEventNodeRecovered fires when a previously lost node becomes reachable again.
	FleetEventNodeRecovered FleetEventKind = "node-recovered"
	// FleetEventCertExpiring fires once per distinct expiry when a node certificate is
	// within CertWarnBefore of expiring (or has already expired).
	FleetEventCertExpiring FleetEventKind = "cert-expiring"
)

// FleetEvent is a fleet-health transition emitted to the registry's event sink so
// the control plane can surface it (notifications / alerting). Node is a snapshot
// of the affected node; the cert fields are set only for FleetEventCertExpiring.
type FleetEvent struct {
	Kind      FleetEventKind
	Node      *entities.ManagedNode
	ExpiresAt int64 // unix expiry of the node cert (cert events only)
	HoursLeft int   // whole hours until expiry, negative if already expired (cert events only)
}

// FleetEventSink receives fleet-health events. It is invoked synchronously from the
// heartbeat reconciler, so implementations should return promptly (hand off async work).
type FleetEventSink func(FleetEvent)

// defaultCertWarnBefore is the fallback lead time for certificate-expiry warnings.
const defaultCertWarnBefore = 7 * 24 * time.Hour

// FleetStatus is a rollup of the adopted fleet's liveness and certificate health,
// suitable for a dashboard header ("X online / Y lost / Z certs expiring").
type FleetStatus struct {
	Total         int `json:"total"`
	Online        int `json:"online"`
	Lost          int `json:"lost"`
	SelfDropped   int `json:"selfDropped"`
	Unknown       int `json:"unknown"`
	CertsExpiring int `json:"certsExpiring"` // valid but within the warn window
	CertsExpired  int `json:"certsExpired"`  // already past expiry
	CertWarnDays  int `json:"certWarnDays"`  // the warn window, in whole days
}

type nodeRegistry struct {
	nodes         dbsql.IGenericRepo[entities.ManagedNode]
	settings      dbsql.IGenericRepo[entities.ControlSetting]
	ca            *fleetCA
	cfg           NodeRegistryConfig
	parentBaseURL string
	secretCipher  *atrest.Cipher
	bootstrapHTTP *http.Client

	presenceMu sync.RWMutex
	presence   func(nodeID string) bool

	eventMu   sync.RWMutex
	eventSink FleetEventSink

	// certMu guards certWarned, the per-node dedup of certificate-expiry warnings:
	// nodeID → the CertExpiresAt value last warned about, so a renewal (which pushes
	// the expiry out) re-arms the warning for the next approach.
	certMu     sync.Mutex
	certWarned map[string]int64
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
		ca:            newFleetCA(settings, cfg.ParentID, cfg.CertTTL, cfg.SecretCipher),
		cfg:           cfg,
		parentBaseURL: strings.TrimRight(cfg.ParentBaseURL, "/"),
		secretCipher:  cfg.SecretCipher,
		certWarned:    map[string]int64{},
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
	// The PSK is a secret stored encrypted at rest (legacy plaintext passes through).
	return decodeSecret(s.secretCipher, row.Value), nil
}

func (s *nodeRegistry) SetFleetKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if len(key) < 16 {
		return fmt.Errorf("fleet key must be at least 16 characters")
	}
	return s.upsertFleetKey(ctx, key)
}

func (s *nodeRegistry) GenerateFleetKey(ctx context.Context) (string, error) {
	b := make([]byte, fleetKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(b)
	if err := s.upsertFleetKey(ctx, key); err != nil {
		return "", err
	}
	return key, nil
}

// upsertFleetKey stores the fleet PSK, encrypting it at rest when a cipher is configured.
func (s *nodeRegistry) upsertFleetKey(ctx context.Context, key string) error {
	stored, err := encodeSecret(s.secretCipher, key)
	if err != nil {
		return err
	}
	return s.upsertSetting(ctx, fleetKeySettingKey, stored)
}

// nodeListPageSize is the page size List uses to walk the whole fleet. Paging (rather
// than a single hardcoded cap) means the operator list, Scan dedup, and Heartbeat all
// see every adopted node instead of silently truncating at a fixed limit.
const nodeListPageSize = 500

func (s *nodeRegistry) List(ctx context.Context) ([]*entities.ManagedNode, error) {
	var all []*entities.ManagedNode
	var offset uint64
	for {
		rows, _, err := s.nodes.Get(ctx, "", nodeListPageSize, offset, nil, nil)
		if err != nil {
			if isNoResultFoundErr(err) {
				break
			}
			return nil, err
		}
		all = append(all, rows...)
		if uint64(len(rows)) < nodeListPageSize {
			break
		}
		offset += nodeListPageSize
	}
	if all == nil {
		return []*entities.ManagedNode{}, nil
	}
	return all, nil
}

// FleetStatus rolls up liveness and certificate health across all adopted nodes.
func (s *nodeRegistry) FleetStatus(ctx context.Context) (FleetStatus, error) {
	nodes, err := s.List(ctx)
	if err != nil {
		return FleetStatus{}, err
	}
	warn := s.certWarnSeconds()
	now := time.Now().Unix()
	out := FleetStatus{Total: len(nodes), CertWarnDays: int(warn / 86400)}
	for _, node := range nodes {
		switch strings.ToLower(node.Status) {
		case "online":
			out.Online++
		case "lost":
			out.Lost++
		case "self-dropped":
			out.SelfDropped++
		default:
			out.Unknown++
		}
		if exp := node.CertExpiresAt; exp > 0 {
			switch {
			case exp <= now:
				out.CertsExpired++
			case exp-now <= warn:
				out.CertsExpiring++
			}
		}
	}
	return out, nil
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
			NodeID:  r.NodeID,
			Name:    r.Name,
			Version: r.Version,
			// Advisory only — see DiscoveredNode.Kind.
			Kind:      r.Kind,
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
		NodeId: nodeID,
		// The AUTHORITATIVE kind: the node told us over the adopt call, which is fleet-key-signed
		// and claim-code-gated. An empty answer means a node that predates the field, and every
		// one of those is a camera.
		Kind: firstNonEmpty(res.Kind, "camera"),
		// Operator's chosen label wins; fall back to the node's reported hostname.
		Name:        firstNonEmpty(in.Name, res.Name),
		Description: strings.TrimSpace(in.Description),
		Icon:        strings.TrimSpace(in.Icon),
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
		// CRITICAL: the node has ALREADY committed Paired=true and consumed its single-use
		// claim code before replying to us. If we cannot persist our record now, the node is
		// stranded — paired to us with no row on our side — so it dials the control channel
		// forever as "unknown", disappears from discovery (a paired node stops answering
		// scans), and every retry fails with "invalid or expired claim code" because the code
		// is already burned. Roll the node back to unpaired using the token it just gave us, so
		// it becomes discoverable and adoptable again. Then surface a clean error, not the raw
		// DB string.
		s.rollbackNodePairing(ctx, baseURL, res.Token)
		return nil, fmt.Errorf("%w: %v", ErrAdoptPersist, err)
	}
	saved, err := s.nodes.GetByUnique(ctx, "", "node_id", node.NodeId)
	if err != nil || saved == nil {
		// The write succeeded but the read-back glitched; return what we just persisted rather
		// than a nil node (which the API layer would render as an empty success).
		nodeCopy := node
		return &nodeCopy, nil
	}
	return saved, nil
}

// rollbackNodePairing best-effort tells a node to release (unpair) using the token from a
// just-completed adopt reply. Used when the parent fails to persist the adoption: without it
// the node is left paired-but-recordless (see Adopt). Errors are swallowed — this is a
// recovery path and the node also ages out of trust on its own.
func (s *nodeRegistry) rollbackNodePairing(ctx context.Context, baseURL, token string) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(baseURL) == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"token": token})
	_, _ = s.postNode(ctx, baseURL+"/api/pairing/release", body)
}

// RevokeNode blocklists a node's certificate at the fleet CA. Future control-channel
// connections and enroll/renew attempts from that node are refused. Used to shut down a
// stranded/orphaned node that keeps dialing with a valid cert but has no managed record.
func (s *nodeRegistry) RevokeNode(ctx context.Context, nodeID string) error {
	return s.ca.Revoke(ctx, nodeID)
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

// UpdateMeta edits an adopted node's operator-facing fields only (name/description/icon).
func (s *nodeRegistry) UpdateMeta(ctx context.Context, nodeID, name, description, icon string, updatedBy int64) (*entities.ManagedNode, error) {
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, ErrNodeUnknown
	}
	node.Name = strings.TrimSpace(name)
	node.Description = strings.TrimSpace(description)
	if i := strings.TrimSpace(icon); i != "" {
		node.Icon = i
	}
	node.UpdatedBy = updatedBy
	node.UpdatedAt = time.Now().Unix()
	if _, err := s.nodes.UpdateById(ctx, "", *node); err != nil {
		return nil, err
	}
	return node, nil
}

// UpdatePosition sets a node's geographic coordinates and placed flag. See INodeRegistry.
func (s *nodeRegistry) UpdatePosition(ctx context.Context, nodeID string, lat, lon float64, placed bool, updatedBy int64) (*entities.ManagedNode, error) {
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, ErrNodeUnknown
	}
	node.Lat = lat
	node.Lon = lon
	node.MapPlaced = placed
	node.UpdatedBy = updatedBy
	node.UpdatedAt = time.Now().Unix()
	if _, err := s.nodes.UpdateById(ctx, "", *node); err != nil {
		return nil, err
	}
	return node, nil
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

// SetControlPresence injects the control-channel liveness oracle. See INodeRegistry.
func (s *nodeRegistry) SetControlPresence(connected func(nodeID string) bool) {
	s.presenceMu.Lock()
	s.presence = connected
	s.presenceMu.Unlock()
}

// controlConnected reports whether the node currently holds a live control channel.
func (s *nodeRegistry) controlConnected(nodeID string) bool {
	s.presenceMu.RLock()
	fn := s.presence
	s.presenceMu.RUnlock()
	return fn != nil && fn(nodeID)
}

func (s *nodeRegistry) SetFleetEventSink(sink FleetEventSink) {
	s.eventMu.Lock()
	s.eventSink = sink
	s.eventMu.Unlock()
}

// emitFleetEvent hands a detected fleet-health transition to the sink (if any).
func (s *nodeRegistry) emitFleetEvent(e FleetEvent) {
	s.eventMu.RLock()
	sink := s.eventSink
	s.eventMu.RUnlock()
	if sink != nil {
		sink(e)
	}
}

// certWarnSeconds is the certificate-expiry warning lead time in seconds.
func (s *nodeRegistry) certWarnSeconds() int64 {
	w := s.cfg.CertWarnBefore
	if w <= 0 {
		w = defaultCertWarnBefore
	}
	return int64(w.Seconds())
}

// checkCertExpiry raises a one-shot warning when a node's certificate is within the
// warn window (or already expired). It emits at most once per distinct expiry value,
// so a renewal that pushes the expiry out re-arms the warning; a node that renews
// before the window is reached never warns. It never writes the node record — the
// expiry was persisted at enrollment.
func (s *nodeRegistry) checkCertExpiry(node *entities.ManagedNode, now int64) {
	exp := node.CertExpiresAt
	if exp <= 0 {
		return
	}
	if exp-now > s.certWarnSeconds() {
		// Healthy and outside the window: forget any prior warning so a later approach
		// (e.g. after this session already warned once) can warn again.
		s.certMu.Lock()
		delete(s.certWarned, node.NodeId)
		s.certMu.Unlock()
		return
	}
	s.certMu.Lock()
	already := s.certWarned[node.NodeId] == exp
	if !already {
		s.certWarned[node.NodeId] = exp
	}
	s.certMu.Unlock()
	if already {
		return
	}
	s.emitFleetEvent(FleetEvent{
		Kind:      FleetEventCertExpiring,
		Node:      node,
		ExpiresAt: exp,
		HoursLeft: int((exp - now) / 3600),
	})
}

// lostGraceSeconds is how long a node may go with no contact — neither a live control
// channel nor a successful mTLS poll — before it is declared lost. Three heartbeat
// intervals (floored at 90s) absorbs a control-channel reconnect or a single missed
// poll without flapping the node offline.
func (s *nodeRegistry) lostGraceSeconds() int64 {
	iv := s.cfg.HeartbeatInterval
	if iv <= 0 {
		iv = 60 * time.Second
	}
	g := int64((3 * iv).Seconds())
	if g < 90 {
		g = 90
	}
	return g
}

// Heartbeat reconciles every adopted node's status. The persistent node-dialed
// control channel is the authoritative liveness signal — it survives NAT / firewalls /
// re-IP, so a node holding a live connection is online regardless of whether the
// parent can reach its mTLS port directly. The mTLS poll is only a fallback, and a
// node is declared lost only after the grace window with no contact on either path,
// so a brief channel blip can no longer flap a healthy node offline. A node that
// self-dropped (status already "self-dropped") is left as-is.
func (s *nodeRegistry) Heartbeat(ctx context.Context) {
	nodes, err := s.List(ctx)
	if err != nil {
		return
	}
	grace := s.lostGraceSeconds()
	now := time.Now().Unix()

	// Phase 1 — liveness. Control-channel presence is an in-memory lookup (instant);
	// the mTLS poll is a synchronous network call with a per-probe timeout, so a fleet
	// with several unreachable nodes would blow the whole sweep past the heartbeat
	// interval if probed serially. Probe only the not-control-connected nodes, and do
	// so concurrently under a bounded worker pool + a per-sweep budget.
	var toProbe []*entities.ManagedNode
	controlAlive := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node.Status == "self-dropped" {
			continue
		}
		// Certificate health is independent of liveness — a reachable node with a stale
		// cert still needs attention — so check it every sweep for every node.
		s.checkCertExpiry(node, now)
		if s.controlConnected(node.NodeId) {
			controlAlive[node.NodeId] = true
		} else {
			toProbe = append(toProbe, node)
		}
	}
	probeAlive := s.probeNodesConcurrently(ctx, toProbe)

	// Phase 2 — reconcile + persist. Writes stay serial (the on-prem store is a
	// single-writer sqlite) but the slow part (the probes) already happened in parallel.
	for _, node := range nodes {
		if node.Status == "self-dropped" {
			continue
		}
		prev := node.Status
		alive := controlAlive[node.NodeId] || probeAlive[node.NodeId]
		switch {
		case alive:
			node.Status = "online"
			node.LastSeenAt = now
			if prev == "lost" {
				s.emitFleetEvent(FleetEvent{Kind: FleetEventNodeRecovered, Node: node})
			}
		case now-node.LastSeenAt >= grace:
			if prev == "lost" {
				// Already lost and still is: nothing changed, skip the write and the
				// re-notification (the lost event is edge-triggered, fired once).
				continue
			}
			node.Status = "lost"
			s.emitFleetEvent(FleetEvent{Kind: FleetEventNodeLost, Node: node})
		default:
			// Within the grace window with no contact: hold the prior status (still
			// online) rather than flap, and skip the needless write.
			continue
		}
		node.UpdatedAt = now
		_, _ = s.nodes.UpdateById(ctx, "", *node)
	}
}

// heartbeatProbeConcurrency bounds how many mTLS liveness probes run at once so a
// large fleet doesn't open an unbounded number of sockets in a single sweep.
const heartbeatProbeConcurrency = 16

// heartbeatSweepBudget caps the wall-clock time all probes in one sweep may take, so
// a batch of unreachable nodes (each hitting the per-probe timeout) can never stall
// reconciliation indefinitely. Nodes not resolved within the budget are treated as
// not-yet-alive this sweep; the grace window keeps them from flapping offline.
const heartbeatSweepBudget = 30 * time.Second

// probeNodesConcurrently mTLS-probes the given nodes with a bounded worker pool and a
// per-sweep budget, returning the set of node ids that answered.
func (s *nodeRegistry) probeNodesConcurrently(ctx context.Context, nodes []*entities.ManagedNode) map[string]bool {
	alive := make(map[string]bool, len(nodes))
	if len(nodes) == 0 {
		return alive
	}
	sweepCtx, cancel := context.WithTimeout(ctx, heartbeatSweepBudget)
	defer cancel()

	workers := heartbeatProbeConcurrency
	if len(nodes) < workers {
		workers = len(nodes)
	}
	jobs := make(chan *entities.ManagedNode)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for node := range jobs {
				if s.probeOverMTLS(sweepCtx, node) {
					mu.Lock()
					alive[node.NodeId] = true
					mu.Unlock()
				}
			}
		}()
	}
	for _, node := range nodes {
		select {
		case <-sweepCtx.Done():
			// Budget exhausted — stop dispatching; unprobed nodes are handled by grace.
			close(jobs)
			wg.Wait()
			return alive
		case jobs <- node:
		}
	}
	close(jobs)
	wg.Wait()
	return alive
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
	// Check revocation FIRST: a revoked node has no valid claim on the fleet even if its row
	// still exists, and an operator who blocked a stranded (row-less) node needs that block to
	// register as "revoked", not get masked by the "unknown" (no-row) case below.
	if revoked, _ := s.ca.IsRevoked(ctx, nodeID); revoked {
		return nil, ErrNodeRevoked
	}
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeID)
	if err != nil || node == nil {
		return nil, ErrNodeUnknown
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
	// Kind is the node telling us what it is, over a call it authenticated with the fleet key and
	// a claim code the operator read off its screen. This — not the multicast announce — is the
	// value we store and trust.
	Kind string `json:"kind,omitempty"`
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
