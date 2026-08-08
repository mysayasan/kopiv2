package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/infra/access/osdp"
)

// Controller joins the OSDP bus to the decision path: a badge arrives on a reader, a decision is
// made, and — if it is a grant — a strike fires. It is the top of the P1 runtime.

// Store is everything the controller reads. It is an interface so the SAME controller runs against
// the live database and against an offline cached replica; "works offline" is then a property of
// which Store is plugged in, not a second code path that can drift from the first.
type Store interface {
	ReaderByBus(ctx context.Context, busPort string, address int) (*entities.Reader, error)
	Door(ctx context.Context, id int64) (*entities.Door, error)
	CredentialByCard(ctx context.Context, format string, facility int, number string) (*entities.Credential, error)
	Holder(ctx context.Context, id int64) (*entities.Holder, error)
	// GrantsFor returns the grants reaching this holder at this door — the caller does the
	// group join, so the decision path never needs a group index.
	GrantsFor(ctx context.Context, holderId, doorId int64) ([]entities.Grant, error)
	Schedules(ctx context.Context, ids []int64) (map[int64]entities.Schedule, map[int64][]entities.ScheduleWindow, error)
	HolidayOn(ctx context.Context, siteId int64, date string) (*entities.Holiday, error)
	RecordEvent(ctx context.Context, ev entities.AccessEvent) error
}

// Actuator opens a door. It is deliberately narrow.
//
// EVERY unlock — badge, plate, operator override, schedule, flow, API — must funnel through one
// audited implementation of this interface, exactly as myiotsan funnels all actuation through
// CommandService.Issue. That single path is the only reason "who opened door 3 at 02:14, and
// through what" is answerable. Do not add a second path, however convenient a direct relay call
// looks during development.
type Actuator interface {
	Unlock(ctx context.Context, door entities.Door, seconds int, ev entities.AccessEvent) error
}

// Alarmer raises alarms that are NOT access decisions: duress, tamper, a reader going out of
// service. Separate from the access log because these need to reach a human now, not be queryable
// later.
type Alarmer interface {
	Raise(ctx context.Context, kind string, ev entities.AccessEvent, detail string)
}

// Alarm kinds.
const (
	AlarmDuress        = "duress"
	AlarmTamper        = "tamper"
	AlarmReaderOffline = "reader-offline"
	AlarmSecureChannel = "secure-channel"
	AlarmDoorForced    = "door-forced"
	AlarmDoorHeldOpen  = "door-held-open"
)

// Controller options.
type ControllerConfig struct {
	BusPort  string
	Location *time.Location
	// Offline and CacheAge describe whether this controller is running on a cached replica. A
	// controller with a live database leaves them zero.
	Offline  bool
	CacheAge func() time.Duration
	// Now is injectable so schedule behaviour is testable without waiting for Tuesday.
	Now func() time.Time
	// TickInterval drives the door state machines' timers (relock, held-open). Default 1s.
	//
	// It bounds how LATE an alarm can be, not whether it fires: a held-open threshold of 30s with a
	// 1s tick alarms somewhere in 30–31s. Making it much coarser to save wakeups directly delays
	// every door alarm on the site.
	TickInterval time.Duration
	// PINWindow is how long a keypad entry stays available to be paired with a card. Default 15s.
	//
	// It must be SHORT. A PIN left buffered indefinitely would be picked up by whoever badges next,
	// which on a duress PIN means an alarm attributed to the wrong person, and on a normal one
	// means a door opening for a credential that never authenticated.
	PINWindow time.Duration
	// Decisions, when set, is told about every access DECISION — badge grants and denials, operator
	// unlocks — after it is recorded. It feeds the notification stream the fleet control plane
	// correlates across nodes; door-state audit rows (forced, held-open) are NOT decisions and do
	// not pass through here — they raise their own alarms. Nil-safe: standalone tests leave it unset.
	Decisions func(ctx context.Context, ev entities.AccessEvent)
}

