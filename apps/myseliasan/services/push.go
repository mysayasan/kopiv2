package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/webpush"
)

// Mobile push (W3-9), the second half of the notification gap F-20 named.
//
// W2-7 gave the control plane an outbound leg at all — before it, a node going dark at 03:00
// was persisted, logged, streamed to any open browser, and stopped there. Email closed most of
// that. What email does not do is WAKE somebody. This does: an operator watching fifty sites
// gets the phone in their pocket buzzed, which is precisely the situation the finding
// described and the one nobody is looking at a screen for.
//
// THE THING THIS FEATURE MUST BE HONEST ABOUT, and the reason half this file exists:
//
// A Web Push message is delivered by POSTing to an endpoint the BROWSER VENDOR owns —
// fcm.googleapis.com, updates.push.services.mozilla.com, web.push.apple.com. There is no
// standard way to wake a closed phone without one of them. **This control plane is normally
// deployed on an intranet with no internet egress**, where every one of those POSTs fails at
// the TCP connect and no amount of configuration changes it.
//
// So the feature never claims to work. It MEASURES whether it works, per device, by actually
// delivering — and it says which of the four things happened in words an operator can act on:
// the service was never reached (no egress), the subscription is dead, the service refused, or
// it was accepted. Subscribing performs a real delivery immediately, so somebody who turns
// this on finds out in the same second rather than during an incident. That is the W3-7 drill
// applied to a different promise.
//
// WHAT LEAVES THE BUILDING, stated plainly because an air-gapped operator deserves to decide:
// enabling push means this appliance makes outbound HTTPS requests to a browser vendor, and
// the vendor learns that an endpoint it issued received a message and when. The CONTENT is
// end-to-end encrypted to the device (RFC 8291) and the vendor cannot read it. The timing and
// the existence of the message are not hidden from them, and nothing here pretends otherwise.

const (
	// vapidSettingKey is the control-setting row holding the install's VAPID private key,
	// sealed at rest with the same cipher the fleet CA key uses.
	vapidSettingKey = "push.vapid.private"
	// vapidSubjectKey is the contact the push service is given for this install.
	vapidSubjectKey = "push.vapid.subject"
	// pushQueueSize bounds the outbound buffer. A control plane can burst notifications (a
	// switch failing takes fifty nodes with it), and a phone does not need fifty buzzes —
	// see the drop note in Send.
	pushQueueSize = 256
	// pushSendTimeout bounds one delivery.
	pushSendTimeout = 20 * time.Second
	// pushMaxSubscriptions caps a listing read.
	pushMaxSubscriptions = 500
	// pushTitleMax / pushBodyMax keep a payload inside what every push service accepts.
	// Truncating HERE is better than a 413 from a vendor that does not say which
	// notification was too long.
	pushTitleMax = 120
	pushBodyMax  = 480
)

// Delivery states for the install as a whole. They mirror the per-device outcomes but answer
// a different question — "can this appliance push at all" — and they exist as their own
// vocabulary because "untested" and "working" must never look alike on a screen.
const (
	// PushDeliveryUntested means nothing has been attempted yet. It is NOT "fine".
	PushDeliveryUntested = "untested"
	// PushDeliveryConfirmed means a push service accepted a message from this appliance.
	PushDeliveryConfirmed = "confirmed"
	// PushDeliveryUnreachable means no push service could be contacted — the air-gapped case.
	// This is the one an operator must not read as a bug in the product.
	PushDeliveryUnreachable = "unreachable"
	// PushDeliveryRejected means a service was reached and refused every message.
	PushDeliveryRejected = "rejected"
	// PushDeliveryNoDevices means push is configured and nothing has subscribed.
	PushDeliveryNoDevices = "no-devices"
)

// ErrPushNotConfigured is returned when the install has no VAPID identity yet.
var ErrPushNotConfigured = errors.New("push notifications are not set up on this control plane")

