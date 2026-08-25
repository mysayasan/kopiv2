package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/webpush"
)

// --- fakes ---------------------------------------------------------------------------

type fakePushRepo struct {
	dbsql.IGenericRepo[entities.PushSubscription]
	mu   sync.Mutex
	rows []*entities.PushSubscription
	seq  int64
}

func (f *fakePushRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.PushSubscription, uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*entities.PushSubscription{}
	for _, row := range f.rows {
		cp := *row
		out = append(out, &cp)
	}
	return out, uint64(len(out)), nil
}

func (f *fakePushRepo) GetById(_ context.Context, _ string, id uint64) (*entities.PushSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakePushRepo) GetByUnique(_ context.Context, _ string, ukey string, values ...any) (*entities.PushSubscription, error) {
	if ukey != "push_endpoint" || len(values) != 1 {
		return nil, errors.New("no result found")
	}
	want, _ := values[0].(string)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.Endpoint == want {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakePushRepo) Create(_ context.Context, _ string, model entities.PushSubscription) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakePushRepo) UpdateById(_ context.Context, _ string, model entities.PushSubscription) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakePushRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakePushRepo) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func (f *fakePushRepo) byEndpoint(endpoint string) *entities.PushSubscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.Endpoint == endpoint {
			cp := *row
			return &cp
		}
	}
	return nil
}

type fakeSettingRepo struct {
	dbsql.IGenericRepo[entities.ControlSetting]
	rows []*entities.ControlSetting
	seq  int64
}

func (f *fakeSettingRepo) GetByUnique(_ context.Context, _ string, ukey string, values ...any) (*entities.ControlSetting, error) {
	if ukey != "key" || len(values) != 1 {
		return nil, errors.New("no result found")
	}
	want, _ := values[0].(string)
	for _, row := range f.rows {
		if row.Key == want {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeSettingRepo) Create(_ context.Context, _ string, model entities.ControlSetting) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

// pushRig is a push service whose transport is a script rather than the internet.
type pushRig struct {
	svc  *pushService
	subs *fakePushRepo
	set  *fakeSettingRepo

	mu       sync.Mutex
	sent     []webpush.Subscription
	payloads [][]byte
	// outcome decides what the fake transport answers, per endpoint. An endpoint with no
	// entry is delivered successfully.
	outcome map[string]webpush.Outcome
}

func newPushRig() *pushRig {
	rig := &pushRig{subs: &fakePushRepo{}, set: &fakeSettingRepo{}, outcome: map[string]webpush.Outcome{}}
	rig.svc = newPushServiceWith(rig.subs, rig.set, nil, nil,
		func(_ context.Context, _ webpush.Keys, sub webpush.Subscription, payload []byte, _ webpush.Options) (webpush.Result, error) {
			rig.mu.Lock()
			rig.sent = append(rig.sent, sub)
			rig.payloads = append(rig.payloads, payload)
			out, ok := rig.outcome[sub.Endpoint]
			rig.mu.Unlock()
			if !ok {
				out = webpush.OutcomeDelivered
			}
			switch out {
			case webpush.OutcomeDelivered:
				return webpush.Result{Outcome: out, Status: 201}, nil
			case webpush.OutcomeUnreachable:
				return webpush.Result{Outcome: out, Detail: "no route to host"}, errors.New("dial tcp: no route to host")
			case webpush.OutcomeGone:
				return webpush.Result{Outcome: out, Status: 410, Detail: "gone"}, nil
			default:
				return webpush.Result{Outcome: out, Status: 400, Detail: "refused"}, nil
			}
		}, nil)
	return rig
}

func (r *pushRig) sentCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func (r *pushRig) sentTo(endpoint string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.sent {
		if s.Endpoint == endpoint {
			n++
		}
	}
	return n
}

func (r *pushRig) lastPayload(t *testing.T) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.payloads) == 0 {
		t.Fatal("nothing was ever sent")
	}
	var out map[string]any
	if err := json.Unmarshal(r.payloads[len(r.payloads)-1], &out); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	return out
}

