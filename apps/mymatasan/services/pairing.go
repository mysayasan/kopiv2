package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

const (
	pairingStateKey   = "pairing.state"
	pairingFleetKey   = "pairing.fleetKey"
	pairingNodeIDKey  = "pairing.nodeId"
	pairingEnrollKey  = "pairing.enrollment"
	claimCodeTTL      = 10 * time.Minute
	pairingTokenBytes = 32
	fleetKeyMinLen    = 16
	// assertionWindow bounds how stale an adoption assertion may be. Tighter than
	// the discovery replay window because adoption is a single deliberate call.
	assertionWindow = 60 * time.Second
)

// Pairing errors. The API layer maps these to HTTP statuses (already-paired → 409,
// the auth failures → 401/400) so the control plane can react.
var (
	ErrPairingAlreadyPaired = errors.New("node is already paired to a control plane")
	ErrPairingBadAssertion  = errors.New("invalid fleet-key assertion")
	ErrPairingBadClaimCode  = errors.New("invalid or expired claim code")
	ErrPairingBadToken      = errors.New("invalid pairing token")
	ErrPairingFleetKeyUnset = errors.New("fleet key is not configured")
	ErrPairingFleetKeyShort = errors.New("fleet key is too short")
)

// PairingStatus is the UI-facing view of the node's pairing state. It never
// includes the fleet key or pairing token — only whether they are set.
type PairingStatus struct {
	NodeID          string `json:"nodeId"`
	Name            string `json:"name"`
	Paired          bool   `json:"paired"`
	ParentID        string `json:"parentId,omitempty"`
	ParentName      string `json:"parentName,omitempty"`
	ParentBaseURL   string `json:"parentBaseUrl,omitempty"`
	PairedAt        int64  `json:"pairedAt,omitempty"`
	FleetKeySet     bool   `json:"fleetKeySet"`
	Discoverable    bool   `json:"discoverable"`
	ClaimCodeActive bool   `json:"claimCodeActive"`
}

// AdoptRequest is the control plane's bind call. Assertion =
// SignAssertion(fleetKey, parentId, nonce, ts), which proves the caller holds the
// fleet key. ClaimCode is the short-lived code an admin generated on the node.
type AdoptRequest struct {
	ParentID      string `json:"parentId"`
	ParentName    string `json:"parentName"`
	ParentBaseURL string `json:"parentBaseUrl"`
	ClaimCode     string `json:"claimCode"`
	Nonce         string `json:"nonce"`
	Timestamp     int64  `json:"ts"`
	Assertion     string `json:"assertion"`
}

// AdoptResult is returned to the control plane on a successful bind. Token is the
// long random secret the parent presents on later release; the node stores only
// its hash.
type AdoptResult struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Token  string `json:"token"`
}

// Enrollment is the node's mTLS material: the pairing token (used to authenticate
// the node→parent enroll/renew call) and the CA-issued certificate bundle. Stored
// encrypted at rest.
type Enrollment struct {
	Token         string `json:"token"`
	NodeCertPEM   string `json:"nodeCert"`
	NodeKeyPEM    string `json:"nodeKey"`
	CARootPEM     string `json:"caRoot"`
	CertExpiresAt int64  `json:"certExpiresAt"`
}

// HasCert reports whether a usable certificate bundle is present.
func (e Enrollment) HasCert() bool {
	return e.NodeCertPEM != "" && e.NodeKeyPEM != "" && e.CARootPEM != ""
}

// IPairingService owns the node's single-parent lock state machine.
type IPairingService interface {
	Status(ctx context.Context) (PairingStatus, error)
	FleetKey(ctx context.Context) ([]byte, error)
	SetFleetKey(ctx context.Context, key string) error
	Discoverable(ctx context.Context) bool
	AnnounceInfo(ctx context.Context, httpsPort int) pairing.AnnounceInfo
	GenerateClaimCode(ctx context.Context) (code string, expiresAt int64, err error)
	Adopt(ctx context.Context, req AdoptRequest) (AdoptResult, error)
	Release(ctx context.Context, token string) error
	Unpair(ctx context.Context) (parentBaseURL string, err error)
	// NodeID returns the node's stable identity.
	NodeID(ctx context.Context) (string, error)
	// ParentBaseURL returns the bound parent's URL (empty when unpaired).
	ParentBaseURL(ctx context.Context) (string, error)
	// Enrollment / cert bundle accessors for the mTLS channel.
	Enrollment(ctx context.Context) (Enrollment, error)
	SaveCert(ctx context.Context, certPEM, keyPEM, caRootPEM string, expiresAt int64) error
}