// PushSubscribeRequest is what the browser hands back after the user agrees.
type PushSubscribeRequest struct {
	Endpoint    string `json:"endpoint"`
	P256dh      string `json:"p256dh"`
	Auth        string `json:"auth"`
	Label       string `json:"label"`
	MinSeverity string `json:"minSeverity"`
}

// PushDeviceView is one subscription as a screen sees it — never its key material.
type PushDeviceView struct {
	Id     int64  `json:"id"`
	Label  string `json:"label"`
	UserId int64  `json:"userId"`
	// Vendor is the push service's host, so an operator can see WHERE this device is reached
	// and therefore what this appliance has to be able to talk to.
	Vendor          string `json:"vendor"`
	MinSeverity     string `json:"minSeverity"`
	Enabled         bool   `json:"enabled"`
	LastOutcome     string `json:"lastOutcome"`
	LastDetail      string `json:"lastDetail"`
	LastAttemptAt   int64  `json:"lastAttemptAt"`
	LastDeliveredAt int64  `json:"lastDeliveredAt"`
	CreatedAt       int64  `json:"createdAt"`
	Mine            bool   `json:"mine"`
}

// PushStatus is what this appliance can actually do, for the screen.
type PushStatus struct {
	// Configured is whether a VAPID identity exists. Without one no browser can subscribe.
	Configured bool `json:"configured"`
	// PublicKey is what the browser needs to subscribe. Not a secret.
	PublicKey string `json:"publicKey"`
	// Delivery is one of the PushDelivery* values — the install's capability, DERIVED FROM
	// REAL ATTEMPTS rather than from configuration. A feature that reports itself working
	// because it is switched on is the thing this whole programme exists to stop.
	Delivery string `json:"delivery"`
	// Devices / DevicesReached make the verdict checkable rather than something to trust.
	Devices        int `json:"devices"`
	DevicesReached int `json:"devicesReached"`
	// LastAttemptAt / LastDetail carry the most recent evidence.
	LastAttemptAt int64  `json:"lastAttemptAt"`
	LastDetail    string `json:"lastDetail"`
	// Vendors lists the push services this appliance would need to reach, so somebody
	// configuring a firewall has the list without having to read subscription rows.
	Vendors []string `json:"vendors"`
}

// IPushService owns the install's push identity, its devices, and delivery to them.
type IPushService interface {
	// Status reports what this appliance can actually do.
	Status(ctx context.Context, userId int64) (PushStatus, error)
	// Subscribe registers a device AND immediately proves whether it can be reached.
	Subscribe(ctx context.Context, req PushSubscribeRequest, userId int64) (PushDeviceView, error)
	// Unsubscribe removes a device. A user may remove their own; an administrator any.
	Unsubscribe(ctx context.Context, id, userId int64, admin bool) error
	// List returns the devices this user may see.
	List(ctx context.Context, userId int64, admin bool) ([]PushDeviceView, error)
	// TestDevice sends a real notification to one device, on demand.
	TestDevice(ctx context.Context, id, userId int64, admin bool) (PushDeviceView, error)
	// Channel is the notification sink to register with the feed.
	Channel() notification.Channel
	// Close stops the delivery worker.
	Close()
}

type pushService struct {
	subs     dbsql.IGenericRepo[entities.PushSubscription]
	settings dbsql.IGenericRepo[entities.ControlSetting]
	cipher   *atrest.Cipher
	audit    IAuditService
	logf     func(string, ...any)
	// send is the transport, injectable so tests never touch a vendor.
	send func(ctx context.Context, keys webpush.Keys, sub webpush.Subscription, payload []byte, opts webpush.Options) (webpush.Result, error)

	keyMu sync.Mutex
	keys  *webpush.Keys

	queue    chan notification.Notification
	stopOnce sync.Once
	stop     chan struct{}
}

// NewPushService builds the push service and starts its delivery worker.
func NewPushService(
	db dbsql.IDbCrud,
	cipher *atrest.Cipher,
	audit IAuditService,
	logf func(string, ...any),
) IPushService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := newPushServiceWith(
		dbsql.NewGenericRepo[entities.PushSubscription](db),
		dbsql.NewGenericRepo[entities.ControlSetting](db),
		cipher, audit, nil, logf)
	go s.run()
	return s
}

