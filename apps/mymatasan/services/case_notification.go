package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	shared "github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Putting a feed entry into a case file (W3-3a's named follow-up).
//
// The Notifications screen is where an operator NOTICES something. Until now the only way to
// get what they noticed into a case was to read the time off the row, go to the timeline, find
// it again and bookmark it — which loses the provenance and takes long enough that people do
// not do it. This closes that.

// notificationPadSeconds is the footage kept either side of a feed entry that names a camera.
//
// The same padding the timeline's own bookmark uses, and for the same reason: an operator who
// marks a moment means the footage AROUND that moment. An instant cannot be exported and
// cannot be held.
const notificationPadSeconds = int64(30)

// caseNotificationSource is the small surface this needs from the feed and the alert log.
// Narrow on purpose: a case service holding the whole notification service could mark things
// read, publish, or purge, none of which putting evidence in a case has any business doing.
type caseNotificationSource interface {
	notification(ctx context.Context, id int64) (*shared.Notification, error)
	alert(ctx context.Context, id int64) (*entities.AlertEvent, error)
}

type repoNotificationSource struct {
	notifications dbsql.IGenericRepo[shared.Notification]
	alerts        dbsql.IGenericRepo[entities.AlertEvent]
}

func (r repoNotificationSource) notification(ctx context.Context, id int64) (*shared.Notification, error) {
	if r.notifications == nil {
		return nil, errors.New("the notification feed is not available")
	}
	row, err := r.notifications.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		// GetById ERRORS on a missing row rather than returning nil, so both shapes mean
		// the same thing here.
		return nil, errors.New("no such notification")
	}
	return row, nil
}

func (r repoNotificationSource) alert(ctx context.Context, id int64) (*entities.AlertEvent, error) {
	if r.alerts == nil {
		return nil, errors.New("the alert log is not available")
	}
	row, err := r.alerts.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, errors.New("no such alert")
	}
	return row, nil
}

// AddNotification puts one feed entry into a case, RESOLVED to what it actually refers to.
//
// THE RESOLUTION IS THE POINT. Most of what an operator notices in the feed is a notification
// ABOUT something else — an alert firing on a camera. Storing that as a "notification" item
// would put the same evidence in the case under two kinds depending on which screen it was
// added from, with different spans, and the export would carry it twice. So:
//
//	a feed entry pointing at an alert  ->  a CaseItemAlert, with the ALERT's own camera,
//	                                      time and snapshot, and the alert's id for
//	                                      provenance;
//	a feed entry naming a camera       ->  a CaseItemNotification with footage padded around
//	                                      the moment, which the case then holds;
//	anything else                      ->  a CaseItemNotification with no camera, which holds
//	                                      nothing and exports as text. "The recorder rebooted
//	                                      at 03:12" is not footage and is still evidence.
//
// The operator's own note travels with it either way, because the note is the half that turns
// a pile of rows into an argument.
func (s *caseService) AddNotification(ctx context.Context, caseId, notificationId int64, note string, actor CaseActor) (*entities.CaseItem, error) {
	if s.feed == nil {
		return nil, errors.New("this appliance cannot put feed entries into a case")
	}
	notif, err := s.feed.notification(ctx, notificationId)
	if err != nil {
		return nil, err
	}

	in := CaseItemInput{Note: strings.TrimSpace(note), Actor: actor}

	if strings.TrimSpace(notif.RefType) == notificationRefAlert && notif.RefId > 0 {
		alert, aerr := s.feed.alert(ctx, notif.RefId)
		if aerr != nil {
			// The alert is gone — retention deletes alerts on their own schedule while the
			// feed row survives. Fall through to the feed entry itself rather than refuse:
			// the operator can still record WHAT they saw and WHEN, which is the part a
			// case needs, and the alternative is a button that stops working on old rows.
			return s.addFeedEntry(ctx, caseId, notif, in)
		}
		in.Kind = entities.CaseItemAlert
		in.CameraId = alert.CameraId
		in.SourceId = alert.Id
		in.SnapshotPath = alert.SnapshotPath
		in.Label = alertItemLabel(alert, notif)
		// THE ALERT'S OWN TIME, not the notification's. They are usually within a second of
		// each other and occasionally are not — a relayed or replayed notification carries
		// the time it was ingested here. The evidence is the alert.
		at := alert.CreatedAt
		if at <= 0 {
			at = notif.CreatedAt
		}
		in.StartedAt = at - notificationPadSeconds
		in.EndedAt = at + notificationPadSeconds
		if in.StartedAt < 0 {
			in.StartedAt = 0
		}
		return s.AddItem(ctx, caseId, in)
	}

	return s.addFeedEntry(ctx, caseId, notif, in)
}

// addFeedEntry stores the feed row itself, with footage only when it names a camera.
func (s *caseService) addFeedEntry(ctx context.Context, caseId int64, notif *shared.Notification, in CaseItemInput) (*entities.CaseItem, error) {
	in.Kind = entities.CaseItemNotification
	in.SourceId = notif.Id
	in.Label = feedItemLabel(notif)
	in.CameraId = notif.CameraId
	at := notif.CreatedAt
	if at <= 0 {
		return nil, errors.New("that feed entry has no time on it")
	}
	if notif.CameraId > 0 {
		in.StartedAt = at - notificationPadSeconds
		if in.StartedAt < 0 {
			in.StartedAt = 0
		}
		in.EndedAt = at + notificationPadSeconds
	} else {
		// No camera, so no span: buildItem zeroes EndedAt for this kind anyway, and the
		// moment is what matters.
		in.StartedAt = at
	}
	return s.AddItem(ctx, caseId, in)
}

// notificationRefAlert is the RefType the vision alert path stamps on its feed rows. Spelled
// once here rather than inline: guessing this string is how a resolution silently stops
// resolving and every alert quietly becomes a plain feed entry.
const notificationRefAlert = "alert_event"

// feedItemLabel is what the case screen shows for a feed entry. The TITLE, not the body: a
// case item's label is a caption in a list, and the body is often a paragraph.
func feedItemLabel(notif *shared.Notification) string {
	title := strings.TrimSpace(notif.Title)
	if title != "" {
		return title
	}
	if body := strings.TrimSpace(notif.Body); body != "" {
		return body
	}
	return strings.TrimSpace(notif.Category)
}

// alertItemLabel prefers the alert's own words and falls back to the feed entry's.
func alertItemLabel(alert *entities.AlertEvent, notif *shared.Notification) string {
	if label := strings.TrimSpace(alert.Label); label != "" {
		if alert.Confidence > 0 {
			return fmt.Sprintf("%s (%.0f%%)", label, alert.Confidence*100)
		}
		return label
	}
	return feedItemLabel(notif)
}