// pairingState is the persisted singleton (one runtime-setting row). The token is
// kept only as a hash; the fleet key lives in its own (encrypted) row.
type pairingState struct {
	Paired        bool   `json:"paired"`
	ParentID      string `json:"parentId,omitempty"`
	ParentName    string `json:"parentName,omitempty"`
	ParentBaseURL string `json:"parentBaseUrl,omitempty"`
	PairedAt      int64  `json:"pairedAt,omitempty"`
	TokenHash     string `json:"tokenHash,omitempty"`
}

type pairingService struct {
	repo    dbsql.IGenericRepo[entities.RuntimeSetting]
	cipher  *atrest.Cipher
	name    string
	version string

	mu          sync.Mutex
	nodeIDCache string
	claimCode   string
	claimExp    time.Time
}

// NewPairingService builds the node pairing service. name defaults to the host
// name when empty; cipher (may be nil) encrypts the fleet key at rest.
func NewPairingService(repo dbsql.IGenericRepo[entities.RuntimeSetting], cipher *atrest.Cipher, name, version string) IPairingService {
	if strings.TrimSpace(name) == "" {
		if hn, err := os.Hostname(); err == nil {
			name = hn
		} else {
			name = "mymatasan"
		}
	}
	return &pairingService{repo: repo, cipher: cipher, name: name, version: version}
}

func (s *pairingService) Status(ctx context.Context) (PairingStatus, error) {
	st, err := s.load(ctx)
	if err != nil {
		return PairingStatus{}, err
	}
	key, _ := s.FleetKey(ctx)
	nodeID, _ := s.nodeID(ctx)
	s.mu.Lock()
	claimActive := s.claimCode != "" && time.Now().Before(s.claimExp)
	s.mu.Unlock()
	return PairingStatus{
		NodeID:          nodeID,
		Name:            s.name,
		Paired:          st.Paired,
		ParentID:        st.ParentID,
		ParentName:      st.ParentName,
		ParentBaseURL:   st.ParentBaseURL,
		PairedAt:        st.PairedAt,
		FleetKeySet:     len(key) > 0,
		Discoverable:    !st.Paired && len(key) > 0,
		ClaimCodeActive: claimActive,
	}, nil
}

// FleetKey returns the decrypted fleet key bytes, or nil when unset.
func (s *pairingService) FleetKey(ctx context.Context) ([]byte, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", pairingFleetKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if row == nil || row.Value == "" {
		return nil, nil
	}
	stored, err := base64.StdEncoding.DecodeString(row.Value)
	if err != nil {
		return nil, err
	}
	if s.cipher != nil {
		return s.cipher.DecryptBytes(stored) // passthrough if not encrypted
	}
	return stored, nil
}

func (s *pairingService) SetFleetKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if len(key) < fleetKeyMinLen {
		return ErrPairingFleetKeyShort
	}
	stored := []byte(key)
	if s.cipher != nil {
		enc, err := s.cipher.EncryptBytes(stored)
		if err != nil {
			return err
		}
		stored = enc
	}
	return s.upsert(ctx, pairingFleetKey, base64.StdEncoding.EncodeToString(stored))
}

func (s *pairingService) Discoverable(ctx context.Context) bool {
	st, err := s.load(ctx)
	if err != nil || st.Paired {
		return false
	}
	key, _ := s.FleetKey(ctx)
	return len(key) > 0
}

func (s *pairingService) AnnounceInfo(ctx context.Context, httpsPort int) pairing.AnnounceInfo {
	nodeID, _ := s.nodeID(ctx)
	return pairing.AnnounceInfo{
		NodeID:    nodeID,
		Name:      s.name,
		Version:   s.version,
		HTTPSPort: httpsPort,
	}
}

