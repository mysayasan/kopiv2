package services

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/sqlite"
	infranotif "github.com/mysayasan/kopiv2/infra/notification"
)

// captureChannel records everything published, so a test can assert on exactly what would leave
// the building over a webhook, the SSE stream, or the fleet control channel.
type captureChannel struct {
	got []infranotif.Notification
}

func (c *captureChannel) Name() string { return "capture" }
func (c *captureChannel) Send(_ context.Context, n infranotif.Notification) error {
	c.got = append(c.got, n)
	return nil
}

// newCaptureAlarmer stands up a real notification service over a throwaway SQLite database with a
// capture channel registered, mirroring how the app wires NotificationAlarmer in production.
func newCaptureAlarmer(t *testing.T) (*NotificationAlarmer, *captureChannel) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "alarm-test.db")

	cfg := dbsql.DbConfigModel{Engine: "sqlite", DbName: path}
	status, err := bootstrap.Ensure(ctx, bootstrap.Options{
		AppName: "mypintusan-alarm-test",
		Config:  cfg,
		Bootstrap: bootstrap.BootstrapConfig{
			Enabled: true, AutoCreateDatabase: true, AutoCreateSchema: true, AutoMigrate: true,
		},
		Entities: []any{sharedentities.Notification{}},
	})
	if err != nil {
		t.Fatalf("bootstrap.Ensure: %v", err)
	}
	if !status.Ready {
		t.Fatalf("bootstrap not ready: %+v", status)
	}

	db, err := sqlite.NewDbCrud(cfg)
	if err != nil {
		t.Fatalf("sqlite.NewDbCrud: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := db.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	svc := notification.NewService(dbsql.NewGenericRepo[sharedentities.Notification](db), notification.Options{})
	capture := &captureChannel{}
	svc.Register(capture)
	return NewNotificationAlarmer(svc, nil), capture
}

func TestDecisionPublishesGrant(t *testing.T) {
	alarmer, capture := newCaptureAlarmer(t)

	alarmer.Decision(context.Background(), entities.AccessEvent{
		Id: 41, DoorId: 3, ReaderId: 2, HolderId: 7, HolderName: "J. Smith",
		RawCredential: "83826900", Decision: entities.DecisionGranted, Reason: entities.ReasonOK,
	})

	if len(capture.got) != 1 {
		t.Fatalf("published %d notifications, want 1", len(capture.got))
	}
	n := capture.got[0]
	if n.Category != "access.granted" {
		t.Fatalf("category = %q, want access.granted", n.Category)
	}
	if n.Severity != infranotif.Info {
		t.Fatalf("severity = %q, want info", n.Severity)
	}
	if n.Title != "Badge accepted: J. Smith" {
		t.Fatalf("title = %q", n.Title)
	}
	if n.Data["reason"] != entities.ReasonOK || n.Data["decision"] != entities.DecisionGranted {
		t.Fatalf("data = %+v", n.Data)
	}
}

func TestDecisionPublishesDenialWithCoarseReason(t *testing.T) {
	alarmer, capture := newCaptureAlarmer(t)

	// A bad PIN must not be distinguishable in the feed from any other credential rejection:
	// whether a door uses PINs at all is information the feed has no business carrying.
	alarmer.Decision(context.Background(), entities.AccessEvent{
		Id: 42, DoorId: 3, HolderName: "J. Smith", RawCredential: "83826900",
		Decision: entities.DecisionDenied, Reason: entities.ReasonBadPin,
		Detail: "incorrect PIN",
	})

	if len(capture.got) != 1 {
		t.Fatalf("published %d notifications, want 1", len(capture.got))
	}
	n := capture.got[0]
	if n.Category != "access.denied" {
		t.Fatalf("category = %q, want access.denied", n.Category)
	}
	if n.Title != "Badge denied: J. Smith" {
		t.Fatalf("title = %q", n.Title)
	}
	if n.Data["reason"] != "credential-rejected" {
		t.Fatalf("reason = %v, want credential-rejected", n.Data["reason"])
	}
	if n.Body == "Denied: incorrect PIN" || n.Body == "Denied: bad pin" {
		t.Fatalf("body leaks PIN phrasing: %q", n.Body)
	}
}

// TestDuressGrantIsInvisibleInTheDecisionFeed is the property that matters most in this file: a
// duress grant's decision notification must be BYTE-IDENTICAL to a normal grant's. Anything with
// feed access — a dashboard on a wall, a webhook, the fleet control plane's UI — could otherwise
// reveal to a coercer that an alarm was raised. The separate Critical duress alarm (Raise) is the
// operator's signal; this stream must say nothing.
func TestDuressGrantIsInvisibleInTheDecisionFeed(t *testing.T) {
	base := entities.AccessEvent{
		Id: 50, DoorId: 3, ReaderId: 2, HolderId: 7, HolderName: "J. Smith",
		RawCredential: "83826900", Decision: entities.DecisionGranted,
	}

	normal := base
	normal.Reason = entities.ReasonOK

	duress := base
	duress.Reason = entities.ReasonDuress
	duress.Duress = true
	duress.Detail = "access granted under a duress PIN"

	alarmerA, captureA := newCaptureAlarmer(t)
	alarmerA.Decision(context.Background(), normal)
	alarmerB, captureB := newCaptureAlarmer(t)
	alarmerB.Decision(context.Background(), duress)

	if len(captureA.got) != 1 || len(captureB.got) != 1 {
		t.Fatalf("published %d/%d notifications, want 1/1", len(captureA.got), len(captureB.got))
	}
	a, b := captureA.got[0], captureB.got[0]
	// The hub stamps a random ID and a timestamp; blank them so the comparison is over content.
	a.ID, b.ID = "", ""
	a.CreatedAt, b.CreatedAt = 0, 0
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("a duress grant is distinguishable in the decision feed:\nnormal: %+v\nduress: %+v", a, b)
	}
}

func TestDecisionOperatorUnlockTitles(t *testing.T) {
	alarmer, capture := newCaptureAlarmer(t)

	alarmer.Decision(context.Background(), entities.AccessEvent{
		Id: 60, DoorId: 3, HolderId: 1, HolderName: "admin",
		RawCredential: "operator", Decision: entities.DecisionGranted, Reason: entities.ReasonOK,
	})
	alarmer.Decision(context.Background(), entities.AccessEvent{
		Id: 61, DoorId: 3, HolderId: 1, HolderName: "admin",
		RawCredential: "operator", Decision: entities.DecisionDenied, Reason: entities.ReasonLockdown,
	})

	if len(capture.got) != 2 {
		t.Fatalf("published %d notifications, want 2", len(capture.got))
	}
	if capture.got[0].Title != "Door unlocked by admin" {
		t.Fatalf("granted title = %q", capture.got[0].Title)
	}
	if capture.got[1].Title != "Remote unlock refused" {
		t.Fatalf("denied title = %q", capture.got[1].Title)
	}
}

// TestNilAlarmerDecisionIsSafe: the controller calls Decisions through a nil-safe config hook, but
// the alarmer itself must also tolerate a nil receiver the way Raise does.
func TestNilAlarmerDecisionIsSafe(t *testing.T) {
	var a *NotificationAlarmer
	a.Decision(context.Background(), entities.AccessEvent{Decision: entities.DecisionGranted})
}