func subscribeReq(endpoint, label, min string) PushSubscribeRequest {
	return PushSubscribeRequest{Endpoint: endpoint, P256dh: "BP-fake-ua-public", Auth: "fake-auth", Label: label, MinSeverity: min}
}

const epA = "https://fcm.googleapis.com/fcm/send/aaa"
const epB = "https://updates.push.services.mozilla.com/wpush/v2/bbb"

// --- enrolment ------------------------------------------------------------------------

// Subscribing must PROVE the device can be reached, not merely record that somebody agreed.
// The whole point of this feature's honesty is that the verdict comes from a real attempt, and
// the moment to discover an install cannot push is while looking at the button — not at 3am.
func TestSubscribeProvesTheDeviceBeforeItReturns(t *testing.T) {
	rig := newPushRig()
	view, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "Ahmad's phone", "warning"), 7)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if rig.sentTo(epA) != 1 {
		t.Fatalf("subscribing sent %d messages to the new device; it must send exactly one, or "+
			"nothing has been proved and the screen is reporting a guess", rig.sentTo(epA))
	}
	if view.LastOutcome != string(webpush.OutcomeDelivered) {
		t.Fatalf("outcome after a successful proof = %q, want %q", view.LastOutcome, webpush.OutcomeDelivered)
	}
	if view.LastDeliveredAt == 0 {
		t.Fatal("a delivered proof left LastDeliveredAt unset")
	}
	if !view.Mine {
		t.Fatal("the device somebody just enrolled did not come back as theirs")
	}
	if view.Vendor != "fcm.googleapis.com" {
		t.Fatalf("vendor = %q, want the endpoint host", view.Vendor)
	}
}

// An install with no egress must still ENROL the device and say plainly that nothing could be
// reached. Refusing the subscription would be worse: the operator would keep pressing it, and
// the row recording "we tried and could not" is the evidence the screen needs.
func TestSubscribeOnAnAirGappedInstallRecordsUnreachableRatherThanFailing(t *testing.T) {
	rig := newPushRig()
	rig.outcome[epA] = webpush.OutcomeUnreachable
	view, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "warning"), 7)
	if err != nil {
		t.Fatalf("subscribe on an air-gapped install returned an error: %v", err)
	}
	if view.LastOutcome != string(webpush.OutcomeUnreachable) {
		t.Fatalf("outcome = %q, want %q", view.LastOutcome, webpush.OutcomeUnreachable)
	}
	if view.LastDeliveredAt != 0 {
		t.Fatal("a device that was never reached was marked as delivered to")
	}
	st, err := rig.svc.Status(context.Background(), 7)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Delivery != PushDeliveryUnreachable {
		t.Fatalf("install verdict = %q, want %q — an air-gapped install reported as a delivery "+
			"failure sends somebody hunting a bug in the product", st.Delivery, PushDeliveryUnreachable)
	}
}

// Browsers renew their subscriptions. Every renewal that added a row would add one more buzz
// per notification to the SAME phone, and nothing about that looks broken from the server.
func TestReSubscribingTheSameBrowserUpdatesItsRowInsteadOfAddingOne(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "warning"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone (renewed)", "critical"), 7); err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	if got := rig.subs.count(); got != 1 {
		t.Fatalf("%d rows after a renewal from the same browser, want 1 — every extra row is one "+
			"more buzz for the same notification on the same phone", got)
	}
	row := rig.subs.byEndpoint(epA)
	if row.Label != "phone (renewed)" || row.MinSeverity != string(notification.Critical) {
		t.Fatalf("the renewal did not update the row: label=%q min=%q", row.Label, row.MinSeverity)
	}
}

// A shared browser must not silently re-point somebody else's device at whoever signed in last.
func TestReSubscribingDoesNotStealTheDeviceFromItsOwner(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "warning"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "warning"), 9); err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	if owner := rig.subs.byEndpoint(epA).UserId; owner != 7 {
		t.Fatalf("owner after somebody else re-subscribed = %d, want 7", owner)
	}
}

