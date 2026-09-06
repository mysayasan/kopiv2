package apis

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/onvif"
)

// stubCameraService accepts a credential change and remembers what it was given.
type stubCameraService struct {
	services.ICameraService
	saved onvif.Credentials
}

func (s *stubCameraService) SaveCredentials(_ context.Context, _ uint64, creds onvif.Credentials) (*services.CameraDetail, error) {
	s.saved = creds
	return &services.CameraDetail{Username: creds.Username}, nil
}

// TestChangingCameraCredentialsIsAudited covers a wiring slip rather than a missing call:
// the handler always asked the Auditor to record the change, but NewCameraApi dropped the
// audit argument on the floor when it built the handler, so every camera audit call landed
// on a nil Auditor and returned silently. Nothing failed, nothing logged, and the trail
// simply had no record of who changed a camera's login — the one question a credential
// change exists to answer. Constructing the API the way the app does is the only way to
// catch that: a test that pokes a hand-built cameraApi{audit: ...} would pass either way.
func TestChangingCameraCredentialsIsAudited(t *testing.T) {
	trail := &recordingAuditTrail{}
	serv := &stubCameraService{}
	router := mux.NewRouter()
	NewCameraApi(router, serv, nil, nil, nil, NewAuditor(trail, nil))

	r := signedInRequest("POST", "/cameras/5/credentials")
	r.Body = io.NopCloser(strings.NewReader(`{"username":"opsuser","password":"s3cret"}`))
	r.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("saving credentials returned %d: %s", w.Code, w.Body.String())
	}
	if serv.saved.Username != "opsuser" {
		t.Fatalf("service did not receive the credentials: %+v", serv.saved)
	}

	e := trail.find(services.ActionCameraCredentialChange)
	if e == nil {
		t.Fatalf("changing a camera's credentials recorded nothing; entries=%v", trail.entries)
	}
	if e.ActorId != 7 || e.ActorEmail != "sam.admin" {
		t.Errorf("actor not attributed: id=%d label=%q", e.ActorId, e.ActorEmail)
	}
	if e.TargetId != "5" {
		t.Errorf("the trail does not name the camera: %q", e.TargetId)
	}
	// The username is the point of the record; the password must never reach the trail.
	if got, _ := e.Metadata["username"].(string); got != "opsuser" {
		t.Errorf("the trail does not say which login was set: metadata=%v", e.Metadata)
	}
	if strings.Contains(e.Detail, "s3cret") || strings.Contains(strings.ToLower(entryText(e)), "s3cret") {
		t.Errorf("the camera password leaked into the audit trail: %q %v", e.Detail, e.Metadata)
	}
}

// entryText flattens an entry's free-text fields so a secret cannot hide in one of them.
func entryText(e *services.AuditEntry) string {
	out := e.Detail
	for k, v := range e.Metadata {
		out += " " + k + "="
		if s, ok := v.(string); ok {
			out += s
		}
	}
	return out
}