// Controller drives one bus.
type Controller struct {
	store    Store
	bus      *osdp.Bus
	act      Actuator
	alarm    Alarmer
	verify   PINVerifier
	cfg      ControllerConfig
	mu       sync.RWMutex
	lockdown bool
	// readerState tracks what the bus has told us about each address, so a decision knows whether
	// the reader is online and whether its session is encrypted WITHOUT asking the bus mid-flight.
	readerState map[uint8]readerState
	// pins buffers a keypad entry until a card arrives at the same reader.
	pins map[uint8]bufferedPIN
	// machines is the door state machine per door.
	machines map[int64]*DoorMachine
}

type readerState struct {
	online bool
	secure bool
}

// bufferedPIN is a keypad entry waiting to be paired with a card.
//
// LIMITATION, stated rather than hidden: this implements PIN-THEN-CARD. A holder types their PIN
// and then badges. The commoner field convention is card-then-PIN, which needs the decision to be
// held open awaiting digits — a pending-transaction state this does not yet have. A card requiring
// a PIN with none buffered is denied with `bad-pin`, which is safe but is not the behaviour a site
// used to card-then-PIN will expect.
type bufferedPIN struct {
	digits string
	at     time.Time
}

func NewController(store Store, bus *osdp.Bus, act Actuator, alarm Alarmer, verify PINVerifier, cfg ControllerConfig) *Controller {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.PINWindow <= 0 {
		cfg.PINWindow = 15 * time.Second
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Second
	}
	return &Controller{
		store: store, bus: bus, act: act, alarm: alarm, verify: verify, cfg: cfg,
		readerState: map[uint8]readerState{},
		pins:        map[uint8]bufferedPIN{},
		machines:    map[int64]*DoorMachine{},
	}
}

