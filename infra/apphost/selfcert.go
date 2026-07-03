package apphost

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ensureSelfSignedCert makes sure a usable TLS keypair exists at certPath/keyPath.
// If both already exist it is a no-op (operators can drop in their own cert, or
// front the app with a TLS-terminating reverse proxy). If either is missing it
// generates a long-lived self-signed certificate covering localhost, the
// configured hostnames, and every local IP address, so a freshly installed
// appliance serves HTTPS on first boot without manual cert wrangling. Browsers
// will show a one-time "not trusted" warning for a self-signed cert on a LAN —
// that is expected; supply a real cert or a reverse proxy for a trusted chain.
func ensureSelfSignedCert(certPath, keyPath string, hostnames []string) error {
	if certPath == "" || keyPath == "" {
		return nil // handled (errored) by runServer as before
	}
	if fileExists(certPath) && fileExists(keyPath) {
		return nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("self-signed cert: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("self-signed cert: serial: %w", err)
	}

	dnsNames, ipAddrs := certSANs(hostnames)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "mymatasan", Organization: []string{"MyMataSan"}},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("self-signed cert: create: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("self-signed cert: marshal key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return fmt.Errorf("self-signed cert: mkdir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
		return fmt.Errorf("self-signed cert: mkdir: %w", err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return fmt.Errorf("self-signed cert: write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return fmt.Errorf("self-signed cert: write key: %w", err)
	}

	log.Printf("generated self-signed TLS certificate (cert=%s key=%s dns=%v ips=%d) — browsers will warn until you install a trusted cert or front the app with a reverse proxy", certPath, keyPath, dnsNames, len(ipAddrs))
	return nil
}

// certSANs builds the DNS and IP SAN lists: localhost plus any concrete hostnames
// from config, and every usable local IP so the cert validates when the appliance
// is reached by its LAN address.
func certSANs(hostnames []string) ([]string, []net.IP) {
	dnsSet := map[string]struct{}{"localhost": {}}
	ipSet := map[string]net.IP{"127.0.0.1": net.ParseIP("127.0.0.1"), "::1": net.ParseIP("::1")}

	for _, h := range hostnames {
		h = strings.TrimSpace(h)
		if h == "" || h == "*" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			ipSet[ip.String()] = ip
		} else {
			dnsSet[h] = struct{}{}
		}
	}

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				ipSet[ipNet.IP.String()] = ipNet.IP
			}
		}
	}

	dnsNames := make([]string, 0, len(dnsSet))
	for d := range dnsSet {
		dnsNames = append(dnsNames, d)
	}
	ips := make([]net.IP, 0, len(ipSet))
	for _, ip := range ipSet {
		ips = append(ips, ip)
	}
	return dnsNames, ips
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