// newPushServiceWith is the injectable constructor. It does NOT start the delivery worker, so a
// test drives fanOut on its own goroutine and asserts on what came out instead of racing a
// background loop. send may be nil to use the real transport — every test passes its own, since
// a unit test that reaches a browser vendor is a unit test that fails on an aeroplane.
func newPushServiceWith(
	subs dbsql.IGenericRepo[entities.PushSubscription],
	settings dbsql.IGenericRepo[entities.ControlSetting],
	cipher *atrest.Cipher,
	audit IAuditService,
	send func(ctx context.Context, keys webpush.Keys, sub webpush.Subscription, payload []byte, opts webpush.Options) (webpush.Result, error),
	logf func(string, ...any),
) *pushService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if send == nil {
		send = webpush.Send
	}
	return &pushService{
		subs:     subs,
		settings: settings,
		cipher:   cipher,
		audit:    audit,
		logf:     logf,
		send:     send,
		queue:    make(chan notification.Notification, pushQueueSize),
		stop:     make(chan struct{}),
	}
}

func (s *pushService) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// --- identity ---------------------------------------------------------------------------

// keysFor returns the install's VAPID identity, creating it on first use.
//
// GENERATED ONCE AND NEVER ROTATED CASUALLY. A browser binds its subscription to the public
// key it subscribed with, so a new key pair silently invalidates every device already
// registered — they stay in the table, they stop being reachable, and the only symptom is
// notifications that quietly stop arriving. There is deliberately no "regenerate" button.
func (s *pushService) keysFor(ctx context.Context) (webpush.Keys, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.keys != nil {
		return *s.keys, nil
	}
	row, err := s.settings.GetByUnique(ctx, "", "key", vapidSettingKey)
	if err == nil && row != nil && strings.TrimSpace(row.Value) != "" {
		private := decodeSecret(s.cipher, row.Value)
		public, perr := webpush.PublicKeyOf(private)
		if perr == nil {
			keys := webpush.Keys{Public: public, Private: private}
			s.keys = &keys
			return keys, nil
		}
		// A key that cannot produce a public half is a key nothing can use. Saying so is
		// better than minting a replacement that silently orphans every device.
		return webpush.Keys{}, fmt.Errorf("the stored push identity is unusable: %w", perr)
	}

	keys, err := webpush.GenerateKeys()
	if err != nil {
		return webpush.Keys{}, err
	}
	sealed, err := encodeSecret(s.cipher, keys.Private)
	if err != nil {
		return webpush.Keys{}, err
	}
	now := time.Now().Unix()
	if _, err := s.settings.Create(ctx, "", entities.ControlSetting{
		Key: vapidSettingKey, Value: sealed, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return webpush.Keys{}, err
	}
	s.keys = &keys
	return keys, nil
}

func (s *pushService) subject(ctx context.Context) string {
	row, err := s.settings.GetByUnique(ctx, "", "key", vapidSubjectKey)
	if err == nil && row != nil && strings.TrimSpace(row.Value) != "" {
		return strings.TrimSpace(row.Value)
	}
	return "mailto:admin@myseliasan.local"
}

// --- devices ----------------------------------------------------------------------------

func (s *pushService) Subscribe(ctx context.Context, req PushSubscribeRequest, userId int64) (PushDeviceView, error) {
	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" || strings.TrimSpace(req.P256dh) == "" || strings.TrimSpace(req.Auth) == "" {
		return PushDeviceView{}, errors.New("this browser did not provide a usable push subscription")
	}
	if u, err := url.Parse(endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
		return PushDeviceView{}, errors.New("the push endpoint this browser provided is not a valid https URL")
	}
	if _, err := s.keysFor(ctx); err != nil {
		return PushDeviceView{}, err
	}

	now := time.Now().Unix()
	row := entities.PushSubscription{
		UserId:      userId,
		Endpoint:    endpoint,
		P256dh:      strings.TrimSpace(req.P256dh),
		Auth:        strings.TrimSpace(req.Auth),
		Label:       pushLabel(req.Label),
		MinSeverity: normalizePushSeverity(req.MinSeverity),
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	// The SAME browser re-subscribing updates its row. Without this, a browser that renews
	// its subscription (which they do) accumulates rows, and every renewal adds one more buzz
	// per notification to the same phone.
	if existing, err := s.subs.GetByUnique(ctx, "", "push_endpoint", endpoint); err == nil && existing != nil {
		row.Id = existing.Id
		row.CreatedAt = existing.CreatedAt
		// Ownership does NOT transfer on re-subscribe: a shared browser must not silently
		// re-point somebody else's device at whoever signed in last.
		row.UserId = existing.UserId
		if _, err := s.subs.UpdateById(ctx, "", row); err != nil {
			return PushDeviceView{}, err
		}
	} else {
		id, err := s.subs.Create(ctx, "", row)
		if err != nil {
			return PushDeviceView{}, err
		}
		row.Id = int64(id)
	}

	// PROVE IT NOW. Somebody who has just agreed to be woken should learn in the same second
	// whether this appliance can actually do it — not during the incident it was for.
	s.deliver(ctx, &row, notification.Notification{
		Category: notification.CategoryHealthCheck,
		Severity: notification.Info,
		Title:    "Notifications are on",
		Body:     "This device will be woken when the fleet needs attention.",
	})

	s.record(ctx, ActionPushSubscribe, row, "registered a device for push notifications")
	return s.view(row, userId, true), nil
}

func (s *pushService) Unsubscribe(ctx context.Context, id, userId int64, admin bool) error {
	row, err := s.subs.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		if err != nil && !isNoResultErr(err) {
			return err
		}
		return errors.New("no such device")
	}
	// A subscription wakes a phone in somebody's pocket. Only its owner, or an administrator
	// cleaning up after somebody who has left, may silence it.
	if row.UserId != userId && !admin {
		return errors.New("that device belongs to somebody else")
	}
	if _, err := s.subs.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	s.record(ctx, ActionPushUnsubscribe, *row, "removed a device from push notifications")
	return nil
}