// Machine returns the state machine for a door, creating it on first use.
func (c *Controller) Machine(ctx context.Context, doorId int64) (*DoorMachine, error) {
	c.mu.Lock()
	if m, ok := c.machines[doorId]; ok {
		c.mu.Unlock()
		return m, nil
	}
	c.mu.Unlock()

	door, err := c.store.Door(ctx, doorId)
	if err != nil || door == nil {
		if err == nil {
			err = fmt.Errorf("door %d not found", doorId)
		}
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check: another goroutine may have created it while the store was being read.
	if m, ok := c.machines[doorId]; ok {
		return m, nil
	}
	m := NewDoorMachine(*door)
	c.machines[doorId] = m
	return m, nil
}

// ContactChanged feeds a door-position report in from myiotsan.
//
// This is the seam that turns "we energised the strike" into "somebody actually went through", and
// with it door-forced and door-held-open detection. A door with no contact bound simply never has
// this called — which is a capability gap to surface, not to paper over.
func (c *Controller) ContactChanged(ctx context.Context, doorId int64, open bool) {
	m, err := c.Machine(ctx, doorId)
	if err != nil {
		return
	}
	c.emitDoorEvents(ctx, doorId, m.ContactChanged(c.cfg.Now(), open))
}

// RequestToExit feeds a REX press. It shunts the forced alarm and does not unlock anything.
func (c *Controller) RequestToExit(ctx context.Context, doorId int64) {
	if m, err := c.Machine(ctx, doorId); err == nil {
		c.emitDoorEvents(ctx, doorId, m.RequestToExit(c.cfg.Now()))
	}
}

// SetFreeAccess drives the scheduled-unlock overlay for a door.
func (c *Controller) SetFreeAccess(ctx context.Context, doorId int64, on bool) {
	if m, err := c.Machine(ctx, doorId); err == nil {
		c.emitDoorEvents(ctx, doorId, m.SetFreeAccessSchedule(c.cfg.Now(), on))
	}
}

// emitDoorEvents turns state-machine output into alarms and audit rows.
func (c *Controller) emitDoorEvents(ctx context.Context, doorId int64, evs []DoorEvent) {
	for _, e := range evs {
		switch e.Kind {
		case EvForced:
			ev := entities.AccessEvent{At: e.At.Unix(), DoorId: doorId,
				Decision: entities.DecisionDenied, Reason: entities.ReasonDoorForced, Detail: e.Detail}
			c.record(ctx, ev)
			if c.alarm != nil {
				c.alarm.Raise(ctx, AlarmDoorForced, ev, e.Detail)
			}
		case EvHeldOpen:
			ev := entities.AccessEvent{At: e.At.Unix(), DoorId: doorId,
				Decision: entities.DecisionDenied, Reason: entities.ReasonDoorHeldOpen, Detail: e.Detail}
			c.record(ctx, ev)
			if c.alarm != nil {
				c.alarm.Raise(ctx, AlarmDoorHeldOpen, ev, e.Detail)
			}
		}
	}
}

// SetLockdown overrides every grant and every schedule at every door on this bus.
//
// It does NOT override egress. Egress is hardware — a mechanical lever or a power-cut interlock —
// and cannot be overridden by software by design, because the failure mode of this app is a person
// trapped behind a door.
func (c *Controller) SetLockdown(ctx context.Context, on bool) {
	c.mu.Lock()
	c.lockdown = on
	ms := make(map[int64]*DoorMachine, len(c.machines))
	for id, m := range c.machines {
		ms[id] = m
	}
	c.mu.Unlock()

	// Every door already being tracked is sealed too. The decision path denies badges regardless,
	// but a door standing open on a free-access schedule would otherwise simply stay open — which
	// is the one case where "lockdown" would have meant nothing at all.
	now := c.cfg.Now()
	for id, m := range ms {
		c.emitDoorEvents(ctx, id, m.SetLockdown(now, on))
	}
}

// ErrUnknownDoor means this controller does not own the door.
var ErrUnknownDoor = errors.New("mypintusan: door is not on this bus")

// IsUnknownDoor reports whether an error means the door belongs to another bus, so a caller holding
// several controllers can try the next one.
func IsUnknownDoor(err error) bool { return errors.Is(err, ErrUnknownDoor) }

// OperatorUnlock opens a door from an operator action rather than a badge.
//
// It goes through the SAME actuator and writes the SAME access-event row as a badge, deliberately.
// A remote unlock is the most abusable capability in this application — it opens a door with no
// credential at all — so the one thing that must never happen is for it to bypass the audit trail.
// "Who opened door 3 at 02:14, and through what path" has to be answerable identically whether the
// answer is a card or a person clicking a button.
func (c *Controller) OperatorUnlock(ctx context.Context, door entities.Door, actor int64, actorName string) error {
	if !c.ownsDoor(ctx, door.Id) {
		return ErrUnknownDoor
	}

	c.mu.RLock()
	lockdown := c.lockdown
	c.mu.RUnlock()

	now := c.cfg.Now()
	ev := entities.AccessEvent{
		At: now.Unix(), DoorId: door.Id, HolderId: actor, HolderName: actorName,
		RawCredential: "operator", Decision: entities.DecisionDenied, Offline: c.cfg.Offline,
	}

	// Lockdown outranks an operator too. Someone who can lift lockdown can open the door; someone
	// who cannot must not be able to route around it with the unlock button.
	if lockdown {
		ev.Reason = entities.ReasonLockdown
		ev.Detail = "remote unlock refused: the site is in lockdown"
		c.record(ctx, ev)
		return fmt.Errorf("the site is in lockdown")
	}
	if !door.Enabled {
		ev.Reason = entities.ReasonDoorDisabled
		ev.Detail = "remote unlock refused: the door is disabled"
		c.record(ctx, ev)
		return fmt.Errorf("the door is disabled")
	}

	seconds := door.StrikeSeconds(false)
	ev.Reason = entities.ReasonOK
	ev.Decision = entities.DecisionGranted
	ev.Detail = "remote unlock by " + actorName

	if err := c.act.Unlock(ctx, door, seconds, ev); err != nil {
		ev.Decision = entities.DecisionDenied
		ev.Detail = "remote unlock failed: " + err.Error()
		c.record(ctx, ev)
		return err
	}
	if m, mErr := c.Machine(ctx, door.Id); mErr == nil {
		// Tell the state machine the door is legitimately open, or the person walking through the
		// unlock they were just given trips a forced-door alarm.
		c.emitDoorEvents(ctx, door.Id, m.Grant(now, seconds))
	}
	c.record(ctx, ev)
	return nil
}

// ownsDoor reports whether this controller is responsible for a door: either it already tracks it,
// or a reader on this bus is bound to it.
func (c *Controller) ownsDoor(ctx context.Context, doorId int64) bool {
	c.mu.RLock()
	_, tracked := c.machines[doorId]
	c.mu.RUnlock()
	if tracked {
		return true
	}
	d, err := c.store.Door(ctx, doorId)
	return err == nil && d != nil
}

// Lockdown reports the current state.
func (c *Controller) Lockdown() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lockdown
}

