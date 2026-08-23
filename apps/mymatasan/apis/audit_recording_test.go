package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// recordingAuditTrail captures what the handlers record.
type recordingAuditTrail struct {
	services.IAuditService
	entries []services.AuditEntry
}

func (t *recordingAuditTrail) Record(_ context.Context, e services.AuditEntry) {
	t.entries = append(t.entries, e)
}

func (t *recordingAuditTrail) find(action string) *services.AuditEntry {
	for i := range t.entries {
		if t.entries[i].Action == action {
			return &t.entries[i]
		}
	}
	return nil
}

// stubRecordingService serves one segment and counts deletions.
type stubRecordingService struct {
	services.IRecordingService
	seg     *entities.RecordingSegment
	deleted []uint64
}

func (s *stubRecordingService) GetSegmentById(_ context.Context, id uint64) (*entities.RecordingSegment, error) {
	if s.seg != nil && uint64(s.seg.Id) == id {
		return s.seg, nil
	}
	return nil, nil
}

func (s *stubRecordingService) DeleteSegment(_ context.Context, id uint64) error {
	s.deleted = append(s.deleted, id)
	return nil
}

// signedInRequest builds a request carrying an authenticated principal, the way the auth
// middleware would have left it.
func signedInRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	ctx := sharedapis.WithLocalUser(r.Context(), &sharedservices.AuthenticatedUser{
		Id: 7, Username: "sam.admin", RoleId: 2, IsAdmin: true,
	})
	r.RemoteAddr = "203.0.113.9:51000"
	r.Header.Set("User-Agent", "test-agent/1.0")
	return r.WithContext(ctx)
}

func newAuditedRecordingApi(seg *entities.RecordingSegment) (*mux.Router, *stubRecordingService, *recordingAuditTrail) {
	trail := &recordingAuditTrail{}
	serv := &stubRecordingService{seg: seg}
	router := mux.NewRouter()
	// No trusted proxies: an untrusted caller must not be able to forge the recorded
	// address with X-Forwarded-For.
	NewRecordingApi(router, serv, nil, nil, nil, nil, nil, nil, NewAuditor(trail, nil), nil)
	return router, serv, trail
}

func testSegment() *entities.RecordingSegment {
	return &entities.RecordingSegment{
		Id: 412, CameraId: 3, FilePath: "/recordings/cam3/20260819_140500.mp4",
		StartedAt: 1755612300, EndedAt: 1755612600,
	}
}

// TestDeletingFootageIsAudited is the finding this whole work item exists for. Before it,
// DELETE /api/recording/segments/{id} destroyed evidence and recorded nothing at all: no
// actor, no reason, no trace. RBAC keeps an operator away from this route, so the actor is
// an administrator — which is exactly the case an investigation cannot otherwise answer.
func TestDeletingFootageIsAudited(t *testing.T) {
	router, serv, trail := newAuditedRecordingApi(testSegment())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, signedInRequest("DELETE", "/recording/segments/412"))

	if w.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", w.Code, w.Body.String())
	}
	if len(serv.deleted) != 1 {
		t.Fatalf("expected the segment to be deleted, got %v", serv.deleted)
	}

	e := trail.find(services.ActionRecordingDelete)
	if e == nil {
		t.Fatalf("deleting footage recorded nothing; entries=%v", trail.entries)
	}
	if e.ActorId != 7 || e.ActorEmail != "sam.admin" {
		t.Errorf("actor not attributed: id=%d label=%q", e.ActorId, e.ActorEmail)
	}
	if e.ActorRole != 2 {
		t.Errorf("actor role not captured: %d", e.ActorRole)
	}
	if e.TargetType != services.TargetRecording || e.TargetId != "412" {
		t.Errorf("target wrong: %s/%s", e.TargetType, e.TargetId)
	}
	if e.Outcome != services.OutcomeSuccess {
		t.Errorf("outcome = %q", e.Outcome)
	}
	if e.ClientIp != "203.0.113.9" {
		t.Errorf("client ip = %q, want the peer address", e.ClientIp)
	}
	if e.UserAgent != "test-agent/1.0" {
		t.Errorf("user agent = %q", e.UserAgent)
	}
	// The camera and time range must be captured BEFORE the row is deleted. Afterwards
	// "recording 412 was deleted" is not an answer to "what footage did we lose".
	if e.Metadata["cameraId"] != int64(3) {
		t.Errorf("cameraId not captured before delete: %v", e.Metadata["cameraId"])
	}
	if e.Metadata["startedAt"] != int64(1755612300) || e.Metadata["endedAt"] != int64(1755612600) {
		t.Errorf("time range not captured before delete: %v", e.Metadata)
	}
}

