package services

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/infra/access/osdp"
)

// This file wires the REAL OSDP driver to the REAL decision path over a real connection: a
// simulated reader on one end, the controller on the other, and a physical strike output in
// between. It is the "one door, one reader, working end to end" milestone of P1 — and it needs no
// hardware, which is the entire argument for having built the simulator first.

// memStore is an in-memory Store. It is deliberately dumb: the controller's job is the ORDER of
// operations and the shape of the audit row, not query performance.
type memStore struct {
	mu sync.Mutex

	readers   map[string]*entities.Reader // "port/addr"
	doors     map[int64]*entities.Door
	creds     map[string]*entities.Credential // "format/facility/number"
	holders   map[int64]*entities.Holder
	grants    map[int64][]entities.Grant // by holder
	schedules map[int64]entities.Schedule
	windows   map[int64][]entities.ScheduleWindow
	holidays  map[string]*entities.Holiday

	events   []entities.AccessEvent
	recErr   error
	credErr  error
	eventsCh chan entities.AccessEvent
}

func newStore() *memStore {
	return &memStore{
		readers: map[string]*entities.Reader{}, doors: map[int64]*entities.Door{},
		creds: map[string]*entities.Credential{}, holders: map[int64]*entities.Holder{},
		grants: map[int64][]entities.Grant{}, schedules: map[int64]entities.Schedule{},
		windows: map[int64][]entities.ScheduleWindow{}, holidays: map[string]*entities.Holiday{},
		eventsCh: make(chan entities.AccessEvent, 64),
	}
}

func rkey(port string, addr int) string { return port + "/" + strconv.Itoa(addr) }
func ckey(format string, facility int, number string) string {
	return format + "/" + strconv.Itoa(facility) + "/" + number
}

func (m *memStore) ReaderByBus(_ context.Context, port string, addr int) (*entities.Reader, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readers[rkey(port, addr)], nil
}
func (m *memStore) Door(_ context.Context, id int64) (*entities.Door, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.doors[id], nil
}
func (m *memStore) CredentialByCard(_ context.Context, f string, fc int, n string) (*entities.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.credErr != nil {
		return nil, m.credErr
	}
	return m.creds[ckey(f, fc, n)], nil
}
func (m *memStore) Holder(_ context.Context, id int64) (*entities.Holder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.holders[id], nil
}
func (m *memStore) GrantsFor(_ context.Context, holderId, doorId int64) ([]entities.Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entities.Grant
	for _, g := range m.grants[holderId] {
		if g.DoorId == doorId {
			out = append(out, g)
		}
	}
	return out, nil
}
func (m *memStore) Schedules(_ context.Context, ids []int64) (map[int64]entities.Schedule, map[int64][]entities.ScheduleWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := map[int64]entities.Schedule{}
	w := map[int64][]entities.ScheduleWindow{}
	for _, id := range ids {
		if sc, ok := m.schedules[id]; ok {
			s[id] = sc
			w[id] = m.windows[id]
		}
	}
	return s, w, nil
}
func (m *memStore) HolidayOn(_ context.Context, _ int64, date string) (*entities.Holiday, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.holidays[date], nil
}
func (m *memStore) RecordEvent(_ context.Context, ev entities.AccessEvent) error {
	m.mu.Lock()
	if m.recErr != nil {
		err := m.recErr
		m.mu.Unlock()
		return err
	}
	m.events = append(m.events, ev)
	m.mu.Unlock()
	select {
	case m.eventsCh <- ev:
	default:
	}
	return nil
}

// MarkReader records supervision state the way the real store does: on the reader row itself, so a
// test can assert that a reader the bus declared offline stops claiming to be fine.
func (m *memStore) MarkReader(_ context.Context, readerId int64, tamperState string, lastSeenAt int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.readers {
		if r.Id != readerId {
			continue
		}
		if tamperState != "" {
			r.TamperState = tamperState
		}
		if lastSeenAt > 0 {
			r.LastSeenAt = lastSeenAt
		}
	}
	return nil
}