func (s *pairingService) GenerateClaimCode(ctx context.Context) (string, int64, error) {
	code, err := newClaimCode()
	if err != nil {
		return "", 0, err
	}
	exp := time.Now().Add(claimCodeTTL)
	s.mu.Lock()
	s.claimCode = code
	s.claimExp = exp
	s.mu.Unlock()
	return code, exp.Unix(), nil
}

func (s *pairingService) Adopt(ctx context.Context, req AdoptRequest) (AdoptResult, error) {
	key, err := s.FleetKey(ctx)
	if err != nil {
		return AdoptResult{}, err
	}
	if len(key) == 0 {
		return AdoptResult{}, ErrPairingFleetKeyUnset
	}
	// 1. Authenticate the caller: valid fleet-key assertion + fresh timestamp.
	if !pairing.AssertionFresh(req.Timestamp, assertionWindow) ||
		!pairing.VerifyAssertion(key, req.Assertion, req.ParentID, req.Nonce, strconv.FormatInt(req.Timestamp, 10)) {
		return AdoptResult{}, ErrPairingBadAssertion
	}

	// 2. Serialize the bind so two control planes can't both win the lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	// 3. Confirm the operator's claim code (node-side adoption gate).
	if !s.claimCodeValidLocked(req.ClaimCode) {
		return AdoptResult{}, ErrPairingBadClaimCode
	}

	st, err := s.load(ctx)
	if err != nil {
		return AdoptResult{}, err
	}
	if st.Paired {
		return AdoptResult{}, ErrPairingAlreadyPaired
	}

	token, err := newToken()
	if err != nil {
		return AdoptResult{}, err
	}
	st = pairingState{
		Paired:        true,
		ParentID:      strings.TrimSpace(req.ParentID),
		ParentName:    strings.TrimSpace(req.ParentName),
		ParentBaseURL: strings.TrimSpace(req.ParentBaseURL),
		PairedAt:      time.Now().Unix(),
		TokenHash:     sha256Hex(token),
	}
	if err := s.saveState(ctx, st); err != nil {
		return AdoptResult{}, err
	}
	// Persist the plaintext token (encrypted) so the node can authenticate its
	// subsequent enroll/renew calls to the parent. Cert fields fill in after enroll.
	if err := s.saveEnrollment(ctx, Enrollment{Token: token}); err != nil {
		return AdoptResult{}, err
	}
	s.claimCode = "" // single-use: consume the code on success

	nodeID, _ := s.nodeIDLocked(ctx)
	return AdoptResult{NodeID: nodeID, Name: s.name, Token: token}, nil
}

func (s *pairingService) Release(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load(ctx)
	if err != nil {
		return err
	}
	if !st.Paired {
		return nil // already released — idempotent
	}
	if st.TokenHash == "" ||
		subtle.ConstantTimeCompare([]byte(sha256Hex(token)), []byte(st.TokenHash)) != 1 {
		return ErrPairingBadToken
	}
	_ = s.clearEnrollment(ctx)
	return s.saveState(ctx, pairingState{})
}

func (s *pairingService) Unpair(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	parentURL := st.ParentBaseURL
	_ = s.clearEnrollment(ctx)
	if err := s.saveState(ctx, pairingState{}); err != nil {
		return "", err
	}
	return parentURL, nil
}

// NodeID returns the node's stable identity.
func (s *pairingService) NodeID(ctx context.Context) (string, error) {
	return s.nodeID(ctx)
}

// ParentBaseURL returns the bound parent's URL, or "" when unpaired.
func (s *pairingService) ParentBaseURL(ctx context.Context) (string, error) {
	st, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	return st.ParentBaseURL, nil
}

