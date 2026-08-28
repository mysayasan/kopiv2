package services

import (
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
)

// The door state machine: what happens to the physical door after a decision has been made.
//
// The decision path answers "may this person come in?". This answers the questions that follow and
// that nobody asks until something goes wrong — did the door actually open, is it still open, did
// it open when nobody was granted, and when should the strike drop.
//
// It is driven by an injected clock rather than a timer goroutine. A door whose behaviour depends
// on wall-clock timing is otherwise testable only by sleeping, which makes the awkward cases (a
// door propped at the exact held-open threshold, a REX pressed one second before the shunt expires)
// either slow or flaky, and usually both.

// DoorState is where the strike is.
type DoorState int

const (
	// DoorLocked means the strike is de-energised. For a fail-secure door — the reference kit
	// default — this is also the unpowered state, and the inside lever still opens it mechanically.
	DoorLocked DoorState = iota
	// DoorUnlocked means the strike is energised and the door may be pushed.
	DoorUnlocked
)

func (s DoorState) String() string {
	if s == DoorUnlocked {
		return "unlocked"
	}
	return "locked"
}

// DoorEventKind is something the machine wants the rest of the system to know about.
type DoorEventKind int

const (
	// EvUnlocked and EvRelocked bracket a strike firing.
	EvUnlocked DoorEventKind = iota
	EvRelocked
	// EvForced is the door opening with no grant and no REX — the alarm that justifies binding a
	// contact at all.
	EvForced
	// EvHeldOpen is a door propped past its threshold. Distinct from forced: somebody may have
	// entered legitimately and then wedged it, which is the commonest way a secure door stops
	// being one.
	EvHeldOpen
	// EvClosed clears whichever alarm was active.
	EvClosed
	EvFreeAccessBegan
	EvFreeAccessEnded
)

func (k DoorEventKind) String() string {
	switch k {
	case EvUnlocked:
		return "unlocked"
	case EvRelocked:
		return "relocked"
	case EvForced:
		return "door-forced"
	case EvHeldOpen:
		return "door-held-open"
	case EvClosed:
		return "closed"
	case EvFreeAccessBegan:
		return "free-access-began"
	case EvFreeAccessEnded:
		return "free-access-ended"
	}
	return "unknown"
}

// DoorEvent is one emitted transition.
type DoorEvent struct {
	Kind   DoorEventKind
	At     time.Time
	Detail string
}

// DoorMachine tracks one door.
type DoorMachine struct {
	door entities.Door

	// mu guards every mutable field below.
	//
	// A door machine is driven from several goroutines at once and always was: the OSDP bus
	// goroutine feeds ContactChanged and RequestToExit as the hardware reports them, the
	// decision path calls Grant, the schedule worker calls SetFreeAccessSchedule, and the
	// controller's Run loop calls Tick on its own timer. Controller.mu only ever protected the
	// MAP of machines — it is released before Tick runs — so the machine's own fields were
	// unsynchronized across all of those callers.
	//
	// The consequence is not theoretical for a door: shuntUntil is written by Grant/REX and read
	// by ContactChanged to decide whether an opening is expected. Losing that write raises a
	// forced-door alarm on a legitimate entry; losing the contactOpen write in the other
	// direction means a door that was forced never alarms at all.
	//
	// The lock lives here rather than in the Controller because the machine is the unit of
	// state, and a caller must not be able to hold half of it. Events are returned to the caller
	// AFTER the lock is dropped, so emitting them can never re-enter the machine under its own
	// lock.
	mu sync.Mutex

	state       DoorState
	contactOpen bool

	// unlockUntil is when a timed unlock expires.
	unlockUntil time.Time
	// shuntUntil suppresses the forced alarm. A grant or a REX press opens this window: the door is
	// EXPECTED to open, and without the window every legitimate entry would raise a forced alarm.
	shuntUntil time.Time
	// openedAt is when the contact last opened, for the held-open timer.
	openedAt time.Time

	forcedActive bool
	heldActive   bool
	// contactSeen records that a door-position report has actually arrived, whatever the door's
	// configuration says. It is what lets ContactBound answer for a contact on the reader's own
	// supervised input, which nothing configures.
	contactSeen bool

	lockdown bool
	// freeAccessScheduled is the schedule's opinion; freeAccessActive is whether the door is
	// actually standing open. They differ while a perimeter door waits for its first person.
	freeAccessScheduled bool
	freeAccessActive    bool
	firstPersonIn       bool

	// ShuntSeconds is how long after a grant or REX the door may open without alarming. It has to
	// cover a person actually reaching the door and pushing it.
	ShuntSeconds int
	// RelockOnClose drops the strike the moment the door closes behind someone, rather than waiting
	// out the full unlock timer. This is what stops a second person walking in on the tail of the
	// first, and it is why the strike time can be generous without weakening the door.
	RelockOnClose bool
}

