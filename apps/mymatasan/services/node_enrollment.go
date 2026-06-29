package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/fleetca"
)

// EnrollmentManager runs the node side of the mTLS hardening: after adoption it
// enrolls with the control-plane fleet CA (sending a CSR, storing the issued
// cert), serves the mutual-TLS management listener (heartbeat + release) while a
// cert is held, and renews the cert before it expires. It reconciles on a timer
// and on demand (Kick), reading all state live from the pairing service so adopt
// and unpair take effect without a restart.
type EnrollmentManager struct {
	svc         IPairingService
	mtlsPort    int
	renewBefore time.Duration
	httpClient  *http.Client
	logf        func(format string, args ...any)

	kick chan struct{}

	mu         sync.Mutex
	server     *http.Server
	servingPEM string // node cert currently bound to the listener, to detect renewals
}

// NewEnrollmentManager builds the manager. The HTTP client tolerates the parent's
// self-signed cert on the bootstrap enroll leg; the management listener it starts
// is full mTLS.
func NewEnrollmentManager(svc IPairingService, mtlsPort int, renewBefore time.Duration, logf func(string, ...any)) *EnrollmentManager {
	if mtlsPort <= 0 {
		mtlsPort = 49532
	}
	if renewBefore <= 0 {
		renewBefore = 48 * time.Hour
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &EnrollmentManager{
		svc:         svc,
		mtlsPort:    mtlsPort,
		renewBefore: renewBefore,
		logf:        logf,
		kick:        make(chan struct{}, 1),
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
	}
}

// Kick requests an immediate reconcile (e.g. right after adoption).
func (m *EnrollmentManager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Run reconciles on start, on Kick, and every few minutes until ctx is cancelled.
func (m *EnrollmentManager) Run(ctx context.Context) {
	m.reconcile(ctx)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stopListener()
			return
		case <-m.kick:
			m.reconcile(ctx)
		case <-ticker.C:
			m.reconcile(ctx)
		}
	}
}

func (m *EnrollmentManager) reconcile(ctx context.Context) {
	st, err := m.svc.Status(ctx)
	if err != nil || !st.Paired {
		m.stopListener() // unpaired → no mTLS channel
		return
	}
	enr, err := m.svc.Enrollment(ctx)
	if err != nil {
		return
	}
	if !enr.HasCert() || certNeedsRenewal(enr.CertExpiresAt, m.renewBefore) {
		if err := m.enroll(ctx); err != nil {
			m.logf("enrollment failed (will retry): %v", err)
			return
		}
		enr, _ = m.svc.Enrollment(ctx)
	}
	if enr.HasCert() {
		m.ensureListener(enr)
	} else {
		m.stopListener()
	}
}

// enroll generates a fresh key + CSR and exchanges it with the control plane for a
// signed certificate, which it stores via the pairing service.
func (m *EnrollmentManager) enroll(ctx context.Context) error {
	nodeID, err := m.svc.NodeID(ctx)
	if err != nil {
		return err
	}
	parentURL, err := m.svc.ParentBaseURL(ctx)
	if err != nil {
		return err
	}
	enr, err := m.svc.Enrollment(ctx)
	if err != nil {
		return err
	}
	parentURL = strings.TrimRight(strings.TrimSpace(parentURL), "/")
	if parentURL == "" || enr.Token == "" {
		return fmt.Errorf("enroll: missing parent URL or token")
	}
	keyPEM, csrPEM, err := fleetca.GenerateKeyAndCSR(nodeID)
	if err != nil {
		return err
	}
	reqBody, _ := json.Marshal(map[string]string{"nodeId": nodeID, "token": enr.Token, "csr": string(csrPEM)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parentURL+"/api/nodes/enroll", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("enroll: control plane unreachable: %w", err)
	}
	defer resp.Body.Close()
	type enrollResult struct {
		NodeCert string `json:"nodeCert"`
		CARoot   string `json:"caRoot"`
	}
	var env struct {
		Data struct {
			Result enrollResult `json:"result"`
		} `json:"data"`
		Result  enrollResult `json:"result"`
		Message string       `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&env)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("enroll: rejected: %s", env.Message)
	}
	// The control plane replies with the standard envelope (top-level "result", via
	// SendResult); accept the legacy "data.result" nesting too so a response-shape
	// difference can't silently empty the cert. Mirrors the adopt-response parser.
	res := env.Result
	if res.NodeCert == "" {
		res = env.Data.Result
	}
	certPEM := res.NodeCert
	caRoot := res.CARoot
	if certPEM == "" || caRoot == "" {
		return fmt.Errorf("enroll: empty certificate in response")
	}
	expiresAt := certNotAfter([]byte(certPEM))
	if err := m.svc.SaveCert(ctx, certPEM, string(keyPEM), caRoot, expiresAt); err != nil {
		return err
	}
	m.logf("enrolled with control plane; cert valid until %s", time.Unix(expiresAt, 0).Format(time.RFC3339))
	return nil
}

// ensureListener starts the mTLS management listener, or restarts it if the cert
// changed (renewal).
func (m *EnrollmentManager) ensureListener(enr Enrollment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil && m.servingPEM == enr.NodeCertPEM {
		return // already serving the current cert
	}
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
	tlsCfg, err := fleetca.ServerTLSConfig([]byte(enr.NodeCertPEM), []byte(enr.NodeKeyPEM), []byte(enr.CARootPEM))
	if err != nil {
		m.logf("mTLS listener config error: %v", err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", m.handleHeartbeat)
	mux.HandleFunc("/release", m.handleRelease)
	srv := &http.Server{
		Addr:      fmt.Sprintf(":%d", m.mtlsPort),
		Handler:   mux,
		TLSConfig: tlsCfg,
	}
	m.server = srv
	m.servingPEM = enr.NodeCertPEM
	go func() {
		// Cert/key already in TLSConfig.Certificates, so empty strings here are fine.
		if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			m.logf("mTLS listener stopped: %v", err)
		}
	}()
	m.logf("mTLS management listener started on :%d", m.mtlsPort)
}

func (m *EnrollmentManager) stopListener() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
		m.servingPEM = ""
	}
}

func (m *EnrollmentManager) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	// TLS layer already verified the caller's client cert chains to our fleet CA.
	nodeID, _ := m.svc.NodeID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"paired": true, "nodeId": nodeID})
}

func (m *EnrollmentManager) handleRelease(w http.ResponseWriter, r *http.Request) {
	// mTLS authenticated the parent; release using our own stored token, then
	// reconcile (which tears down this listener now that we're unpaired).
	enr, err := m.svc.Enrollment(r.Context())
	if err == nil {
		_ = m.svc.Release(r.Context(), enr.Token)
	}
	w.WriteHeader(http.StatusOK)
	go m.Kick()
}

// certNeedsRenewal reports whether a cert expiring at unix `expiresAt` is within
// `before` of now (or already gone / unknown).
func certNeedsRenewal(expiresAt int64, before time.Duration) bool {
	if expiresAt <= 0 {
		return true
	}
	return time.Now().Add(before).After(time.Unix(expiresAt, 0))
}

func certNotAfter(certPEM []byte) int64 {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return 0
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0
	}
	return leaf.NotAfter.Unix()
}