func (s *pushService) List(ctx context.Context, userId int64, admin bool) ([]PushDeviceView, error) {
	rows, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	out := []PushDeviceView{}
	for _, row := range rows {
		if row.UserId != userId && !admin {
			continue
		}
		out = append(out, s.view(*row, userId, false))
	}
	return out, nil
}

func (s *pushService) TestDevice(ctx context.Context, id, userId int64, admin bool) (PushDeviceView, error) {
	row, err := s.subs.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return PushDeviceView{}, errors.New("no such device")
	}
	if row.UserId != userId && !admin {
		return PushDeviceView{}, errors.New("that device belongs to somebody else")
	}
	s.deliver(ctx, row, notification.Notification{
		Category: notification.CategoryHealthCheck,
		Severity: notification.Info,
		Title:    "Test notification",
		Body:     "If you can read this, this control plane can reach this device.",
	})
	s.record(ctx, ActionPushTest, *row, "sent a test notification to a device")
	return s.view(*row, userId, false), nil
}

func (s *pushService) Status(ctx context.Context, userId int64) (PushStatus, error) {
	out := PushStatus{Delivery: PushDeliveryUntested, Vendors: []string{}}
	keys, err := s.keysFor(ctx)
	if err != nil {
		return out, err
	}
	out.Configured = true
	out.PublicKey = keys.Public

	rows, err := s.all(ctx)
	if err != nil {
		return out, err
	}
	vendors := map[string]bool{}
	attempted, reached, unreachable := 0, 0, 0
	for _, row := range rows {
		out.Devices++
		if host := endpointHost(row.Endpoint); host != "" {
			vendors[host] = true
		}
		if row.LastAttemptAt > out.LastAttemptAt {
			out.LastAttemptAt = row.LastAttemptAt
			out.LastDetail = row.LastDetail
		}
		switch row.LastOutcome {
		case string(webpush.OutcomeDelivered):
			attempted++
			reached++
		case string(webpush.OutcomeUnreachable):
			attempted++
			unreachable++
		case "":
			// never attempted
		default:
			attempted++
		}
	}
	out.DevicesReached = reached
	for host := range vendors {
		out.Vendors = append(out.Vendors, host)
	}

	// THE VERDICT, and the order of these cases is the whole point.
	switch {
	case out.Devices == 0:
		out.Delivery = PushDeliveryNoDevices
	case attempted == 0:
		// Registered but never attempted. NOT "working" — the same rule as W3-7's untested
		// standby: absence of evidence gets its own answer.
		out.Delivery = PushDeliveryUntested
	case reached > 0:
		out.Delivery = PushDeliveryConfirmed
	case unreachable == attempted:
		// Every attempt failed before reaching anything. This is the air-gapped install, and
		// calling it a delivery failure would send somebody hunting a bug in the product.
		out.Delivery = PushDeliveryUnreachable
	default:
		out.Delivery = PushDeliveryRejected
	}
	return out, nil
}