// awaitEvent waits for the next recorded access event.
func (m *memStore) awaitEvent(t *testing.T, within time.Duration) entities.AccessEvent {
	t.Helper()
	select {
	case ev := <-m.eventsCh:
		return ev
	case <-time.After(within):
		t.Fatal("no access event was recorded")
		return entities.AccessEvent{}
	}
}

// recordingActuator counts unlocks so a test can prove a door opened — or, more importantly, that
// it did not.
type recordingActuator struct {
	mu      sync.Mutex
	unlocks []int
	err     error
	inner   Actuator
}

func (a *recordingActuator) Unlock(ctx context.Context, d entities.Door, secs int, ev entities.AccessEvent) error {
	a.mu.Lock()
	if a.err != nil {
		err := a.err
		a.mu.Unlock()
		return err
	}
	a.unlocks = append(a.unlocks, secs)
	inner := a.inner
	a.mu.Unlock()
	if inner != nil {
		return inner.Unlock(ctx, d, secs, ev)
	}
	return nil
}
func (a *recordingActuator) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.unlocks)
}

type recordingAlarm struct {
	mu     sync.Mutex
	raised []string
	ch     chan string
}

func newAlarm() *recordingAlarm { return &recordingAlarm{ch: make(chan string, 32)} }
func (a *recordingAlarm) Raise(_ context.Context, kind string, _ entities.AccessEvent, _ string) {
	a.mu.Lock()
	a.raised = append(a.raised, kind)
	a.mu.Unlock()
	select {
	case a.ch <- kind:
	default:
	}
}
func (a *recordingAlarm) has(kind string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range a.raised {
		if k == kind {
			return true
		}
	}
	return false
}

// awaitAlarm waits for an alarm of this kind to be raised, and fails the test if it is not.
//
// It exists because the controller RECORDS a door event and THEN raises its alarm, back to back
// on whichever goroutine noticed — the tick loop, for held-open. A test that takes the recorded
// event off the store's channel as its cue has therefore only been told about the first of the
// two, and sampling has() at that instant is a race it loses whenever the machine is busy. It
// lost it on CI while passing on every developer's laptop.
//
// Waiting is not weakening the assertion: what the product promises is that both happen
// promptly, not that the alarm precedes the log. An alarm that is never raised still fails
// here, just after a bounded wait rather than immediately.
func (a *recordingAlarm) awaitAlarm(t *testing.T, kind string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if a.has(kind) {
			return
		}
		if time.Now().After(deadline) {
			a.mu.Lock()
			raised := append([]string(nil), a.raised...)
			a.mu.Unlock()
			t.Fatalf("no %s alarm was raised within %s (raised: %v)", kind, within, raised)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// rig is a controller wired to a real OSDP bus and a real simulated reader.
type rig struct {
	t     *testing.T
	store *memStore
	pd    *osdp.PD
	bus   *osdp.Bus
	act   *recordingActuator
	alarm *recordingAlarm
	ctrl  *Controller
	pdMu  sync.Mutex
}

const testPort = "tcp://sim"

func newRig(t *testing.T, cfg ControllerConfig) *rig {
	t.Helper()
	store := newStore()
	r := newRigWithStore(t, store, cfg)
	r.store = store
	r.seedDefaults()
	return r
}

// newRigWithStore builds the same rig over ANY Store, so the identical end-to-end scenario can be
// run against the in-memory fake and against real SQLite. That the two agree is the evidence that
// the offline replica and the live database really are one code path.
func newRigWithStore(t *testing.T, data Store, cfg ControllerConfig) *rig {
	t.Helper()

	pd := osdp.NewPD(1)
	cpEnd, pdEnd := net.Pipe()

	r := &rig{t: t, pd: pd, act: &recordingActuator{}, alarm: newAlarm()}

	// The PD side of the wire — the same shape the simulator binary uses.
	go func() {
		sc := bufio.NewScanner(pdEnd)
		sc.Buffer(make([]byte, 0, 4096), osdp.MaxFrameSize*4)
		sc.Split(osdp.ScanFrames)
		for sc.Scan() {
			req := append([]byte(nil), sc.Bytes()...)
			r.pdMu.Lock()
			out := pd.Handle(req)
			r.pdMu.Unlock()
			if out == nil {
				continue
			}
			if _, err := pdEnd.Write(out); err != nil {
				return
			}
		}
	}()

	r.bus = osdp.NewBus(cpEnd, osdp.Options{
		SlotInterval: 2 * time.Millisecond,
		ReplyTimeout: 40 * time.Millisecond,
		OfflineAfter: 3,
		// Fast enough that a test does not wait a second for a door contact. The production
		// default is a second; the driver caps status commands at a quarter of a reader's slots
		// however short this is, so shortening it here cannot starve card delivery.
		StatusInterval: 20 * time.Millisecond,
	})
	if err := r.bus.Add(1); err != nil {
		t.Fatalf("bus.Add: %v", err)
	}

	cfg.BusPort = testPort
	if cfg.Location == nil {
		cfg.Location = func() *time.Location { return kl }
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Date(2026, 8, 4, 10, 0, 0, 0, kl) }
	}
	r.act.inner = BusActuator{Bus: r.bus, Resolve: func(context.Context, entities.Door) (DoorStrike, error) {
		return DoorStrike{Address: 1, Output: 0}, nil
	}}
	r.ctrl = NewController(data, r.bus, r.act, r.alarm, testPIN, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	busDone, ctrlDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(busDone); _ = r.bus.Run(ctx) }()
	go func() { defer close(ctrlDone); _ = r.ctrl.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		cpEnd.Close()
		pdEnd.Close()
		<-busDone
		<-ctrlDone
	})

	return r
}

