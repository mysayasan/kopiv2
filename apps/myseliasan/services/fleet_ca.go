package services

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/fleetca"
)

const (
	caCertKey      = "pairing.caCert"
	caKeyKey       = "pairing.caKey"
	parentCertKey  = "pairing.parentCert"
	parentKeyKey   = "pairing.parentKey"
	revokedKey     = "pairing.revoked"
	caValidFor     = 10 * 365 * 24 * time.Hour
	parentCertTTL  = 90 * 24 * time.Hour
	parentRenewAt  = 7 * 24 * time.Hour
	defaultCertTTL = 7 * 24 * time.Hour
)

// fleetCA wraps the on-prem certificate authority for the pairing mTLS channel. It
// is the trust root for node and control-plane certificates and is entirely
// self-contained — it never contacts myidsan. The CA key is stored in the control
// plane database (ControlSetting); on-prem, local DB.
type fleetCA struct {
	settings dbsql.IGenericRepo[entities.ControlSetting]
	parentID string
	certTTL  time.Duration

	mu sync.Mutex
	ca *fleetca.CA
}

func newFleetCA(settings dbsql.IGenericRepo[entities.ControlSetting], parentID string, certTTL time.Duration) *fleetCA {
	if certTTL <= 0 {
		certTTL = defaultCertTTL
	}
	return &fleetCA{settings: settings, parentID: parentID, certTTL: certTTL}
}

// ensure loads the CA from storage or creates and persists a new one.
func (f *fleetCA) ensure(ctx context.Context) (*fleetca.CA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ca != nil {
		return f.ca, nil
	}
	certPEM, _ := f.read(ctx, caCertKey)
	keyPEM, _ := f.read(ctx, caKeyKey)
	if certPEM != "" && keyPEM != "" {
		ca, err := fleetca.LoadCA([]byte(certPEM), []byte(keyPEM))
		if err == nil {
			f.ca = ca
			return ca, nil
		}
		// fall through to regenerate on a corrupt/incompatible stored CA
	}
	ca, err := fleetca.NewCA("myseliasan-fleet-ca", caValidFor)
	if err != nil {
		return nil, err
	}
	if err := f.write(ctx, caCertKey, string(ca.CertPEM())); err != nil {
		return nil, err
	}
	if err := f.write(ctx, caKeyKey, string(ca.KeyPEM())); err != nil {
		return nil, err
	}
	f.ca = ca
	return ca, nil
}

// CARootPEM returns the CA public certificate distributed to nodes.
func (f *fleetCA) CARootPEM(ctx context.Context) ([]byte, error) {
	ca, err := f.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return ca.CertPEM(), nil
}

// SignNodeCSR issues a node certificate (CN = nodeID) from a CSR, unless the node
// is revoked. Returns the leaf cert PEM and the CA root PEM.
func (f *fleetCA) SignNodeCSR(ctx context.Context, nodeID string, csrPEM []byte) (certPEM, caRootPEM []byte, err error) {
	if revoked, _ := f.IsRevoked(ctx, nodeID); revoked {
		return nil, nil, ErrNodeRevoked
	}
	ca, err := f.ensure(ctx)
	if err != nil {
		return nil, nil, err
	}
	leaf, err := ca.IssueFromCSR(csrPEM, nodeID, f.certTTL, nil)
	if err != nil {
		return nil, nil, err
	}
	return leaf, ca.CertPEM(), nil
}

// ParentClientTLS returns a tls.Config for the control plane to dial a node's mTLS
// management listener, verifying the node's server cert CN == expectNodeID. The
// parent's own client cert is minted (and cached) on first use and renewed as it
// nears expiry.
func (f *fleetCA) ParentClientTLS(ctx context.Context, expectNodeID string) (certPEM, keyPEM, caRootPEM []byte, err error) {
	ca, err := f.ensure(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	certPEM, keyPEM, err = f.parentCert(ctx, ca)
	if err != nil {
		return nil, nil, nil, err
	}
	return certPEM, keyPEM, ca.CertPEM(), nil
}

func (f *fleetCA) parentCert(ctx context.Context, ca *fleetca.CA) (certPEM, keyPEM []byte, err error) {
	cert, _ := f.read(ctx, parentCertKey)
	key, _ := f.read(ctx, parentKeyKey)
	if cert != "" && key != "" && !certNearExpiry([]byte(cert), parentRenewAt) {
		return []byte(cert), []byte(key), nil
	}
	c, k, err := ca.IssueLeaf(f.parentID, parentCertTTL, nil)
	if err != nil {
		return nil, nil, err
	}
	_ = f.write(ctx, parentCertKey, string(c))
	_ = f.write(ctx, parentKeyKey, string(k))
	return c, k, nil
}

// Revoke records a nodeID as revoked so it can no longer renew its certificate.
func (f *fleetCA) Revoke(ctx context.Context, nodeID string) error {
	set, _ := f.revokedSet(ctx)
	if set == nil {
		set = map[string]bool{}
	}
	set[nodeID] = true
	return f.writeRevoked(ctx, set)
}

// Unrevoke clears a revocation (e.g. when re-adopting a previously released node).
func (f *fleetCA) Unrevoke(ctx context.Context, nodeID string) error {
	set, _ := f.revokedSet(ctx)
	if set == nil || !set[nodeID] {
		return nil
	}
	delete(set, nodeID)
	return f.writeRevoked(ctx, set)
}

func (f *fleetCA) IsRevoked(ctx context.Context, nodeID string) (bool, error) {
	set, err := f.revokedSet(ctx)
	if err != nil {
		return false, err
	}
	return set[nodeID], nil
}

func (f *fleetCA) revokedSet(ctx context.Context) (map[string]bool, error) {
	raw, err := f.read(ctx, revokedKey)
	if err != nil || raw == "" {
		return map[string]bool{}, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return map[string]bool{}, nil
	}
	set := make(map[string]bool, len(list))
	for _, id := range list {
		set[id] = true
	}
	return set, nil
}

func (f *fleetCA) writeRevoked(ctx context.Context, set map[string]bool) error {
	list := make([]string, 0, len(set))
	for id := range set {
		list = append(list, id)
	}
	payload, _ := json.Marshal(list)
	return f.write(ctx, revokedKey, string(payload))
}

func (f *fleetCA) read(ctx context.Context, key string) (string, error) {
	row, err := f.settings.GetByUnique(ctx, "", "key", key)
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

// certNearExpiry reports whether the leaf in certPEM expires within `within` of
// now (or is unparseable, in which case it should be reissued).
func certNearExpiry(certPEM []byte, within time.Duration) bool {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return true
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	return time.Now().Add(within).After(leaf.NotAfter)
}

func (f *fleetCA) write(ctx context.Context, key, value string) error {
	now := time.Now().Unix()
	row, err := f.settings.GetByUnique(ctx, "", "key", key)
	if err != nil {
		if isNoResultFoundErr(err) {
			_, cerr := f.settings.Create(ctx, "", entities.ControlSetting{Key: key, Value: value, CreatedAt: now, UpdatedAt: now})
			return cerr
		}
		return err
	}
	row.Value = value
	row.UpdatedAt = now
	_, err = f.settings.UpdateById(ctx, "", *row)
	return err
}