// NewDoorMachine returns a machine for a door, locked and closed.
func NewDoorMachine(door entities.Door) *DoorMachine {
	return &DoorMachine{
		door:          door,
		state:         DoorLocked,
		ShuntSeconds:  max(door.UnlockSeconds, 5) + 10,
		RelockOnClose: true,
	}
}

// State reports where the strike is.
func (m *DoorMachine) State() DoorState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// ContactOpen reports the last known door position.
func (m *DoorMachine) ContactOpen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.contactOpen
}

// InAlarm reports whether a forced or held-open condition is currently active.
func (m *DoorMachine) InAlarm() (forced, held bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forcedActive, m.heldActive
}

// FreeAccess reports whether the door is currently standing unlocked on a schedule.
func (m *DoorMachine) FreeAccess() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.freeAccessActive
}

// ContactBound reports whether this door can detect that it was actually opened.
//
// A door with no contact can report that it ENERGISED THE STRIKE but not that anyone went through:
// no forced alarm, no held-open alarm, no evacuation roster. That is a capability gap to surface in
// the UI, not something to degrade silently — an operator who believes they have forced-door
// detection and does not is worse off than one who knows they do not.
//
// Two things can bind a contact and they are known at different times. A myiotsan contact is
// CONFIGURED, so the door's own row answers for it; a contact on the reader's supervised input is
// DISCOVERED, because the reader announces how many inputs it has and then reports them. Answering
// only from configuration would have told an operator they had no forced-door detection on every
// door whose contact is wired the ordinary way, straight to the reader.
func (m *DoorMachine) ContactBound() bool {
	if m.door.ContactDeviceKey != "" {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.contactSeen
}

// Grant unlocks the door after an access decision has granted. seconds comes from the decision, so
// a holder with the accessibility extension gets their longer time here without the machine
// needing to know why.
func (m *DoorMachine) Grant(now time.Time, seconds int) []DoorEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lockdown {
		// Defensive: the decision path already denies under lockdown. Refusing again here means no
		// other caller — an operator override, a flow, a future API — can route around it.
		return nil
	}
	if seconds <= 0 {
		seconds = m.door.StrikeSeconds(false)
	}
	m.state = DoorUnlocked
	m.unlockUntil = now.Add(time.Duration(seconds) * time.Second)
	m.shuntUntil = now.Add(time.Duration(m.ShuntSeconds) * time.Second)
	// The building now has somebody in it, which is what a perimeter free-access schedule waits for.
	m.firstPersonIn = true

	evs := []DoorEvent{{Kind: EvUnlocked, At: now, Detail: "access granted"}}
	if m.freeAccessScheduled && !m.freeAccessActive {
		m.freeAccessActive = true
		evs = append(evs, DoorEvent{Kind: EvFreeAccessBegan, At: now,
			Detail: "first person in — free access now active"})
	}
	return evs
}

// RequestToExit shunts the forced alarm for someone leaving.
//
// It does NOT unlock. On the fail-secure door the reference kit specifies, egress is the inside
// lever retracting the latch mechanically — no electricity involved — so the only thing software
// has to do is stop treating that opening as a break-in. Making REX drive the strike would put
// egress on the software path, which is exactly what the life-safety constraint forbids.
func (m *DoorMachine) RequestToExit(now time.Time) []DoorEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shuntUntil = now.Add(time.Duration(m.ShuntSeconds) * time.Second)
	return nil
}

// ContactChanged feeds the door-position sensor.
func (m *DoorMachine) ContactChanged(now time.Time, open bool) []DoorEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Set BEFORE the no-change return. The first report from a closed door says `false`, which
	// matches the initial state and returns early — so testing capability after the change check
	// would leave every quiet door looking as though it had no contact at all.
	m.contactSeen = true
	if open == m.contactOpen {
		return nil
	}
	m.contactOpen = open

	if open {
		m.openedAt = now
		// Expected if the door was unlocked, a grant or REX shunt is live, or it is standing open
		// on a free-access schedule. Anything else is a door being forced.
		expected := m.state == DoorUnlocked || now.Before(m.shuntUntil) || m.freeAccessActive
		if expected {
			return nil
		}
		m.forcedActive = true
		return []DoorEvent{{Kind: EvForced, At: now, Detail: "the door opened with no grant and no exit request"}}
	}

	// Closing clears whatever was wrong and, if configured, drops the strike immediately so a
	// second person cannot follow on the first person's unlock.
	evs := []DoorEvent{{Kind: EvClosed, At: now}}
	m.forcedActive, m.heldActive = false, false
	if m.RelockOnClose && m.state == DoorUnlocked && !m.freeAccessActive {
		m.state = DoorLocked
		m.unlockUntil = time.Time{}
		evs = append(evs, DoorEvent{Kind: EvRelocked, At: now, Detail: "relocked on close"})
	}
	return evs
}

