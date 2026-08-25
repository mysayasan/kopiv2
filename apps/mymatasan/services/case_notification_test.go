package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	shared "github.com/mysayasan/kopiv2/domain/entities"
)

// fakeFeed is the notification feed and the alert log, as this service sees them.
type fakeFeed struct {
	notifs map[int64]*shared.Notification
	alerts map[int64]*entities.AlertEvent
}

func (f *fakeFeed) notification(_ context.Context, id int64) (*shared.Notification, error) {
	if row, ok := f.notifs[id]; ok {
		return row, nil
	}
	return nil, errors.New("no such notification")
}

func (f *fakeFeed) alert(_ context.Context, id int64) (*entities.AlertEvent, error) {
	if row, ok := f.alerts[id]; ok {
		return row, nil
	}
	return nil, errors.New("no such alert")
}

func (f *caseFixture) withFeed(feed *fakeFeed) *caseFixture {
	f.svc.feed = feed
	return f
}

// THE RESOLUTION, and the reason this is not just a new Kind on the existing endpoint.
//
// Most of what an operator notices in the feed is a notification ABOUT something else. Storing
// it as a "notification" item would put the same evidence in the case under two different
// kinds depending on which screen it was added from, with two different spans, and the export
// would carry it twice.
func TestAFeedEntryAboutAnAlertBecomesTheALERT(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{
		notifs: map[int64]*shared.Notification{
			1: {Id: 1, Title: "Person in the loading bay", CameraId: 3,
				RefType: "alert_event", RefId: 88, CreatedAt: 1_700_000_500},
		},
		alerts: map[int64]*entities.AlertEvent{
			88: {Id: 88, CameraId: 3, Label: "person", Confidence: 0.91,
				SnapshotPath: "recordings/cam3/snapshots/88.jpg", CreatedAt: 1_700_000_490},
		},
	})
	c := f.openCase(t, "Loading bay")

	item, err := f.svc.AddNotification(context.Background(), c.Id, 1, "same jacket as item 2",
		CaseActor{Id: 7, Name: "sam"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if item.Kind != entities.CaseItemAlert {
		t.Fatalf("kind = %q, want %q — a feed entry pointing at an alert IS the alert, and "+
			"storing it twice under two kinds is how one piece of evidence ends up in an "+
			"export bundle twice", item.Kind, entities.CaseItemAlert)
	}
	if item.SourceId != 88 {
		t.Fatalf("sourceId = %d, want the ALERT's id (88) so the provenance points at the "+
			"evidence rather than at the row that mentioned it", item.SourceId)
	}
	if item.SnapshotPath == "" {
		t.Fatal("the alert's snapshot was dropped — a case item with no picture is a line of text")
	}
	// The ALERT's time, not the feed row's. They are usually within a second of each other
	// and occasionally are not: a relayed or replayed notification carries the time it was
	// ingested here, and the evidence is the alert.
	if item.StartedAt != 1_700_000_490-notificationPadSeconds ||
		item.EndedAt != 1_700_000_490+notificationPadSeconds {
		t.Fatalf("span %d..%d is not centred on the alert's own time", item.StartedAt, item.EndedAt)
	}
	if !item.HoldsFootage() {
		t.Fatal("an alert on a camera must hold its footage past retention")
	}
	if item.Note != "same jacket as item 2" {
		t.Fatalf("the operator's note did not travel with it: %q", item.Note)
	}
}

// A feed entry that names a camera but refers to nothing else — a camera going offline — is
// evidence in its own right, and the footage either side of it is the part that matters.
func TestAFeedEntryOnACameraKeepsTheFootageAroundIt(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{
		notifs: map[int64]*shared.Notification{
			2: {Id: 2, Title: "Camera offline", CameraId: 5, CreatedAt: 1_700_000_600},
		},
	})
	c := f.openCase(t, "Outage")

	item, err := f.svc.AddNotification(context.Background(), c.Id, 2, "", CaseActor{Id: 7})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if item.Kind != entities.CaseItemNotification {
		t.Fatalf("kind = %q, want %q", item.Kind, entities.CaseItemNotification)
	}
	if item.CameraId != 5 || !item.HoldsFootage() {
		t.Fatalf("a feed entry on a camera must hold that camera's footage: %+v", item)
	}
	if item.Label != "Camera offline" {
		t.Fatalf("label = %q, want the entry's title", item.Label)
	}
	if item.SourceId != 2 {
		t.Fatalf("sourceId = %d, want the feed row's id", item.SourceId)
	}
}