func TestSubscribeRejectsAnUnusableSubscription(t *testing.T) {
	rig := newPushRig()
	cases := map[string]PushSubscribeRequest{
		"no endpoint": {P256dh: "x", Auth: "y"},
		"no keys":     {Endpoint: epA},
		"not https":   subscribeReq("http://fcm.googleapis.com/x", "", ""),
		"not a url":   subscribeReq("not-a-url", "", ""),
	}
	for name, req := range cases {
		if _, err := rig.svc.Subscribe(context.Background(), req, 7); err == nil {
			t.Fatalf("%s was accepted as a push subscription", name)
		}
	}
	if rig.sentCount() != 0 {
		t.Fatal("a rejected subscription still attempted a delivery")
	}
}

// --- the per-device floor ----------------------------------------------------------------

// A device with no floor must default to WARNING, not to the hub's own default of info. This
// is the difference between a phone that buzzes when the fleet needs attention and a phone
// whose owner mutes the app by the end of the first day — and a muted app is the same as no
// push at all.
func TestAnUnsetSeverityFloorDefaultsToWarningNotInfo(t *testing.T) {
	if got := normalizePushSeverity(""); got != string(notification.Warning) {
		t.Fatalf("default floor = %q, want %q", got, notification.Warning)
	}
	if got := normalizePushSeverity("   "); got != string(notification.Warning) {
		t.Fatalf("blank floor = %q, want %q", got, notification.Warning)
	}
	if got := normalizePushSeverity("info"); got != string(notification.Info) {
		t.Fatalf("an EXPLICIT info floor must be honoured, got %q", got)
	}
	if got := normalizePushSeverity("CRITICAL"); got != string(notification.Critical) {
		t.Fatalf("case-insensitive floor = %q, want critical", got)
	}
}

// The floor is per DEVICE. The phone in somebody's pocket at 3am and the laptop on their desk
// want different thresholds, and one install-wide filter would force the stricter one on
// everybody or the looser one on the person who then mutes the app.
func TestEachDeviceGetsOnlyWhatClearsItsOwnFloor(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "critical"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epB, "laptop", "info"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	beforeA, beforeB := rig.sentTo(epA), rig.sentTo(epB)

	rig.svc.fanOut(notification.Notification{Severity: notification.Warning, Title: "A node went offline"})

	if got := rig.sentTo(epA) - beforeA; got != 0 {
		t.Fatalf("the critical-only phone was woken by a warning (%d extra sends)", got)
	}
	if got := rig.sentTo(epB) - beforeB; got != 1 {
		t.Fatalf("the info-floor laptop got %d copies of a warning, want 1", got)
	}
}

func TestADisabledDeviceIsSkippedButNotForgotten(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	row := rig.subs.byEndpoint(epA)
	row.Enabled = false
	if _, err := rig.subs.UpdateById(context.Background(), "", *row); err != nil {
		t.Fatalf("park: %v", err)
	}
	before := rig.sentTo(epA)

	rig.svc.fanOut(notification.Notification{Severity: notification.Critical, Title: "Everything is on fire"})

	if got := rig.sentTo(epA) - before; got != 0 {
		t.Fatalf("a parked device was still sent %d notifications", got)
	}
	if rig.subs.count() != 1 {
		t.Fatal("parking a device deleted it; a phone left at home for a week must come back")
	}
}

// --- the dead-subscription rule -----------------------------------------------------------

// A push service answering 410 Gone is telling us this subscription will NEVER work again —
// the app was uninstalled or the site data cleared. Keeping the row means an outbound request
// per notification forever and a permanently red line on somebody's screen.
func TestAGoneSubscriptionIsDeletedRatherThanMarkedFailed(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	rig.outcome[epA] = webpush.OutcomeGone

	rig.svc.fanOut(notification.Notification{Severity: notification.Critical, Title: "x"})

	if rig.subs.count() != 0 {
		t.Fatalf("a subscription the push service reported as gone is still in the table; it "+
			"will be retried on every notification, forever (%d rows)", rig.subs.count())
	}
}