// Tick advances the timers. Callers drive it on their own cadence; it is idempotent, so calling it
// more often than necessary costs nothing but calling it too rarely delays an alarm.
func (m *DoorMachine) Tick(now time.Time) []DoorEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	var evs []DoorEvent

	// Relock when the unlock timer expires — unless the door is standing open on a schedule.
	if m.state == DoorUnlocked && !m.freeAccessActive && !m.unlockUntil.IsZero() && !now.Before(m.unlockUntil) {
		m.state = DoorLocked
		m.unlockUntil = time.Time{}
		evs = append(evs, DoorEvent{Kind: EvRelocked, At: now, Detail: "unlock timer expired"})
	}

	// Held open. Only detectable with a bound contact, and deliberately independent of whether the
	// opening was legitimate: a door propped after a valid entry is the commonest way a secure door
	// stops being one.
	held := m.door.HeldOpenSeconds
	if held > 0 && m.contactOpen && !m.heldActive && !m.freeAccessActive {
		if !now.Before(m.openedAt.Add(time.Duration(held) * time.Second)) {
			m.heldActive = true
			evs = append(evs, DoorEvent{Kind: EvHeldOpen, At: now,
				Detail: "the door has been open past its threshold"})
		}
	}
	return evs
}

// SetFreeAccessSchedule tells the machine what the schedule wants.
//
// FIRST-PERSON-IN is the important part, and it applies to perimeter and critical doors: the
// schedule does NOT unlock the building until one valid holder has actually arrived. Without it, a
// public holiday nobody entered in the calendar leaves the front door standing open all day —
// which is the failure this gate exists to prevent, and it is not hypothetical on a site whose
// holidays vary by state.
func (m *DoorMachine) SetFreeAccessSchedule(now time.Time, on bool) []DoorEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on == m.freeAccessScheduled {
		return nil
	}
	m.freeAccessScheduled = on

	if !on {
		// Leaving the free-access window: relock, and require a fresh first person next time.
		m.firstPersonIn = false
		if m.freeAccessActive {
			m.freeAccessActive = false
			m.state = DoorLocked
			return []DoorEvent{
				{Kind: EvFreeAccessEnded, At: now},
				{Kind: EvRelocked, At: now, Detail: "free access ended"},
			}
		}
		return []DoorEvent{{Kind: EvFreeAccessEnded, At: now}}
	}

	if m.lockdown {
		return nil // lockdown outranks the schedule
	}
	if m.requiresFirstPersonIn() && !m.firstPersonIn {
		// Armed, waiting. The door stays locked and normal badging continues; the first grant
		// promotes it.
		return nil
	}
	m.freeAccessActive = true
	m.state = DoorUnlocked
	return []DoorEvent{
		{Kind: EvFreeAccessBegan, At: now},
		{Kind: EvUnlocked, At: now, Detail: "free access schedule"},
	}
}

// requiresFirstPersonIn reports whether this door class waits for a human before standing open.
func (m *DoorMachine) requiresFirstPersonIn() bool {
	return m.door.Class == entities.ClassPerimeter || m.door.Class == entities.ClassCritical
}

// SetLockdown overrides every grant and every schedule.
//
// It does NOT override egress. Egress is hardware — a mechanical lever, or a power-cut interlock
// wired in series on a maglock feed — and cannot be overridden by software by design, because the
// failure mode of this application is a person trapped behind a door. Lockdown stops people getting
// IN; nothing here can stop them getting out.
func (m *DoorMachine) SetLockdown(now time.Time, on bool) []DoorEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on == m.lockdown {
		return nil
	}
	m.lockdown = on
	if !on {
		// Coming out of lockdown does NOT restore free access on its own: a perimeter door has to
		// earn it again with a live first person, because the building was sealed and whoever was
		// inside may not be any more.
		m.firstPersonIn = false
		return nil
	}

	var evs []DoorEvent
	if m.freeAccessActive {
		m.freeAccessActive = false
		evs = append(evs, DoorEvent{Kind: EvFreeAccessEnded, At: now, Detail: "lockdown"})
	}
	if m.state == DoorUnlocked {
		m.state = DoorLocked
		m.unlockUntil = time.Time{}
		evs = append(evs, DoorEvent{Kind: EvRelocked, At: now, Detail: "lockdown"})
	}
	return evs
}