// --- delivery ------------------------------------------------------------------------------

// Channel returns the notification sink. It is registered on the feed, so everything the
// control plane publishes is offered to every device whose floor it clears.
func (s *pushService) Channel() notification.Channel { return pushChannel{svc: s} }

type pushChannel struct{ svc *pushService }

func (pushChannel) Name() string { return "push" }

// Send hands the notification to the worker and returns immediately.
//
// NON-BLOCKING, and a full queue DROPS rather than waits. The hub delivers to every channel in
// turn: a push service that has gone slow must not hold up the SSE stream, the log or the
// database write, all of which still reach somebody. A dropped buzz is a worse outcome than a
// delayed one only until the delay stalls the feed itself.
func (c pushChannel) Send(ctx context.Context, n notification.Notification) error {
	select {
	case c.svc.queue <- n:
		return nil
	default:
		return errors.New("the push queue is full; this notification was not pushed")
	}
}

func (s *pushService) run() {
	for {
		select {
		case <-s.stop:
			return
		case n := <-s.queue:
			s.fanOut(n)
		}
	}
}

// fanOut delivers one notification to every device whose floor it clears.
func (s *pushService) fanOut(n notification.Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), pushSendTimeout*2)
	defer cancel()
	rows, err := s.all(ctx)
	if err != nil {
		s.logf("listing push devices: %v", err)
		return
	}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		if !pushSeverityAllows(row.MinSeverity, n.Severity) {
			continue
		}
		s.deliver(ctx, row, n)
	}
}

// deliver sends to one device and records what happened.
//
// THE OUTCOME IS WRITTEN DOWN EVERY TIME, because it is the only evidence this feature works.
// And a GONE subscription is DELETED: a device that has been uninstalled or had its site data
// cleared will never accept another message, and a row that is never removed is a push attempt
// on every notification, forever, plus a permanently red line on somebody's screen.
func (s *pushService) deliver(ctx context.Context, row *entities.PushSubscription, n notification.Notification) {
	keys, err := s.keysFor(ctx)
	if err != nil {
		s.logf("push identity unavailable: %v", err)
		return
	}
	payload, err := json.Marshal(map[string]any{
		"title":    truncatePush(n.Title, pushTitleMax),
		"body":     truncatePush(n.Body, pushBodyMax),
		"severity": string(n.Severity),
		"category": n.Category,
		"source":   n.Source,
		"at":       time.Now().Unix(),
	})
	if err != nil {
		s.logf("push payload: %v", err)
		return
	}

	sctx, cancel := context.WithTimeout(ctx, pushSendTimeout)
	defer cancel()
	urgency := "normal"
	if n.Severity == notification.Critical {
		urgency = "high"
	}
	res, _ := s.send(sctx, keys, webpush.Subscription{
		Endpoint: row.Endpoint, P256dh: row.P256dh, Auth: row.Auth,
	}, payload, webpush.Options{
		Subject: s.subject(ctx),
		TTL:     6 * time.Hour,
		Urgency: urgency,
	})

	now := time.Now().Unix()
	if res.Outcome == webpush.OutcomeGone {
		if _, derr := s.subs.DeleteById(ctx, "", uint64(row.Id)); derr != nil {
			s.logf("removing dead push device %d: %v", row.Id, derr)
		}
		return
	}
	row.LastOutcome = string(res.Outcome)
	row.LastDetail = truncatePush(res.Detail, 300)
	row.LastAttemptAt = now
	if res.Outcome == webpush.OutcomeDelivered {
		row.LastDeliveredAt = now
	}
	row.UpdatedAt = now
	if _, uerr := s.subs.UpdateById(ctx, "", *row); uerr != nil {
		s.logf("recording push outcome for device %d: %v", row.Id, uerr)
	}
}