// A refusal is NOT a death. A service that rejects one message (a bad VAPID clock, a rate
// limit) must leave the device enrolled, or a transient fault silently unenrols the fleet.
func TestARejectedDeliveryKeepsTheDeviceAndRecordsWhy(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	rig.outcome[epA] = webpush.OutcomeRejected

	rig.svc.fanOut(notification.Notification{Severity: notification.Critical, Title: "x"})

	if rig.subs.count() != 1 {
		t.Fatal("a single refused delivery unenrolled the device")
	}
	row := rig.subs.byEndpoint(epA)
	if row.LastOutcome != string(webpush.OutcomeRejected) || row.LastDetail == "" {
		t.Fatalf("refusal not recorded: outcome=%q detail=%q", row.LastOutcome, row.LastDetail)
	}
}

// --- the install verdict --------------------------------------------------------------------

// The ORDER of these cases is the feature's honesty. "Registered but never tried" must never
// read as "working", and one reachable device is enough to prove the appliance has egress.
func TestInstallVerdictSeparatesUntestedFromWorkingFromNoEgress(t *testing.T) {
	t.Run("no devices", func(t *testing.T) {
		rig := newPushRig()
		st, _ := rig.svc.Status(context.Background(), 7)
		if st.Delivery != PushDeliveryNoDevices {
			t.Fatalf("verdict = %q, want %q", st.Delivery, PushDeliveryNoDevices)
		}
		if !st.Configured || st.PublicKey == "" {
			t.Fatal("status did not mint a VAPID identity; no browser can subscribe without one")
		}
	})

	t.Run("registered but never attempted", func(t *testing.T) {
		rig := newPushRig()
		// A row written by something other than Subscribe — a restored backup, say. It has
		// never been proved, and it must not be counted as working.
		if _, err := rig.subs.Create(context.Background(), "", entities.PushSubscription{
			UserId: 7, Endpoint: epA, P256dh: "x", Auth: "y", Enabled: true, MinSeverity: "warning",
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		st, _ := rig.svc.Status(context.Background(), 7)
		if st.Delivery != PushDeliveryUntested {
			t.Fatalf("verdict for a never-attempted device = %q, want %q — absence of evidence "+
				"is not evidence of delivery", st.Delivery, PushDeliveryUntested)
		}
	})

	t.Run("one reachable among failures is confirmed", func(t *testing.T) {
		rig := newPushRig()
		rig.outcome[epB] = webpush.OutcomeUnreachable
		if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
			t.Fatalf("subscribe a: %v", err)
		}
		if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epB, "laptop", "info"), 7); err != nil {
			t.Fatalf("subscribe b: %v", err)
		}
		st, _ := rig.svc.Status(context.Background(), 7)
		if st.Delivery != PushDeliveryConfirmed {
			t.Fatalf("verdict = %q, want %q", st.Delivery, PushDeliveryConfirmed)
		}
		if st.DevicesReached != 1 || st.Devices != 2 {
			t.Fatalf("counts = %d of %d reached, want 1 of 2 — the verdict has to be checkable, not trusted",
				st.DevicesReached, st.Devices)
		}
	})

	t.Run("every attempt refused is rejected, not unreachable", func(t *testing.T) {
		rig := newPushRig()
		rig.outcome[epA] = webpush.OutcomeRejected
		if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		st, _ := rig.svc.Status(context.Background(), 7)
		if st.Delivery != PushDeliveryRejected {
			t.Fatalf("verdict = %q, want %q — a service that answered and refused is a different "+
				"problem from one that could not be reached, and they need different fixes",
				st.Delivery, PushDeliveryRejected)
		}
	})
}

// Somebody configuring a firewall needs the list of hosts this appliance must reach without
// having to read subscription rows out of the database.
func TestStatusNamesTheVendorsThisApplianceMustReach(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
		t.Fatalf("subscribe a: %v", err)
	}
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epB, "laptop", "info"), 9); err != nil {
		t.Fatalf("subscribe b: %v", err)
	}
	st, _ := rig.svc.Status(context.Background(), 7)
	joined := strings.Join(st.Vendors, ",")
	for _, want := range []string{"fcm.googleapis.com", "updates.push.services.mozilla.com"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("vendors %q does not name %q", joined, want)
		}
	}
}

// --- ownership -------------------------------------------------------------------------------