// Run drains the bus event channel until ctx is cancelled. It is the only consumer of that channel;
// the bus drops events if it is not drained, and a dropped event is a badge that never opened a door.
func (c *Controller) Run(ctx context.Context) error {
	tick := time.NewTicker(c.cfg.TickInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-c.bus.Events():
			if !ok {
				return nil
			}
			c.handle(ctx, ev)
		case <-tick.C:
			c.tickDoors(ctx)
		}
	}
}

// tickDoors advances every door's timers. It runs on the same goroutine as event handling, so a
// relock and a badge can never race each other into the same machine.
func (c *Controller) tickDoors(ctx context.Context) {
	now := c.cfg.Now()

	c.mu.Lock()
	ids := make([]int64, 0, len(c.machines))
	ms := make([]*DoorMachine, 0, len(c.machines))
	for id, m := range c.machines {
		ids = append(ids, id)
		ms = append(ms, m)
	}
	c.mu.Unlock()

	for i, m := range ms {
		if evs := m.Tick(now); len(evs) > 0 {
			c.emitDoorEvents(ctx, ids[i], evs)
		}
	}
}

func (c *Controller) handle(ctx context.Context, ev osdp.Event) {
	switch ev.Kind {
	case osdp.EventOnline:
		c.mu.Lock()
		c.readerState[ev.Address] = readerState{online: true, secure: ev.SecureSession}
		c.mu.Unlock()

	case osdp.EventOffline:
		c.mu.Lock()
		c.readerState[ev.Address] = readerState{}
		c.mu.Unlock()
		// A reader going out of service is an alarm, not a log line: every door bound to it is now
		// unusable, and nobody finds that out from a dashboard nobody is watching.
		c.raise(ctx, AlarmReaderOffline, ev.Address, ev.Reason)

	case osdp.EventStatus:
		if ev.Tamper {
			c.raise(ctx, AlarmTamper, ev.Address, "reader tamper asserted")
		}

	case osdp.EventFault:
		c.raise(ctx, AlarmSecureChannel, ev.Address, ev.Reason)

	case osdp.EventKeypad:
		c.mu.Lock()
		c.pins[ev.Address] = bufferedPIN{digits: string(ev.Digits), at: c.cfg.Now()}
		c.mu.Unlock()

	case osdp.EventCard:
		c.handleCard(ctx, ev)
	}
}

// pendingPIN reports the buffered entry for a reader WITHOUT consuming it. Test-facing: it lets a
// test wait for the keypad event to have landed before badging, rather than sleeping and hoping.
func (c *Controller) pendingPIN(addr uint8) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pins[addr].digits
}

// takePIN consumes any buffered keypad entry for a reader.
//
// It is consumed on read, not merely expired: a PIN that has been offered to one card must never be
// available to the next. Leaving it would mean the person behind you in the queue badges and opens
// the door on YOUR PIN — and if it was a duress PIN, raises an alarm against them.
func (c *Controller) takePIN(addr uint8, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pins[addr]
	if !ok {
		return ""
	}
	delete(c.pins, addr)
	if now.Sub(p.at) > c.cfg.PINWindow {
		return ""
	}
	return p.digits
}