// seedDefaults plants one door, one reader and one holder granted 24/7, unless a test says
// otherwise. Only meaningful for the in-memory store; the SQLite path seeds through real inserts.
func (r *rig) seedDefaults() {
	r.store.doors[1] = &entities.Door{
		Id: 1, Name: "Front", Class: entities.ClassInterior, LockKind: entities.LockFailSecure,
		UnlockSeconds: 5, ExtendedUnlockSeconds: 15, HeldOpenSeconds: 30, Enabled: true,
		OfflinePolicy: entities.OfflineCached, AntiPassback: entities.APBOff,
		// A bound door contact, so forced and held-open are actually detectable. A door without
		// one can report that it energised the strike and nothing more.
		ContactDeviceKey: "relay-1/in-0",
	}
	r.store.readers[rkey(testPort, 1)] = &entities.Reader{
		Id: 1, Name: "Front-In", DoorId: 1, Direction: entities.DirectionIn,
		BusPort: testPort, OsdpAddress: 1, Enabled: true,
	}
	r.store.holders[20] = &entities.Holder{
		Id: 20, Ref: "E-001", Name: "Aisyah", Kind: entities.HolderStaff, Status: entities.HolderActive,
	}
	r.store.creds[ckey(entities.FormatWiegand26, 7, "1234")] = &entities.Credential{
		Id: 10, HolderId: 20, Kind: entities.CredCard, Format: entities.FormatWiegand26,
		FacilityCode: 7, CardNumber: "1234", Status: entities.CredActive,
	}
	r.store.grants[20] = []entities.Grant{{Id: 1, GroupId: 5, DoorId: 1, ScheduleId: 100}}
	r.store.schedules[100] = entities.Schedule{Id: 100, Name: "Always", Always: true}
}