func TestADeviceIsPersonalAndOnlyItsOwnerOrASuperadminMayTouchIt(t *testing.T) {
	rig := newPushRig()
	view, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "Ahmad's phone", "info"), 7)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := rig.svc.Unsubscribe(context.Background(), view.Id, 9, false); err == nil {
		t.Fatal("somebody else silenced a phone that was not theirs")
	}
	if _, err := rig.svc.TestDevice(context.Background(), view.Id, 9, false); err == nil {
		t.Fatal("somebody else buzzed a phone that was not theirs")
	}
	mine, err := rig.svc.List(context.Background(), 9, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mine) != 0 {
		t.Fatalf("a non-admin saw %d of somebody else's devices", len(mine))
	}
	all, err := rig.svc.List(context.Background(), 9, true)
	if err != nil {
		t.Fatalf("list as admin: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("an administrator saw %d devices, want 1 — somebody has to be able to clean up "+
			"after a person who has left", len(all))
	}
	if err := rig.svc.Unsubscribe(context.Background(), view.Id, 9, true); err != nil {
		t.Fatalf("an administrator could not remove a departed user's device: %v", err)
	}
}

func TestTestDeviceSendsToThatDeviceOnly(t *testing.T) {
	rig := newPushRig()
	a, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7)
	if err != nil {
		t.Fatalf("subscribe a: %v", err)
	}
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epB, "laptop", "info"), 7); err != nil {
		t.Fatalf("subscribe b: %v", err)
	}
	beforeA, beforeB := rig.sentTo(epA), rig.sentTo(epB)

	if _, err := rig.svc.TestDevice(context.Background(), a.Id, 7, false); err != nil {
		t.Fatalf("test: %v", err)
	}
	if rig.sentTo(epA)-beforeA != 1 {
		t.Fatal("testing one device did not send to it")
	}
	if rig.sentTo(epB)-beforeB != 0 {
		t.Fatal("testing one device also buzzed another")
	}
}

// A test is a REAL delivery, so it must clear a floor it would otherwise fail. Pressing "test"
// on a critical-only phone and getting nothing, with no error, is the worst possible answer.
func TestTestDeviceIgnoresTheDevicesOwnFloor(t *testing.T) {
	rig := newPushRig()
	a, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "critical"), 7)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	before := rig.sentTo(epA)
	if _, err := rig.svc.TestDevice(context.Background(), a.Id, 7, false); err != nil {
		t.Fatalf("test: %v", err)
	}
	if rig.sentTo(epA)-before != 1 {
		t.Fatal("a test notification was swallowed by the device's own severity floor")
	}
}

// --- the channel ------------------------------------------------------------------------------