// Enrollment returns the stored mTLS material (decrypted), or a zero value.
func (s *pairingService) Enrollment(ctx context.Context) (Enrollment, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", pairingEnrollKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return Enrollment{}, nil
		}
		return Enrollment{}, err
	}
	if row == nil || row.Value == "" {
		return Enrollment{}, nil
	}
	raw, err := base64.StdEncoding.DecodeString(row.Value)
	if err != nil {
		return Enrollment{}, err
	}
	if s.cipher != nil {
		if raw, err = s.cipher.DecryptBytes(raw); err != nil {
			return Enrollment{}, err
		}
	}
	var e Enrollment
	if err := json.Unmarshal(raw, &e); err != nil {
		return Enrollment{}, nil
	}
	return e, nil
}

// SaveCert stores the issued certificate bundle (preserving the existing token).
func (s *pairingService) SaveCert(ctx context.Context, certPEM, keyPEM, caRootPEM string, expiresAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.Enrollment(ctx)
	if err != nil {
		return err
	}
	e.NodeCertPEM = certPEM
	e.NodeKeyPEM = keyPEM
	e.CARootPEM = caRootPEM
	e.CertExpiresAt = expiresAt
	return s.saveEnrollment(ctx, e)
}

func (s *pairingService) saveEnrollment(ctx context.Context, e Enrollment) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	stored := payload
	if s.cipher != nil {
		if stored, err = s.cipher.EncryptBytes(payload); err != nil {
			return err
		}
	}
	return s.upsert(ctx, pairingEnrollKey, base64.StdEncoding.EncodeToString(stored))
}

func (s *pairingService) clearEnrollment(ctx context.Context) error {
	return s.upsert(ctx, pairingEnrollKey, "")
}

// claimCodeValidLocked checks the supplied code against the active one in
// constant time. Caller must hold s.mu.
func (s *pairingService) claimCodeValidLocked(code string) bool {
	code = strings.TrimSpace(strings.ToUpper(code))
	if s.claimCode == "" || code == "" || time.Now().After(s.claimExp) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(code), []byte(s.claimCode)) == 1
}

// nodeID returns the node's stable identity, generating and persisting one on
// first use.
func (s *pairingService) nodeID(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeIDLocked(ctx)
}

func (s *pairingService) nodeIDLocked(ctx context.Context) (string, error) {
	if s.nodeIDCache != "" {
		return s.nodeIDCache, nil
	}
	row, err := s.repo.GetByUnique(ctx, "", "key", pairingNodeIDKey)
	if err != nil && !isNoResultFoundErr(err) {
		return "", err
	}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		s.nodeIDCache = row.Value
		return row.Value, nil
	}
	id := uuid.NewString()
	if err := s.upsert(ctx, pairingNodeIDKey, id); err != nil {
		return "", err
	}
	s.nodeIDCache = id
	return id, nil
}

func (s *pairingService) load(ctx context.Context) (pairingState, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", pairingStateKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return pairingState{}, nil
		}
		return pairingState{}, err
	}
	if row == nil || row.Value == "" {
		return pairingState{}, nil
	}
	var st pairingState
	if err := json.Unmarshal([]byte(row.Value), &st); err != nil {
		return pairingState{}, nil // corrupt → treat as unpaired
	}
	return st, nil
}

func (s *pairingService) saveState(ctx context.Context, st pairingState) error {
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.upsert(ctx, pairingStateKey, string(payload))
}

// upsert writes a single runtime-setting key, creating or updating as needed.
func (s *pairingService) upsert(ctx context.Context, key, value string) error {
	now := time.Now().Unix()
	row, err := s.repo.GetByUnique(ctx, "", "key", key)
	if err != nil {
		if isNoResultFoundErr(err) {
			_, cerr := s.repo.Create(ctx, "", entities.RuntimeSetting{
				Key:       key,
				Value:     value,
				CreatedAt: now,
				UpdatedAt: now,
			})
			return cerr
		}
		return err
	}
	row.Value = value
	row.UpdatedAt = now
	_, err = s.repo.UpdateById(ctx, "", *row)
	return err
}

func newToken() (string, error) {
	b := make([]byte, pairingTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newClaimCode returns a short, human-typeable code (Crockford-ish base32, upper).
func newClaimCode() (string, error) {
	b := make([]byte, 5) // 8 base32 chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return strings.ToUpper(enc), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