// handleCard is the badge path: decode, resolve, decide, act, log.
func (c *Controller) handleCard(ctx context.Context, ev osdp.Event) {
	now := c.cfg.Now()
	card, decodeErr := DecodeCard(int(ev.Card.BitCount), ev.Card.Data)

	access := entities.AccessEvent{
		At:            now.Unix(),
		RawCredential: card.Raw,
		Decision:      entities.DecisionDenied,
		Offline:       c.cfg.Offline,
	}

	reader, err := c.store.ReaderByBus(ctx, c.cfg.BusPort, int(ev.Address))
	if err != nil || reader == nil {
		// A badge on a reader we have no record of. Logged rather than dropped: it means the bus
		// has a device on it that nobody enrolled, which is worth someone's attention.
		access.Reason = entities.ReasonReaderOffline
		access.Detail = fmt.Sprintf("no reader enrolled at %s address %d", c.cfg.BusPort, ev.Address)
		c.record(ctx, access)
		return
	}
	access.ReaderId = reader.Id
	access.DoorId = reader.DoorId

	door, err := c.store.Door(ctx, reader.DoorId)
	if err != nil || door == nil {
		access.Reason = entities.ReasonDoorDisabled
		access.Detail = "the reader is not bound to a door"
		c.record(ctx, access)
		return
	}

	// A decode failure is a DENIAL, never a guess. A card whose parity failed may be one bit away
	// from a valid credential belonging to somebody else.
	if decodeErr != nil {
		access.Reason = entities.ReasonUnknownCredential
		access.Detail = decodeErr.Error()
		c.record(ctx, access)
		return
	}

	snap, err := c.snapshot(ctx, now, *door, *reader, card)
	if err != nil {
		access.Reason = entities.ReasonUnknownCredential
		access.Detail = err.Error()
		c.record(ctx, access)
		return
	}

	decision := Decide(snap, c.verify)

	if snap.Credential != nil {
		access.CredentialId = snap.Credential.Id
	}
	if snap.Holder != nil {
		access.HolderId = snap.Holder.Id
		// Denormalised so the log still reads correctly years after the holder is deleted.
		access.HolderName = snap.Holder.Name
	}
	access.Reason = decision.Reason
	access.Detail = decision.Detail
	access.Duress = decision.Duress
	access.Offline = decision.Offline
	if decision.Granted {
		access.Decision = entities.DecisionGranted
	}

	if decision.Granted {
		// One chokepoint. Note the unlock happens BEFORE the log write returns, but the event is
		// fully built first, so an audit failure cannot leave a door opening unrecorded in memory.
		if err := c.act.Unlock(ctx, *door, decision.StrikeSeconds, access); err != nil {
			access.Decision = entities.DecisionDenied
			access.Detail = "unlock failed: " + err.Error()
		} else if m, mErr := c.Machine(ctx, door.Id); mErr == nil {
			// Tell the state machine the door is now legitimately open for a while. Without this
			// the person walking through their OWN grant trips a forced-door alarm.
			c.emitDoorEvents(ctx, door.Id, m.Grant(now, decision.StrikeSeconds))
		}
	}

	c.record(ctx, access)

	// The duress alarm goes out AFTER the door has opened and is deliberately silent: no LED, no
	// buzzer, nothing the person standing at the reader can observe. A coercer who can tell an
	// alarm was raised is a coercer who takes it out on the holder.
	if decision.Duress {
		c.alarm.Raise(ctx, AlarmDuress, access, "access granted under a duress PIN")
	}
}

// snapshot gathers everything the decision needs.
func (c *Controller) snapshot(ctx context.Context, now time.Time, door entities.Door, reader entities.Reader, card Card) (Snapshot, error) {
	c.mu.RLock()
	st := c.readerState[uint8(reader.OsdpAddress)]
	lockdown := c.lockdown
	c.mu.RUnlock()

	snap := Snapshot{
		Now:           now,
		Location:      c.cfg.Location,
		Door:          door,
		Reader:        reader,
		ReaderOnline:  st.online,
		ReaderSecure:  st.secure,
		Lockdown:      lockdown,
		Offline:       c.cfg.Offline,
		RawCredential: card.Raw,
		PresentedPIN:  c.takePIN(uint8(reader.OsdpAddress), now),
	}
	if c.cfg.CacheAge != nil {
		snap.CacheAge = c.cfg.CacheAge()
	}

	format, facility, number := card.Key()
	cred, err := c.store.CredentialByCard(ctx, format, facility, number)
	if err != nil {
		return snap, err
	}
	if cred == nil {
		return snap, nil // unknown card; the decision path denies and the raw value is logged
	}
	snap.Credential = cred

	holder, err := c.store.Holder(ctx, cred.HolderId)
	if err != nil {
		return snap, err
	}
	snap.Holder = holder
	if holder == nil {
		return snap, nil
	}

	grants, err := c.store.GrantsFor(ctx, holder.Id, door.Id)
	if err != nil {
		return snap, err
	}
	snap.Grants = grants

	ids := make([]int64, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.ScheduleId)
	}
	scheds, windows, err := c.store.Schedules(ctx, ids)
	if err != nil {
		return snap, err
	}
	snap.Schedules, snap.Windows = scheds, windows

	// The holiday calendar is looked up in SITE-LOCAL time. A public holiday is a calendar day, not
	// an instant, and resolving it in UTC shifts it by the offset for anyone badging near midnight.
	date := now.In(c.cfg.Location).Format("2006-01-02")
	holiday, err := c.store.HolidayOn(ctx, door.SiteId, date)
	if err != nil {
		return snap, err
	}
	snap.Holiday = holiday

	return snap, nil
}

