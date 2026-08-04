package services

import (
	"context"
	"fmt"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	applog "github.com/mysayasan/kopiv2/infra/logging"
	"golang.org/x/crypto/bcrypt"
)

// BcryptPIN is the production PIN verifier.
//
// A PIN is a secret and is checked the way myidsan checks passwords. bcrypt is deliberately slow,
// which matters more here than for a password: a 4-digit PIN space is only 10,000 wide, so the cost
// of the hash is essentially the only thing standing between an attacker holding the database and
// every PIN on the site.
//
// It is injected rather than called from the decision path so Decide stays a pure function with no
// crypto dependency — Decide's job is the ORDER of the gates, not the strength of a hash.
func BcryptPIN(hash, presented string) bool {
	if hash == "" || presented == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(presented)) == nil
}

// HashPIN produces a storable PIN hash. Enrolment uses it; nothing else should.
func HashPIN(pin string) (string, error) {
	if pin == "" {
		return "", fmt.Errorf("a PIN cannot be empty")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash PIN: %w", err)
	}
	return string(h), nil
}

// NotificationAlarmer routes door alarms into the shared notification feed, so they land where an
// operator already looks rather than in a log nobody opens.
type NotificationAlarmer struct {
	notify *notification.Service
	log    applog.Logger
}

// NewNotificationAlarmer builds the production alarm sink.
func NewNotificationAlarmer(notify *notification.Service, log applog.Logger) *NotificationAlarmer {
	return &NotificationAlarmer{notify: notify, log: log}
}

// severity maps an alarm kind to how loudly it arrives.
//
// Duress and door-forced are CRITICAL: both mean either a person is in trouble or a boundary has
// been breached, and neither can wait for somebody to notice a row on a dashboard. A reader going
// offline is a WARNING — every door bound to it is out of service, which is serious but not an
// emergency, and on a site with one flaky segment, paging on it would train people to ignore alarms
// altogether. That is the failure mode worth avoiding: an alarm nobody believes.
func severity(kind string) notification.Severity {
	switch kind {
	case AlarmDuress, AlarmDoorForced:
		return notification.Critical
	default:
		return notification.Warning
	}
}

// Raise publishes an alarm.
//
// Note what it does NOT do: touch the reader. A duress alarm must be invisible at the door — no
// LED, no buzzer, nothing the person standing there can observe — because a coercer who can tell an
// alarm was raised is a coercer who takes it out on the holder.
func (a *NotificationAlarmer) Raise(ctx context.Context, kind string, ev entities.AccessEvent, detail string) {
	if a == nil || a.notify == nil {
		return
	}
	body := detail
	if body == "" {
		body = kind
	}
	a.notify.Publish(ctx, notification.Notification{
		Category: "access." + kind,
		Severity: severity(kind),
		Title:    alarmTitle(kind),
		Body:     body,
		Source:   "mypintusan",
		RefType:  "access_event",
		RefId:    ev.Id,
		Data: map[string]any{
			"doorId":   ev.DoorId,
			"readerId": ev.ReaderId,
			"holderId": ev.HolderId,
			"holder":   ev.HolderName,
			"reason":   ev.Reason,
		},
	})
}

// alarmTitle is the headline an operator sees. Deliberately plain: an alarm headline is read at
// 3am, on a phone, by somebody who was asleep.
func alarmTitle(kind string) string {
	switch kind {
	case AlarmDuress:
		return "DURESS — access granted under a duress PIN"
	case AlarmDoorForced:
		return "Door forced open"
	case AlarmDoorHeldOpen:
		return "Door held open"
	case AlarmTamper:
		return "Reader tamper"
	case AlarmReaderOffline:
		return "Reader offline — doors out of service"
	case AlarmSecureChannel:
		return "Reader secure channel fault"
	default:
		return "Door alarm: " + kind
	}
}