// --- plumbing -------------------------------------------------------------------------------

func (s *pushService) all(ctx context.Context) ([]*entities.PushSubscription, error) {
	rows, _, err := s.subs.Get(ctx, "", pushMaxSubscriptions, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	out := make([]*entities.PushSubscription, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *pushService) view(row entities.PushSubscription, userId int64, mine bool) PushDeviceView {
	return PushDeviceView{
		Id: row.Id, Label: row.Label, UserId: row.UserId,
		Vendor:      endpointHost(row.Endpoint),
		MinSeverity: row.MinSeverity, Enabled: row.Enabled,
		LastOutcome: row.LastOutcome, LastDetail: row.LastDetail,
		LastAttemptAt: row.LastAttemptAt, LastDeliveredAt: row.LastDeliveredAt,
		CreatedAt: row.CreatedAt,
		Mine:      mine || row.UserId == userId,
	}
}

func (s *pushService) record(ctx context.Context, action string, row entities.PushSubscription, detail string) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, AuditEntry{
		Action:     action,
		TargetType: "push",
		TargetId:   fmt.Sprintf("%d", row.Id),
		Outcome:    OutcomeSuccess,
		Detail:     detail + " (" + endpointHost(row.Endpoint) + ")",
		// The endpoint itself is NOT recorded: it is a third-party identifier for somebody's
		// personal device, and an audit trail is a long-lived document read by people who did
		// not need to know which phone anybody carries. The vendor is enough to answer the
		// question a trail is for.
		Metadata: map[string]any{"vendor": endpointHost(row.Endpoint), "label": row.Label},
	})
}

// endpointHost is the vendor's host, which is all anybody but the sender needs.
func endpointHost(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func pushLabel(raw string) string {
	label := strings.TrimSpace(raw)
	if label == "" {
		return "This device"
	}
	return truncatePush(label, 60)
}

func normalizePushSeverity(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	// Normalize() answers "info" for anything it does not recognise, INCLUDING the empty
	// string. That default is right for the hub and wrong here: a device that was never given
	// a floor would be woken by every routine event, and a phone that buzzes for everything is
	// a phone whose owner mutes the app — which is the same as having no push at all. So the
	// unset case is decided before Normalize sees it.
	if trimmed == "" {
		return string(notification.Warning)
	}
	return string(notification.Severity(trimmed).Normalize())
}

func pushSeverityAllows(min string, sev notification.Severity) bool {
	filter := notification.Filter{MinSeverity: notification.Severity(min).Normalize()}
	return filter.Allows(notification.Notification{Severity: sev})
}

// truncatePush bounds a string to max BYTES, cutting on a rune boundary.
//
// Bytes, because the limit that matters is the push service's, and it counts bytes. Rune
// boundary, because three of this product's four languages are not ASCII: slicing a byte at a
// time through Chinese or Arabic leaves a broken final character, which a phone renders as a
// replacement box on the one screen nobody can go and correct.
func truncatePush(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	const ellipsis = "…"
	budget := max - len(ellipsis)
	cut := 0
	for i := range s {
		if i > budget {
			break
		}
		cut = i
	}
	return s[:cut] + ellipsis
}
