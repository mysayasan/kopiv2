// Package fleetca is a tiny on-prem certificate authority used by the myseliasan
// control plane to issue per-node and control-plane client certificates for the
// pairing mTLS channel. It deliberately avoids CRL/OCSP: certificates are
// short-lived and renewed over the existing mTLS channel, and revocation is
// enforced by refusing to renew a revoked serial (see the control-plane service).
//
// Identity lives in the certificate Common Name (the node id / parent id), not in
// DNS/IP SANs, so the verifiers below check the CA chain and the CN rather than the
// hostname — appliances are dialed by IP and a SAN-based check would be brittle.
package fleetca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// CA is a loaded certificate authority capable of signing leaf certificates.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

// NewCA creates a fresh self-signed CA valid for validFor.
func NewCA(commonName string, validFor time.Duration) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(validFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM, err := marshalKey(key)
	if err != nil {
		return nil, err
	}
	return LoadCA(certPEM, keyPEM)
}

// LoadCA reconstructs a CA from previously stored PEM.
func LoadCA(certPEM, keyPEM []byte) (*CA, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("fleetca: load keypair: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("fleetca: CA key is not ECDSA")
	}
	return &CA{cert: leaf, key: key, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// CertPEM returns the CA's public certificate (safe to distribute to nodes).
func (c *CA) CertPEM() []byte { return c.certPEM }

// KeyPEM returns the CA's private key PEM (keep on the control plane only).
func (c *CA) KeyPEM() []byte { return c.keyPEM }

// IssueFromCSR signs a leaf certificate for cn using the public key in csrPEM. The
// CSR's own subject is ignored; the CN is set authoritatively here so a node can't
// pick its own identity. serial must be unique per issuance.
func (c *CA) IssueFromCSR(csrPEM []byte, cn string, validFor time.Duration, serial *big.Int) ([]byte, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, errors.New("fleetca: invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("fleetca: CSR signature: %w", err)
	}
	return c.signLeaf(cn, csr.PublicKey, validFor, serial)
}

// IssueLeaf generates a fresh key and signs a leaf for cn in one step. Used for the
// control plane's own client certificate. Returns cert PEM and key PEM.
func (c *CA) IssueLeaf(cn string, validFor time.Duration, serial *big.Int) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	certPEM, err = c.signLeaf(cn, &key.PublicKey, validFor, serial)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err = marshalKey(key)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

func (c *CA) signLeaf(cn string, pub any, validFor time.Duration, serial *big.Int) ([]byte, error) {
	if serial == nil {
		var err error
		if serial, err = randSerial(); err != nil {
			return nil, err
		}
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(validFor),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, pub, c.key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// GenerateKeyAndCSR creates a new private key and a CSR for cn. The node calls this
// so its private key never leaves the node — only the CSR is sent to the CA.
func GenerateKeyAndCSR(cn string) (keyPEM, csrPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err = marshalKey(key)
	if err != nil {
		return nil, nil, err
	}
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	return keyPEM, csrPEM, nil
}

// ServerTLSConfig builds a tls.Config for the node's mTLS management listener: it
// presents certPEM/keyPEM and requires a client certificate signed by caPEM.
func ServerTLSConfig(certPEM, keyPEM, caPEM []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("fleetca: bad CA PEM for client pool")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// ClientTLSConfig builds a tls.Config for the control plane dialing a node: it
// presents certPEM/keyPEM and verifies the node's server certificate against caPEM,
// checking the CN equals expectServerCN (identity, not hostname). Hostname/SAN
// verification is skipped on purpose; the manual verifier below enforces trust.
func ClientTLSConfig(certPEM, keyPEM, caPEM []byte, expectServerCN string) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("fleetca: bad CA PEM for root pool")
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		InsecureSkipVerify: true, // hostname check disabled; VerifyPeerCertificate enforces CA + CN
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyChainCN(rawCerts, pool, expectServerCN)
		},
	}, nil
}

// verifyChainCN checks the peer's leaf chains to the pool and (optionally) matches
// expectCN.
func verifyChainCN(rawCerts [][]byte, pool *x509.CertPool, expectCN string) error {
	if len(rawCerts) == 0 {
		return errors.New("fleetca: no peer certificate")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return err
	}
	inter := x509.NewCertPool()
	for _, raw := range rawCerts[1:] {
		if c, err := x509.ParseCertificate(raw); err == nil {
			inter.AddCert(c)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return fmt.Errorf("fleetca: peer not trusted: %w", err)
	}
	if expectCN != "" && leaf.Subject.CommonName != expectCN {
		return fmt.Errorf("fleetca: peer CN %q != expected %q", leaf.Subject.CommonName, expectCN)
	}
	return nil
}

// PeerCommonName returns the CN of the verified client certificate on a request's
// TLS state, or "" when none. Node handlers use it to confirm the caller identity.
func PeerCommonName(state *tls.ConnectionState) string {
	if state == nil || len(state.PeerCertificates) == 0 {
		return ""
	}
	return state.PeerCertificates[0].Subject.CommonName
}

func randSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

func marshalKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