// An untrusted caller must not be able to choose what address is recorded against their
// own deletion. With no trusted proxies configured, X-Forwarded-For is ignored entirely.
func TestAuditedClientIpCannotBeForged(t *testing.T) {
	router, _, trail := newAuditedRecordingApi(testSegment())

	r := signedInRequest("DELETE", "/recording/segments/412")
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	router.ServeHTTP(httptest.NewRecorder(), r)

	e := trail.find(services.ActionRecordingDelete)
	if e == nil {
		t.Fatal("no audit entry")
	}
	if e.ClientIp != "203.0.113.9" {
		t.Fatalf("client ip = %q — a forged X-Forwarded-For was trusted", e.ClientIp)
	}
}

// Watching footage is as auditable as deleting it: "who viewed this" is the question a
// GDPR Article 30 record and a CCTV tender both ask, and it cannot be reconstructed later.
func TestViewingFootageIsAudited(t *testing.T) {
	router, _, trail := newAuditedRecordingApi(testSegment())

	// An unranged GET is the opening request of a playback (and a plain download).
	router.ServeHTTP(httptest.NewRecorder(), signedInRequest("GET", "/recording/segments/412/download"))

	e := trail.find(services.ActionRecordingDownload)
	if e == nil {
		t.Fatalf("viewing footage recorded nothing; entries=%v", trail.entries)
	}
	if e.ActorEmail != "sam.admin" || e.TargetId != "412" {
		t.Errorf("entry wrong: actor=%q target=%q", e.ActorEmail, e.TargetId)
	}
	if !strings.Contains(e.Detail, "camera 3") {
		t.Errorf("detail should name the camera and window, got %q", e.Detail)
	}
}

// A scrubbing <video> element issues many ranged requests for one clip. Recording each as
// a separate viewing would bury the trail it is meant to provide, so only the opening
// range of a playback is recorded — every playback starts unranged or at bytes=0-.
func TestMidPlaybackSeeksAreNotEachAudited(t *testing.T) {
	router, _, trail := newAuditedRecordingApi(testSegment())

	r := signedInRequest("GET", "/recording/segments/412/download")
	r.Header.Set("Range", "bytes=1048576-2097151")
	router.ServeHTTP(httptest.NewRecorder(), r)

	if len(trail.entries) != 0 {
		t.Fatalf("a mid-clip seek should not write its own entry, got %v", trail.entries)
	}
}

// A nil Auditor must not panic. Auditing can never be allowed to fail the action being
// audited, and that has to hold for a mis-wired composition root too.
func TestNilAuditorDoesNotPanic(t *testing.T) {
	var a *Auditor
	a.Success(signedInRequest("DELETE", "/x"), "recording.delete", "recording", "1", "detail", nil)
}

// The shared trail must not be reachable for editing. This is a compile-time guard: if a
// mutating method is ever added to the interface, this stops naming the surface honestly
// and the review that adds it has to confront the change.
func TestAuditInterfaceExposesNoEditPath(t *testing.T) {
	var svc services.IAuditService = sharedaudit.NewService(nil, nil)
	if _, ok := svc.(interface {
		Update(context.Context, sharedaudit.AuditLog) error
	}); ok {
		t.Fatal("the audit trail must expose no update path")
	}
	if _, ok := svc.(interface{ DeleteById(context.Context, int64) error }); ok {
		t.Fatal("the audit trail must expose no targeted delete")
	}
}
