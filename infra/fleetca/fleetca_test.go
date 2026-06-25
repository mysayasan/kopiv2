package fleetca

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"
)

// handshakePair drives a TLS handshake over an in-memory pipe and returns both
// sides' results. Deadlines ensure a rejected handshake fails fast instead of
// blocking on the synchronous pipe when the aborting side can't flush its alert.
func handshakePair(clientCfg, serverCfg *tls.Config) (clientErr, serverErr error) {
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	deadline := time.Now().Add(1500 * time.Millisecond)
	c.SetDeadline(deadline)
	s.SetDeadline(deadline)
	client := tls.Client(c, clientCfg)
	server := tls.Server(s, serverCfg)
	cerr := make(chan error, 1)
	serr := make(chan error, 1)
	go func() { cerr <- client.Handshake() }()
	go func() { serr <- server.Handshake() }()
	return <-cerr, <-serr
}

func TestNewCALoadRoundTrip(t *testing.T) {
	ca, err := NewCA("fleet-ca", time.Hour)
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	if _, err := LoadCA(ca.CertPEM(), ca.KeyPEM()); err != nil {
		t.Fatalf("LoadCA: %v", err)
	}
}

func TestIssueFromCSRProducesTrustedLeaf(t *testing.T) {
	ca, _ := NewCA("fleet-ca", time.Hour)
	_, csrPEM, err := GenerateKeyAndCSR("node-1")
	if err != nil {
		t.Fatalf("GenerateKeyAndCSR: %v", err)
	}
	certPEM, err := ca.IssueFromCSR(csrPEM, "node-1", time.Hour, nil)
	if err != nil {
		t.Fatalf("IssueFromCSR: %v", err)
	}
	// The leaf must chain to the CA, and the CN must be the authoritative one.
	block, _ := pem.Decode(certPEM)
	leaf, _ := x509.ParseCertificate(block.Bytes)
	if leaf.Subject.CommonName != "node-1" {
		t.Fatalf("leaf CN = %q want node-1", leaf.Subject.CommonName)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.CertPEM())
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		t.Fatalf("leaf does not chain to CA: %v", err)
	}
}

func TestIssueOverridesCSRCommonName(t *testing.T) {
	ca, _ := NewCA("fleet-ca", time.Hour)
	// CSR claims to be "attacker" but the CA stamps the authoritative CN.
	_, csrPEM, _ := GenerateKeyAndCSR("attacker")
	certPEM, _ := ca.IssueFromCSR(csrPEM, "node-real", time.Hour, nil)
	block, _ := pem.Decode(certPEM)
	leaf, _ := x509.ParseCertificate(block.Bytes)
	if leaf.Subject.CommonName != "node-real" {
		t.Fatalf("CA must override CSR CN, got %q", leaf.Subject.CommonName)
	}
}

func TestMutualTLSHandshake(t *testing.T) {
	ca, _ := NewCA("fleet-ca", time.Hour)
	// Node server cert (CN=node-1) via CSR; parent client cert (CN=parent) directly.
	_, nodeCSR, _ := GenerateKeyAndCSR("node-1")
	nodeCert, _ := ca.IssueFromCSR(nodeCSR, "node-1", time.Hour, nil)
	// We need the node's key too — regenerate via IssueLeaf path for the server side.
	nodeCertFull, nodeKey, _ := ca.IssueLeaf("node-1", time.Hour, nil)
	_ = nodeCert
	parentCert, parentKey, _ := ca.IssueLeaf("parent", time.Hour, nil)

	serverCfg, err := ServerTLSConfig(nodeCertFull, nodeKey, ca.CertPEM())
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	clientCfg, err := ClientTLSConfig(parentCert, parentKey, ca.CertPEM(), "node-1")
	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}

	cerr, serr := handshakePair(clientCfg, serverCfg)
	if cerr != nil || serr != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cerr, serr)
	}
}

func TestClientRejectsWrongServerCN(t *testing.T) {
	ca, _ := NewCA("fleet-ca", time.Hour)
	nodeCert, nodeKey, _ := ca.IssueLeaf("node-1", time.Hour, nil)
	parentCert, parentKey, _ := ca.IssueLeaf("parent", time.Hour, nil)
	serverCfg, _ := ServerTLSConfig(nodeCert, nodeKey, ca.CertPEM())
	// Client expects node-2 but server is node-1 → must fail.
	clientCfg, _ := ClientTLSConfig(parentCert, parentKey, ca.CertPEM(), "node-2")

	cerr, _ := handshakePair(clientCfg, serverCfg)
	if cerr == nil {
		t.Fatal("client should reject a server with the wrong CN")
	}
}

func TestServerRejectsUntrustedClient(t *testing.T) {
	ca, _ := NewCA("fleet-ca", time.Hour)
	other, _ := NewCA("other-ca", time.Hour)
	nodeCert, nodeKey, _ := ca.IssueLeaf("node-1", time.Hour, nil)
	// Client cert signed by a DIFFERENT CA → server must reject.
	badCert, badKey, _ := other.IssueLeaf("parent", time.Hour, nil)
	serverCfg, _ := ServerTLSConfig(nodeCert, nodeKey, ca.CertPEM())
	clientCfg, _ := ClientTLSConfig(badCert, badKey, ca.CertPEM(), "node-1")

	_, serr := handshakePair(clientCfg, serverCfg)
	if serr == nil {
		t.Fatal("server should reject a client cert from an unknown CA")
	}
}