// "The recorder rebooted at 03:12" has no camera and no footage, and it is still the fact that
// explains the gap in the footage either side of it. Refusing it would send an operator to
// write the same sentence as a free-text note, losing the time and the provenance.
func TestAFeedEntryWithNoCameraIsStillEvidence(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{
		notifs: map[int64]*shared.Notification{
			3: {Id: 3, Title: "Recorder restarted", CreatedAt: 1_700_000_700},
		},
	})
	c := f.openCase(t, "Gap in the recording")

	item, err := f.svc.AddNotification(context.Background(), c.Id, 3, "", CaseActor{Id: 7})
	if err != nil {
		t.Fatalf("a system event with no camera was refused: %v", err)
	}
	if item.CameraId != 0 {
		t.Fatalf("cameraId = %d, want 0", item.CameraId)
	}
	if item.StartedAt != 1_700_000_700 {
		t.Fatalf("startedAt = %d, want the moment it happened", item.StartedAt)
	}
	// THE ONE THAT MATTERS HERE. An item with no camera must not pin any footage: a hold
	// query that matched on a stray span would quietly keep every camera's recordings alive
	// for as long as the case stayed open.
	if item.HoldsFootage() {
		t.Fatal("an item with no camera is holding footage")
	}
	if item.EndedAt != 0 {
		t.Fatalf("endedAt = %d, want 0 — a span with no camera is a hold nobody asked for",
			item.EndedAt)
	}
}

// Alerts are purged on their own retention schedule while the feed row survives. The button
// must keep working on old rows: the operator can still record WHAT they saw and WHEN, which
// is the part a case needs.
func TestAFeedEntryWhoseAlertHasBeenPurgedStillGoesIn(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{
		notifs: map[int64]*shared.Notification{
			4: {Id: 4, Title: "Person in the yard", CameraId: 2,
				RefType: "alert_event", RefId: 999, CreatedAt: 1_700_000_800},
		},
		alerts: map[int64]*entities.AlertEvent{}, // 999 is long gone
	})
	c := f.openCase(t, "Old incident")

	item, err := f.svc.AddNotification(context.Background(), c.Id, 4, "", CaseActor{Id: 7})
	if err != nil {
		t.Fatalf("a feed entry whose alert has been purged was refused: %v", err)
	}
	if item.Kind != entities.CaseItemNotification {
		t.Fatalf("kind = %q, want it to fall back to the feed entry itself", item.Kind)
	}
	if item.CameraId != 2 || !item.HoldsFootage() {
		t.Fatalf("the camera and its footage were lost with the alert row: %+v", item)
	}
}

func TestAddingAFeedEntryTwiceIsRefused(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{
		notifs: map[int64]*shared.Notification{
			5: {Id: 5, Title: "Camera offline", CameraId: 5, CreatedAt: 1_700_000_900},
		},
	})
	c := f.openCase(t, "Outage")
	if _, err := f.svc.AddNotification(context.Background(), c.Id, 5, "", CaseActor{Id: 7}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := f.svc.AddNotification(context.Background(), c.Id, 5, "", CaseActor{Id: 7}); err == nil {
		t.Fatal("the same feed entry went into the same case twice — a duplicate clip in an " +
			"export bundle is somebody else's job to explain")
	}
}

func TestAddingAFeedEntryThatDoesNotExistIsRefused(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{notifs: map[int64]*shared.Notification{}})
	c := f.openCase(t, "Nothing")
	if _, err := f.svc.AddNotification(context.Background(), c.Id, 404, "", CaseActor{Id: 7}); err == nil {
		t.Fatal("a notification that does not exist was accepted as evidence")
	}
}

// A closed case is closed. The existing rule, checked through the new door.
func TestAFeedEntryCannotBeAddedToAClosedCase(t *testing.T) {
	f := newCaseFixture(t).withFeed(&fakeFeed{
		notifs: map[int64]*shared.Notification{
			6: {Id: 6, Title: "Camera offline", CameraId: 5, CreatedAt: 1_700_001_000},
		},
	})
	c := f.openCase(t, "Shut")
	if _, err := f.svc.Close(context.Background(), c.Id, "resolved", CaseActor{Id: 7}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := f.svc.AddNotification(context.Background(), c.Id, 6, "", CaseActor{Id: 7}); err == nil {
		t.Fatal("evidence was added to a closed case through the notification door")
	}
}
