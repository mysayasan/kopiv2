package apphost

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSelfSignedCertGeneratesUsableKeypair(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "certs", "cert.pem")
	keyPath := filepath.Join(dir, "certs", "key.pem")

	if err := ensureSelfSignedCert(certPath, keyPath, []string{"nvr.local", "192.168.1.50", "*"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Files must exist and load as a valid keypair.
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("load generated keypair: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	// SANs: localhost + the concrete hostname/IP from config; "*" is dropped.
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("cert should be valid for localhost: %v", err)
	}
	if err := leaf.VerifyHostname("nvr.local"); err != nil {
		t.Errorf("cert should be valid for configured hostname: %v", err)
	}
	if err := leaf.VerifyHostname("192.168.1.50"); err != nil {
		t.Errorf("cert should be valid for configured IP: %v", err)
	}
}

func TestEnsureSelfSignedCertIsNoOpWhenBothExist(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, []byte("existing-cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("existing-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureSelfSignedCert(certPath, keyPath, nil); err != nil {
		t.Fatalf("no-op: %v", err)
	}
	// Operator-provided files must be left untouched.
	if b, _ := os.ReadFile(certPath); string(b) != "existing-cert" {
		t.Errorf("existing cert was overwritten: %q", b)
	}
}

func TestEnsureSelfSignedCertSkipsWhenPathsEmpty(t *testing.T) {
	if err := ensureSelfSignedCert("", "", nil); err != nil {
		t.Fatalf("empty paths should be a no-op: %v", err)
	}
}
