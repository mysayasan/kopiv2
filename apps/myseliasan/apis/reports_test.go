package apis

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
)

type stubReportSvc struct{ data []byte }

func (s *stubReportSvc) FleetHealth(context.Context, time.Time, int) (*services.Report, error) {
	return &services.Report{Filename: "fleet-health-20260726.pdf", Data: s.data}, nil
}
func (s *stubReportSvc) Inventory(context.Context, time.Time, int64) (*services.Report, error) {
	return &services.Report{Filename: "inventory.pdf", Data: s.data}, nil
}
func (s *stubReportSvc) Security(context.Context, time.Time, int) (*services.Report, error) {
	return &services.Report{Filename: "security.pdf", Data: s.data}, nil
}
func (s *stubReportSvc) Incident(context.Context, time.Time, int, int64) (*services.Report, error) {
	return &services.Report{Filename: "incident.pdf", Data: s.data}, nil
}

type recordingAudit struct {
	// Embedded for the same reason as fakeDigestAudit: only Record and List are used
	// here, and an unexpected PurgeOlderThan call should be loud, not silent.
	services.IAuditService
	entries []services.AuditEntry
}

func (a *recordingAudit) Record(_ context.Context, e services.AuditEntry) { a.entries = append(a.entries, e) }
func (a *recordingAudit) List(context.Context, uint64, uint64, services.AuditFilter) ([]*entities.AuditLog, uint64, error) {
	return nil, 0, nil
}

// deliver is exercised directly (bypassing the auth/session middleware, which needs a
// full RBAC stack) to prove the PDF framing + audit write are correct.
func TestReportDeliverStreamsPDFAndAudits(t *testing.T) {
	audit := &recordingAudit{}
	h := &reportsApi{svc: &stubReportSvc{data: []byte("%PDF-1.4 test body")}, audit: audit}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reports/fleet-health.pdf?range=30", nil)
	h.fleetHealth(w, r)

	res := w.Result()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", ct)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "fleet-health-20260726.pdf") {
		t.Fatalf("Content-Disposition = %q, missing filename", cd)
	}
	if !strings.HasPrefix(w.Body.String(), "%PDF-") {
		t.Fatalf("body is not a PDF: %q", w.Body.String()[:8])
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != "report.fleet_health" {
		t.Fatalf("expected one audit entry action=report.fleet_health, got %+v", audit.entries)
	}
	if audit.entries[0].TargetType != "report" {
		t.Fatalf("audit TargetType = %q, want report", audit.entries[0].TargetType)
	}
}

// TestSecurityReportRequiresSuperadmin proves the superadmin gate denies when there is
// no superadmin session (session nil → denied), so the security report cannot leak.
func TestSecurityReportRequiresSuperadmin(t *testing.T) {
	h := &reportsApi{svc: &stubReportSvc{data: []byte("%PDF-1.4")}, audit: &recordingAudit{}, session: nil}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reports/security.pdf", nil)
	h.requireSuper(h.security)(w, r)
	if w.Result().StatusCode == 200 {
		t.Fatal("security report served without a superadmin session")
	}
}
