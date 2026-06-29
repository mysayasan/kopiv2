package services

import (
	"context"
	"sync"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/fleetca"
)

const (
	ssoCaName        = "default"
	ssoCaValidFor    = 10 * 365 * 24 * time.Hour
	ssoClientCertTTL = 365 * 24 * time.Hour
)

// ISsoCaService issues relying-app SSO client certificates from myidsan's own
// certificate authority. The CA (cert + private key) is persisted as a singleton row
// so issued leaves stay verifiable across restarts; issuing is idempotent on the CA
// and produces a fresh client key pair each time. Reuses infra/fleetca (the same CA
// primitives that back fleet-node mTLS).
type ISsoCaService interface {
	// CARootPEM returns the CA public certificate (the trust anchor a verifier loads).
	CARootPEM(ctx context.Context) ([]byte, error)
	// IssueClient mints a client leaf (CN = clientId) and returns it with its private
	// key and the CA cert. The key is returned only here — it is never stored.
	IssueClient(ctx context.Context, clientId string) (clientCertPEM, clientKeyPEM, caCertPEM []byte, err error)
}

type ssoCaService struct {
	repo dbsql.IGenericRepo[myidsanentities.SsoCa]

	mu sync.Mutex
	ca *fleetca.CA
}

func NewSsoCaService(repo dbsql.IGenericRepo[myidsanentities.SsoCa]) ISsoCaService {
	return &ssoCaService{repo: repo}
}

// ensure loads the CA from storage, or creates and persists a new one on first use.
func (s *ssoCaService) ensure(ctx context.Context) (*fleetca.CA, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ca != nil {
		return s.ca, nil
	}
	row, _ := s.repo.GetByUnique(ctx, "", "ukey1", ssoCaName)
	if row != nil && row.CertPem != "" && row.KeyPem != "" {
		if ca, err := fleetca.LoadCA([]byte(row.CertPem), []byte(row.KeyPem)); err == nil {
			s.ca = ca
			return ca, nil
		}
		// fall through to regenerate a corrupt/incompatible stored CA
	}
	ca, err := fleetca.NewCA("myidsan-sso-ca", ssoCaValidFor)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if row != nil {
		row.CertPem = string(ca.CertPEM())
		row.KeyPem = string(ca.KeyPEM())
		row.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			return nil, err
		}
	} else if _, err := s.repo.Create(ctx, "", myidsanentities.SsoCa{
		Name:      ssoCaName,
		CertPem:   string(ca.CertPEM()),
		KeyPem:    string(ca.KeyPEM()),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	s.ca = ca
	return ca, nil
}

func (s *ssoCaService) CARootPEM(ctx context.Context) ([]byte, error) {
	ca, err := s.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return ca.CertPEM(), nil
}

func (s *ssoCaService) IssueClient(ctx context.Context, clientId string) (clientCertPEM, clientKeyPEM, caCertPEM []byte, err error) {
	ca, err := s.ensure(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, key, err := ca.IssueLeaf(clientId, ssoClientCertTTL, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return cert, key, ca.CertPEM(), nil
}