// The hub delivers to every channel in turn. A push service that has gone slow must not hold up
// the SSE stream, the log or the database write — all of which still reach somebody.
func TestTheChannelDropsRatherThanBlockingTheFeed(t *testing.T) {
	rig := newPushRig()
	ch := rig.svc.Channel()
	if ch.Name() != "push" {
		t.Fatalf("channel name = %q", ch.Name())
	}
	// Nothing is draining the queue: this is exactly the "push has gone slow" case.
	for i := 0; i < pushQueueSize; i++ {
		if err := ch.Send(context.Background(), notification.Notification{Title: "x"}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- ch.Send(context.Background(), notification.Notification{Title: "overflow"}) }()
	if err := <-done; err == nil {
		t.Fatal("the channel accepted a notification it had no room for")
	}
}

// What a phone actually shows. The payload is what a person reads at 3am with the screen at
// arm's length, so it carries the severity and the title rather than an id to look up.
func TestThePayloadCarriesWhatAPhoneNeedsToShow(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	rig.svc.fanOut(notification.Notification{
		Severity: notification.Critical,
		Category: "node.offline",
		Source:   "node-a",
		Title:    "Site A is offline",
		Body:     "No heartbeat for 4 minutes.",
	})
	got := rig.lastPayload(t)
	for k, want := range map[string]string{
		"title":    "Site A is offline",
		"body":     "No heartbeat for 4 minutes.",
		"severity": "critical",
		"category": "node.offline",
		"source":   "node-a",
	} {
		if got[k] != want {
			t.Fatalf("payload[%q] = %v, want %q", k, got[k], want)
		}
	}
}

// A vendor that answers 413 does not say WHICH notification was too long. Truncating here is
// the difference between one clipped sentence and an alert nobody ever receives.
func TestAnOverlongNotificationIsTruncatedRatherThanRefusedByTheVendor(t *testing.T) {
	rig := newPushRig()
	if _, err := rig.svc.Subscribe(context.Background(), subscribeReq(epA, "phone", "info"), 7); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	rig.svc.fanOut(notification.Notification{
		Severity: notification.Critical,
		Title:    strings.Repeat("T", pushTitleMax*3),
		Body:     strings.Repeat("B", pushBodyMax*3),
	})
	got := rig.lastPayload(t)
	title, _ := got["title"].(string)
	body, _ := got["body"].(string)
	if len(title) > pushTitleMax || len(body) > pushBodyMax {
		t.Fatalf("payload was not truncated: title %d, body %d bytes", len(title), len(body))
	}
	if !strings.HasPrefix(title, "TTT") {
		t.Fatalf("truncation lost the beginning of the title: %q", title)
	}
}

// Three of this product's four languages are not ASCII. Cutting a byte at a time through
// Chinese or Arabic leaves a broken final character, and the place it shows up is a phone's
// lock screen — the one surface nobody can go and correct.
func TestTruncationCutsOnACharacterNotAByte(t *testing.T) {
	for name, s := range map[string]string{
		"chinese": strings.Repeat("节点离线", 200),
		"arabic":  strings.Repeat("العقدة غير متصلة ", 100),
		"malay":   strings.Repeat("Nod tidak dapat dihubungi ", 100),
	} {
		// SWEPT, not tested at one limit. A single limit passes by luck whenever it happens to
		// land on a character boundary — which is most of the time for a 3-byte script, and is
		// exactly how a byte-slicing bug ships green.
		for limit := 8; limit <= pushBodyMax; limit++ {
			got := truncatePush(s, limit)
			if len(got) > limit {
				t.Fatalf("%s at limit %d: %d bytes, over the byte limit the push service counts",
					name, limit, len(got))
			}
			if !utf8.ValidString(got) {
				t.Fatalf("%s at limit %d: truncation produced invalid UTF-8 (%q); a phone shows "+
					"that as a replacement box, on the one screen nobody can go and correct",
					name, limit, got)
			}
			if !strings.HasSuffix(got, "…") {
				t.Fatalf("%s at limit %d: truncated text does not show it was cut: %q", name, limit, got)
			}
		}
	}
}

// --- the identity -------------------------------------------------------------------------------

// A browser binds its subscription to the public key it subscribed with. A second key pair
// would silently orphan every device already enrolled: they stay in the table, they stop being
// reachable, and the only symptom is notifications that quietly stop arriving.
func TestTheVapidIdentityIsMintedOnceAndReused(t *testing.T) {
	rig := newPushRig()
	first, err := rig.svc.Status(context.Background(), 7)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// Drop the memoised copy so the second read has to come from storage — the case that
	// matters is a RESTART, not a second call in the same process.
	rig.svc.keys = nil
	second, err := rig.svc.Status(context.Background(), 7)
	if err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	if first.PublicKey == "" || first.PublicKey != second.PublicKey {
		t.Fatalf("the install minted a second VAPID key across a restart (%q then %q); every "+
			"device already enrolled would go silently unreachable", first.PublicKey, second.PublicKey)
	}
	if len(rig.set.rows) != 1 {
		t.Fatalf("%d identity rows stored, want 1", len(rig.set.rows))
	}
}

func TestEndpointHostIsAllAnybodyButTheSenderNeeds(t *testing.T) {
	cases := map[string]string{
		epA:              "fcm.googleapis.com",
		epB:              "updates.push.services.mozilla.com",
		"":               "",
		"not a url":      "",
		"https://x/y/z?": "x",
	}
	for in, want := range cases {
		if got := endpointHost(in); got != want {
			t.Fatalf("endpointHost(%q) = %q, want %q", in, got, want)
		}
	}
}