// waitOnline blocks until the controller has seen the reader come up, so a badge is not presented
// into a reader the decision path still believes is offline.
func (r *rig) waitOnline() {
	r.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if r.bus.Status(1) == osdp.StatusOnline {
			// The controller learns from the same event, one hop behind the bus.
			time.Sleep(20 * time.Millisecond)
			return
		}
		select {
		case <-deadline:
			r.t.Fatal("the reader never came online")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// badge presents a card at the simulated reader.
func (r *rig) badge(facility, number int) {
	r.pdMu.Lock()
	r.pd.PresentCard(osdp.CardRead{Format: 1, BitCount: 26, Data: EncodeWiegand26(facility, number)})
	r.pdMu.Unlock()
}

// TestReaderContactRaisesForcedAlarm is the end-to-end version of the gap this bench found.
//
// The door state machine, the forced event, the audit reason, the alarm kind, the CRITICAL
// severity and four translations of "Door forced open" all existed and were unit-tested. Nothing
// called ContactChanged. The reader's own supervised input — the ordinary way a door contact is
// wired — was never read, so on any real installation the alarm could not fire. This drives it the
// way a door does: through the PD, the poll loop and the controller.
func TestReaderContactRaisesForcedAlarm(t *testing.T) {
	r := newRig(t, ControllerConfig{TickInterval: 20 * time.Millisecond})
	r.waitOnline()

	// No grant, no REX: the door simply opens.
	r.pdMu.Lock()
	r.pd.Inputs[0] = true
	r.pdMu.Unlock()

	r.alarm.awaitAlarm(t, AlarmDoorForced, 3*time.Second)
	ev := r.store.awaitEvent(t, 3*time.Second)
	if ev.Reason != entities.ReasonDoorForced {
		t.Errorf("access log reason = %q, want %q", ev.Reason, entities.ReasonDoorForced)
	}
	if ev.DoorId != 1 {
		t.Errorf("the forced event was not attributed to the door: %+v", ev)
	}
}

// TestReaderContactAfterGrantDoesNotAlarm is the other half, and the more important one. A forced
// alarm on every legitimate entry teaches a site to ignore the one alarm that means somebody is
// inside who should not be — which is worse than having no alarm at all.
func TestReaderContactAfterGrantDoesNotAlarm(t *testing.T) {
	r := newRig(t, ControllerConfig{TickInterval: 20 * time.Millisecond})
	r.waitOnline()

	r.badge(7, 1234)
	if ev := r.store.awaitEvent(t, 3*time.Second); ev.Decision != entities.DecisionGranted {
		t.Fatalf("the badge was not granted: %s/%s", ev.Decision, ev.Reason)
	}
	r.pdMu.Lock()
	r.pd.Inputs[0] = true
	r.pdMu.Unlock()

	// Long enough for several status polls to have carried the opening in.
	time.Sleep(300 * time.Millisecond)
	if r.alarm.has(AlarmDoorForced) {
		t.Error("a granted entry raised a forced-door alarm")
	}
	// "No forced alarm" is also what a contact that was never reported at all looks like, so the
	// absence above only means something once the opening is known to have arrived.
	m, err := r.ctrl.Machine(context.Background(), 1)
	if err != nil {
		t.Fatalf("door machine: %v", err)
	}
	if !m.ContactOpen() {
		t.Error("the opening never reached the door state machine, so the check above proved nothing")
	}
}

// TestReaderTamperMarksTheReaderRow covers the second half of an alarm: the reader LIST.
//
// `Reader.TamperState` was written once as `ok` when the reader was enrolled and never again, and
// `LastSeenAt` was never written at all — so an installer opening the readers screen saw "ok, last
// seen never" for a reader that was offline, alarmed and out of service. The alarm fired and the
// screen lied.
func TestReaderTamperMarksTheReaderRow(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()

	if got := r.store.readers[rkey(testPort, 1)]; got.LastSeenAt == 0 {
		t.Error("a reader that came online was never stamped as seen")
	}
	r.pdMu.Lock()
	r.pd.Faults.Tamper = true
	r.pdMu.Unlock()

	r.alarm.awaitAlarm(t, AlarmTamper, 3*time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.store.mu.Lock()
		state := r.store.readers[rkey(testPort, 1)].TamperState
		r.store.mu.Unlock()
		if state == entities.TamperTripped {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reader row tamperState = %q, want %q", state, entities.TamperTripped)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestEndToEndBadgeOpensDoor is the P1 milestone: a badge on a real reader, through the real driver
// and the real decision path, fires a real strike — with no hardware anywhere.
func TestEndToEndBadgeOpensDoor(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Decision != entities.DecisionGranted {
		t.Fatalf("decision = %s (%s: %s)", ev.Decision, ev.Reason, ev.Detail)
	}
	if ev.HolderName != "Aisyah" {
		t.Errorf("holder name = %q — the log must read correctly without a join", ev.HolderName)
	}
	if ev.HolderId != 20 || ev.CredentialId != 10 || ev.DoorId != 1 || ev.ReaderId != 1 {
		t.Errorf("event did not carry the resolved ids: %+v", ev)
	}
	if r.act.count() != 1 {
		t.Errorf("strike fired %d times, want 1", r.act.count())
	}

	// And the strike physically moved on the simulated reader.
	deadline := time.After(2 * time.Second)
	for {
		r.pdMu.Lock()
		on := r.pd.Outputs[0]
		r.pdMu.Unlock()
		if on {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the reader's strike output never energised")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestEndToEndUnknownCardIsDeniedAndLogged guards the most valuable row in the log.
func TestEndToEndUnknownCardIsDeniedAndLogged(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()

	r.badge(7, 9999) // no such credential
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Decision != entities.DecisionDenied || ev.Reason != entities.ReasonUnknownCredential {
		t.Fatalf("decision = %s reason = %s", ev.Decision, ev.Reason)
	}
	if ev.RawCredential == "" {
		t.Error("the raw credential was not retained — there would be nothing to enrol or investigate")
	}
	if r.act.count() != 0 {
		t.Error("an unknown card fired the strike")
	}
}

// TestEndToEndLockdownDeniesEveryone covers the override that sits above every grant.
func TestEndToEndLockdownDeniesEveryone(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()
	r.ctrl.SetLockdown(context.Background(), true)

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Reason != entities.ReasonLockdown {
		t.Fatalf("reason = %s, want lockdown", ev.Reason)
	}
	if r.act.count() != 0 {
		t.Error("lockdown did not stop the strike")
	}

	// Lifting it restores service without a restart.
	r.ctrl.SetLockdown(context.Background(), false)
	r.badge(7, 1234)
	if ev := r.store.awaitEvent(t, 3*time.Second); ev.Decision != entities.DecisionGranted {
		t.Errorf("after lockdown lifted: %s (%s)", ev.Decision, ev.Reason)
	}
}

// TestEndToEndSecureChannelRequiredFailsClosed is the security-critical path, end to end. The
// reader here is online and answering perfectly — it simply has no encrypted session — and a door
// that requires one must not open.
func TestEndToEndSecureChannelRequiredFailsClosed(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.store.doors[1].RequireSecureChannel = true
	r.waitOnline()

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Decision != entities.DecisionDenied || ev.Reason != entities.ReasonSecureChannel {
		t.Fatalf("decision = %s reason = %s — a door requiring encryption opened without it",
			ev.Decision, ev.Reason)
	}
	if r.act.count() != 0 {
		t.Error("the strike fired on a door whose Secure Channel requirement was unmet")
	}
}

// TestEndToEndDuressOpensAndAlarms proves the door behaves identically while the alarm goes out.
func TestEndToEndDuressOpensAndAlarms(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	cred := r.store.creds[ckey(entities.FormatWiegand26, 7, "1234")]
	cred.PinHash = "1234"
	cred.DuressPinHash = "1235"
	r.waitOnline()

	// PIN first, then badge — the pairing the controller implements.
	r.pdMu.Lock()
	r.pd.PresentKeypad(0, []byte("1235"))
	r.pdMu.Unlock()
	waitFor(t, 2*time.Second, func() bool { return r.ctrl.pendingPIN(1) != "" })

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	// The door opens, exactly as it would on the normal PIN. Anything a coercer standing at the
	// reader could observe defeats the feature.
	if ev.Decision != entities.DecisionGranted {
		t.Fatalf("duress did not open the door: %s (%s)", ev.Reason, ev.Detail)
	}
	if !ev.Duress {
		t.Error("the event was not flagged as duress")
	}
	if r.act.count() != 1 {
		t.Errorf("strike fired %d times under duress, want 1", r.act.count())
	}
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("no duress alarm was raised")
	case <-waitAlarm(r.alarm, AlarmDuress):
	}
}

// TestPINIsConsumedOnUse guards a queue hazard: a PIN offered to one card must never be available
// to the next. Otherwise the person behind you badges and opens the door on YOUR PIN — and if it
// was a duress PIN, raises an alarm against them.
func TestPINIsConsumedOnUse(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	cred := r.store.creds[ckey(entities.FormatWiegand26, 7, "1234")]
	cred.PinHash = "1234"
	r.waitOnline()

	r.pdMu.Lock()
	r.pd.PresentKeypad(0, []byte("1234"))
	r.pdMu.Unlock()
	waitFor(t, 2*time.Second, func() bool { return r.ctrl.pendingPIN(1) != "" })

	r.badge(7, 1234)
	if ev := r.store.awaitEvent(t, 3*time.Second); ev.Decision != entities.DecisionGranted {
		t.Fatalf("first badge with PIN: %s (%s)", ev.Reason, ev.Detail)
	}

	// A second badge, no new PIN: must be denied, not served from the previous entry.
	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)
	if ev.Decision == entities.DecisionGranted {
		t.Fatal("a second badge reused the previous holder's PIN")
	}
	if ev.Reason != entities.ReasonBadPin {
		t.Errorf("reason = %s, want bad-pin", ev.Reason)
	}
}

// TestExpiredPINIsNotUsed covers the window. A PIN typed and abandoned must not sit waiting for
// whoever badges next.
func TestExpiredPINIsNotUsed(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, kl)
	clock := now
	r := newRig(t, ControllerConfig{
		PINWindow: time.Second,
		Now:       func() time.Time { return clock },
	})
	cred := r.store.creds[ckey(entities.FormatWiegand26, 7, "1234")]
	cred.PinHash = "1234"
	r.waitOnline()

	r.pdMu.Lock()
	r.pd.PresentKeypad(0, []byte("1234"))
	r.pdMu.Unlock()
	waitFor(t, 2*time.Second, func() bool { return r.ctrl.pendingPIN(1) != "" })

	clock = now.Add(30 * time.Second) // the entry is long stale
	r.badge(7, 1234)

	ev := r.store.awaitEvent(t, 3*time.Second)
	if ev.Decision == entities.DecisionGranted {
		t.Fatal("a stale PIN opened the door")
	}
	if ev.Reason != entities.ReasonBadPin {
		t.Errorf("reason = %s, want bad-pin", ev.Reason)
	}
}

// clock is a movable test clock, so held-open and relock timers can be exercised without sleeping
// for their real durations.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

// TestEndToEndDoorForcedRaisesAlarm covers the alarm that justifies binding a contact at all: the
// door opened and nobody was granted.
func TestEndToEndDoorForcedRaisesAlarm(t *testing.T) {
	r := newRig(t, ControllerConfig{TickInterval: 5 * time.Millisecond})
	r.waitOnline()

	ctx := context.Background()
	r.ctrl.ContactChanged(ctx, 1, true)

	ev := r.store.awaitEvent(t, 3*time.Second)
	if ev.Reason != entities.ReasonDoorForced {
		t.Fatalf("reason = %s, want door-forced", ev.Reason)
	}
	if ev.DoorId != 1 {
		t.Errorf("event door = %d", ev.DoorId)
	}
	r.alarm.awaitAlarm(t, AlarmDoorForced, 3*time.Second)
}

// TestEndToEndBadgeThenOpenDoesNotAlarm is the other half: walking through your own grant must not
// trip the forced alarm. An alarm that fires on every legitimate entry is one nobody reads.
func TestEndToEndBadgeThenOpenDoesNotAlarm(t *testing.T) {
	r := newRig(t, ControllerConfig{TickInterval: 5 * time.Millisecond})
	r.waitOnline()

	r.badge(7, 1234)
	if ev := r.store.awaitEvent(t, 3*time.Second); ev.Decision != entities.DecisionGranted {
		t.Fatalf("badge denied: %s", ev.Reason)
	}

	r.ctrl.ContactChanged(context.Background(), 1, true)

	select {
	case ev := <-r.store.eventsCh:
		if ev.Reason == entities.ReasonDoorForced {
			t.Fatal("walking through a granted door raised a forced alarm")
		}
	case <-time.After(300 * time.Millisecond):
	}
	if r.alarm.has(AlarmDoorForced) {
		t.Error("a forced alarm was raised for a legitimate entry")
	}
}

// TestEndToEndHeldOpenRaisesAlarm covers the door propped after a valid entry — the commonest way a
// secure door stops being secure.
func TestEndToEndHeldOpenRaisesAlarm(t *testing.T) {
	c := &clock{at: time.Date(2026, 8, 4, 10, 0, 0, 0, kl)}
	r := newRig(t, ControllerConfig{TickInterval: 5 * time.Millisecond, Now: c.now})
	r.waitOnline()

	r.badge(7, 1234)
	if ev := r.store.awaitEvent(t, 3*time.Second); ev.Decision != entities.DecisionGranted {
		t.Fatalf("badge denied: %s", ev.Reason)
	}

	ctx := context.Background()
	r.ctrl.ContactChanged(ctx, 1, true)

	// Push the clock past the 30s held-open threshold and let the tick loop notice.
	c.advance(45 * time.Second)

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-r.store.eventsCh:
			if ev.Reason == entities.ReasonDoorHeldOpen {
				r.alarm.awaitAlarm(t, AlarmDoorHeldOpen, 3*time.Second)
				return
			}
		case <-deadline:
			t.Fatal("a door propped past its threshold never raised held-open")
		}
	}
}

// TestEndToEndLockdownSealsAnOpenDoor covers the case where lockdown would otherwise mean nothing:
// a door already standing open on a free-access schedule.
func TestEndToEndLockdownSealsAnOpenDoor(t *testing.T) {
	r := newRig(t, ControllerConfig{TickInterval: 5 * time.Millisecond})
	r.waitOnline()
	ctx := context.Background()

	r.ctrl.SetFreeAccess(ctx, 1, true) // interior door: opens immediately
	m, err := r.ctrl.Machine(ctx, 1)
	if err != nil {
		t.Fatalf("Machine: %v", err)
	}
	if m.State() != DoorUnlocked {
		t.Fatal("setup: free access should have unlocked the door")
	}

	r.ctrl.SetLockdown(ctx, true)
	if m.State() != DoorLocked {
		t.Error("lockdown left a free-access door standing open")
	}
	if m.FreeAccess() {
		t.Error("free access survived lockdown")
	}
}

func waitAlarm(a *recordingAlarm, kind string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for k := range a.ch {
			if k == kind {
				close(done)
				return
			}
		}
	}()
	return done
}

func waitFor(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.After(within)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition not met in time")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// TestEndToEndReaderOfflineAlarms covers supervision reaching the application: every door bound to
// a dead reader is now unusable, and nobody learns that from a dashboard nobody is watching.
func TestEndToEndReaderOfflineAlarms(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()

	r.pdMu.Lock()
	r.pd.Faults.Silent = true
	r.pdMu.Unlock()

	select {
	case kind := <-r.alarm.ch:
		if kind != AlarmReaderOffline {
			t.Errorf("alarm = %s, want reader-offline", kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a reader going silent raised no alarm")
	}
}

// TestEndToEndFailedUnlockIsNotLoggedAsGranted is an honesty check on the audit trail. If the
// strike command fails, the log must not claim the door opened — an access log that records
// openings that never happened is worse than no log.
func TestEndToEndFailedUnlockIsNotLoggedAsGranted(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()

	r.act.mu.Lock()
	r.act.err = context.DeadlineExceeded
	r.act.mu.Unlock()

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Decision == entities.DecisionGranted {
		t.Error("a failed unlock was logged as a grant")
	}
	if ev.Detail == "" {
		t.Error("the failure reason was not recorded")
	}
}

// TestEndToEndOfflineCacheExpiry covers the offline policy through the whole stack.
func TestEndToEndOfflineCacheExpiry(t *testing.T) {
	r := newRig(t, ControllerConfig{
		Offline:  func() bool { return true },
		CacheAge: func() time.Duration { return 100 * time.Hour }, // past the interior 72h TTL
	})
	r.waitOnline()

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Reason != entities.ReasonOfflineCacheStale {
		t.Fatalf("reason = %s, want offline-cache-expired", ev.Reason)
	}
	if !ev.Offline {
		t.Error("the event did not record that it was served from cache")
	}
	if r.act.count() != 0 {
		t.Error("a stale cache still opened the door")
	}
}

// TestRecordPublishesDecisionsButNotDoorStateRows pins the boundary of the decision feed: every
// row with a presented credential (a badge, an operator unlock) is published, a door-state audit
// row (forced, held-open — no credential) is not, and an audit-write failure does not suppress the
// publish — the correlator must still see the decision that happened.
func TestRecordPublishesDecisionsButNotDoorStateRows(t *testing.T) {
	store := newStore()
	var published []entities.AccessEvent
	c := NewController(store, nil, nil, nil, nil, ControllerConfig{
		Decisions: func(_ context.Context, ev entities.AccessEvent) { published = append(published, ev) },
	})
	ctx := context.Background()

	c.record(ctx, entities.AccessEvent{
		RawCredential: "83826900", Decision: entities.DecisionGranted, Reason: entities.ReasonOK,
	})
	c.record(ctx, entities.AccessEvent{
		Decision: entities.DecisionDenied, Reason: entities.ReasonDoorForced,
	})
	if len(published) != 1 || published[0].Reason != entities.ReasonOK {
		t.Fatalf("published %d decisions (%+v), want only the badge grant", len(published), published)
	}

	store.mu.Lock()
	store.recErr = context.DeadlineExceeded
	store.mu.Unlock()
	c.record(ctx, entities.AccessEvent{
		RawCredential: "operator", Decision: entities.DecisionGranted, Reason: entities.ReasonOK,
	})
	if len(published) != 2 {
		t.Fatal("an audit-write failure suppressed the decision publish")
	}
}

// TestEndToEndBadgeOnUnenrolledReaderIsLogged covers a device on the bus that nobody enrolled —
// worth someone's attention rather than a silent drop.
func TestEndToEndBadgeOnUnenrolledReaderIsLogged(t *testing.T) {
	r := newRig(t, ControllerConfig{})
	r.waitOnline()

	r.store.mu.Lock()
	delete(r.store.readers, rkey(testPort, 1))
	r.store.mu.Unlock()

	r.badge(7, 1234)
	ev := r.store.awaitEvent(t, 3*time.Second)

	if ev.Decision != entities.DecisionDenied {
		t.Errorf("decision = %s", ev.Decision)
	}
	if ev.Detail == "" {
		t.Error("no detail recorded for a badge on an unenrolled reader")
	}
	if r.act.count() != 0 {
		t.Error("a badge on an unenrolled reader fired a strike")
	}
}