func (c *Controller) record(ctx context.Context, ev entities.AccessEvent) {
	if err := c.store.RecordEvent(ctx, ev); err != nil && c.alarm != nil {
		// Losing an access event is itself an incident: the log is the product. Surfacing it as an
		// alarm is the only way anyone finds out before an audit does.
		c.alarm.Raise(ctx, AlarmTamper, ev, "FAILED TO RECORD ACCESS EVENT: "+err.Error())
	}
	// Every row with a presented credential is a decision worth publishing — that includes operator
	// unlocks ("operator") but excludes door-state rows (forced/held-open have no RawCredential),
	// which already raise their own alarms. Published even when the audit write above failed: the
	// tamper alarm has surfaced that, and the correlator must still see the decision that happened.
	if c.cfg.Decisions != nil && ev.RawCredential != "" {
		c.cfg.Decisions(ctx, ev)
	}
}

func (c *Controller) raise(ctx context.Context, kind string, addr uint8, detail string) {
	if c.alarm == nil {
		return
	}
	ev := entities.AccessEvent{At: c.cfg.Now().Unix()}
	if r, err := c.store.ReaderByBus(ctx, c.cfg.BusPort, int(addr)); err == nil && r != nil {
		ev.ReaderId, ev.DoorId = r.Id, r.DoorId
	}
	c.alarm.Raise(ctx, kind, ev, detail)
}

// DoorStrike says which OSDP address and which output on it fire a door's strike.
//
// The two are DIFFERENT NUMBERS and conflating them is easy: Address is the reader's PD address on
// the RS-485 segment, Output is the relay channel on that device. An early version of this actuator
// used Door.RelayChannel as the PD address, which meant every unlock was addressed to a PD that did
// not exist — the decision granted, the audit row said `ok`, and the door never opened. It was
// caught only by booting the app and reading the access log.
type DoorStrike struct {
	Address uint8
	Output  byte
}

// StrikeResolver maps a door to the strike that opens it. It is a function rather than a table
// because the binding lives in the database and can change while the process is running.
type StrikeResolver func(ctx context.Context, door entities.Door) (DoorStrike, error)

// BusActuator drives the strike through the OSDP reader's own output.
//
// It is the DRIVER-level actuator, used when the strike is wired to the reader rather than to a
// myiotsan relay. Either way it is the single implementation of Actuator the controller calls, so
// the audit trail is complete regardless of which one a door uses.
type BusActuator struct {
	Bus     *osdp.Bus
	Resolve StrikeResolver
}

func (a BusActuator) Unlock(ctx context.Context, door entities.Door, seconds int, _ entities.AccessEvent) error {
	if a.Resolve == nil {
		return fmt.Errorf("no strike binding resolver configured")
	}
	strike, err := a.Resolve(ctx, door)
	if err != nil {
		return err
	}
	// A TIMED unlock, not a permanent one: if this process dies mid-unlock the reader re-locks on
	// its own timer rather than leaving the door open until somebody notices.
	return a.Bus.Output(ctx, strike.Address, strike.Output, true, time.Duration(seconds)*time.Second)
}
