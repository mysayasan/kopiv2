package services

import (
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
)

var t0 = time.Date(2026, 8, 4, 9, 0, 0, 0, kl)

func testDoor(class string) entities.Door {
	return entities.Door{
		Id: 1, Name: "Front", Class: class, LockKind: entities.LockFailSecure,
		UnlockSeconds: 5, HeldOpenSeconds: 30, Enabled: true,
		ContactDeviceKey: "relay-1/in-0", // a bound contact
	}
}

// kinds flattens events for assertion.
func kinds(evs []DoorEvent) []DoorEventKind {
	out := make([]DoorEventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func has(evs []DoorEvent, k DoorEventKind) bool {
	for _, e := range evs {
		if e.Kind == k {
			return true
		}
	}
	return false
}

// TestGrantUnlocksAndRelocksOnTimer covers the ordinary entry where nobody actually opens the door
// — a badge presented and then thought better of. The strike must drop by itself.
func TestGrantUnlocksAndRelocksOnTimer(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))

	if evs := m.Grant(t0, 5); !has(evs, EvUnlocked) {
		t.Fatalf("grant emitted %v", kinds(evs))
	}
	if m.State() != DoorUnlocked {
		t.Fatal("the strike did not energise")
	}

	if evs := m.Tick(t0.Add(4 * time.Second)); len(evs) != 0 {
		t.Errorf("relocked early: %v", kinds(evs))
	}
	evs := m.Tick(t0.Add(5 * time.Second))
	if !has(evs, EvRelocked) {
		t.Fatalf("did not relock on the timer: %v", kinds(evs))
	}
	if m.State() != DoorLocked {
		t.Error("state is not locked after relock")
	}
}

// TestRelockOnCloseBeatsTheTimer is the anti-tailgating behaviour: the strike drops the moment the
// door closes behind someone, so a second person cannot follow on the first person's unlock. It is
// also why a generous unlock time does not weaken the door.
func TestRelockOnCloseBeatsTheTimer(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))
	m.Grant(t0, 30) // deliberately long

	m.ContactChanged(t0.Add(time.Second), true)
	evs := m.ContactChanged(t0.Add(3*time.Second), false)

	if !has(evs, EvRelocked) {
		t.Fatalf("did not relock on close: %v", kinds(evs))
	}
	if m.State() != DoorLocked {
		t.Error("still unlocked after the door closed")
	}
}

// TestLegitimateOpeningDoesNotAlarm is the shunt window. Without it every valid entry would raise a
// forced-door alarm, and an alarm that fires on every entry is an alarm nobody reads.
func TestLegitimateOpeningDoesNotAlarm(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))
	m.Grant(t0, 5)

	if evs := m.ContactChanged(t0.Add(2*time.Second), true); len(evs) != 0 {
		t.Errorf("a granted entry alarmed: %v", kinds(evs))
	}
	if forced, _ := m.InAlarm(); forced {
		t.Error("forced alarm active after a legitimate entry")
	}
}

// TestOpeningAfterTheShuntExpiresIsForced covers the other side of the window: a grant does not
// license the door being opened ten minutes later.
func TestOpeningAfterTheShuntExpiresIsForced(t *testing.T) {
	d := testDoor(entities.ClassPerimeter)
	m := NewDoorMachine(d)
	m.Grant(t0, 5)
	m.Tick(t0.Add(6 * time.Second)) // relocks

	late := t0.Add(time.Duration(m.ShuntSeconds+1) * time.Second)
	evs := m.ContactChanged(late, true)
	if !has(evs, EvForced) {
		t.Fatalf("an opening long after the grant did not alarm: %v", kinds(evs))
	}
}

// TestDoorForced is the alarm that justifies binding a contact at all.
func TestDoorForced(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))

	evs := m.ContactChanged(t0, true)
	if !has(evs, EvForced) {
		t.Fatalf("a door opening with no grant did not alarm: %v", kinds(evs))
	}
	if forced, _ := m.InAlarm(); !forced {
		t.Error("forced alarm not active")
	}

	// Closing clears it.
	evs = m.ContactChanged(t0.Add(10*time.Second), false)
	if !has(evs, EvClosed) {
		t.Fatalf("close emitted %v", kinds(evs))
	}
	if forced, _ := m.InAlarm(); forced {
		t.Error("forced alarm survived the door closing")
	}
}

// TestRequestToExitShuntsButDoesNotUnlock is the life-safety rule in miniature.
//
// On a fail-secure door egress is the inside lever retracting the latch mechanically. REX exists to
// stop that being treated as a break-in — nothing more. Making it drive the strike would put egress
// on the software path, which is exactly what a Go panic must not be able to interrupt.
func TestRequestToExitShuntsButDoesNotUnlock(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))

	m.RequestToExit(t0)
	if m.State() != DoorLocked {
		t.Fatal("REX energised the strike — egress must not depend on software")
	}

	if evs := m.ContactChanged(t0.Add(2*time.Second), true); len(evs) != 0 {
		t.Errorf("someone leaving raised an alarm: %v", kinds(evs))
	}
	if forced, _ := m.InAlarm(); forced {
		t.Error("exiting raised a forced-door alarm")
	}
}

// TestHeldOpen covers the door propped after a perfectly legitimate entry — the commonest way a
// secure door stops being one.
func TestHeldOpen(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))
	m.Grant(t0, 5)
	m.ContactChanged(t0.Add(time.Second), true)

	if evs := m.Tick(t0.Add(20 * time.Second)); has(evs, EvHeldOpen) {
		t.Error("held-open fired before the threshold")
	}
	evs := m.Tick(t0.Add(31 * time.Second)) // opened at +1s, threshold 30s
	if !has(evs, EvHeldOpen) {
		t.Fatalf("held-open did not fire: %v", kinds(evs))
	}
	if _, held := m.InAlarm(); !held {
		t.Error("held alarm not active")
	}

	// It fires ONCE, not on every tick — an alarm repeating every second buries everything else.
	if evs := m.Tick(t0.Add(40 * time.Second)); has(evs, EvHeldOpen) {
		t.Error("held-open re-fired on a later tick")
	}

	if evs := m.ContactChanged(t0.Add(60*time.Second), false); !has(evs, EvClosed) {
		t.Fatalf("close emitted %v", kinds(evs))
	}
	if _, held := m.InAlarm(); held {
		t.Error("held alarm survived the door closing")
	}
}

// TestForcedDoorAlsoGoesHeldOpen: a door forced and then left standing is both, and the second
// alarm must still fire — it is what distinguishes a brief break-in from an open building.
func TestForcedDoorAlsoGoesHeldOpen(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))
	m.ContactChanged(t0, true) // forced

	evs := m.Tick(t0.Add(31 * time.Second))
	if !has(evs, EvHeldOpen) {
		t.Fatalf("a forced door left open did not raise held-open: %v", kinds(evs))
	}
}

// TestNoContactMeansNoDetection is the capability gap. A door with no bound contact can report that
// it energised the strike and nothing more; the machine must say so rather than quietly behave as
// though everything is monitored.
func TestNoContactMeansNoDetection(t *testing.T) {
	d := testDoor(entities.ClassInterior)
	d.ContactDeviceKey = "" // nothing wired
	m := NewDoorMachine(d)

	if m.ContactBound() {
		t.Fatal("a door with no contact key reported a bound contact")
	}

	// The strike still works; there is simply nothing reporting door position.
	if evs := m.Grant(t0, 5); !has(evs, EvUnlocked) {
		t.Errorf("grant on an unmonitored door emitted %v", kinds(evs))
	}
	if evs := m.Tick(t0.Add(31 * time.Second)); has(evs, EvHeldOpen) {
		t.Error("held-open fired on a door with no contact to report it")
	}
}

// TestFreeAccessFirstPersonIn is the gate that stops a public holiday nobody entered in the
// calendar leaving the front door standing open all day.
func TestFreeAccessFirstPersonIn(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))

	// The schedule opens, but the building is empty. The door must stay locked.
	if evs := m.SetFreeAccessSchedule(t0, true); len(evs) != 0 {
		t.Errorf("a perimeter door unlocked with nobody inside: %v", kinds(evs))
	}
	if m.State() != DoorLocked || m.FreeAccess() {
		t.Fatal("free access engaged before the first person arrived")
	}

	// One valid holder badges in; NOW the door may stand open.
	evs := m.Grant(t0.Add(time.Hour), 5)
	if !has(evs, EvFreeAccessBegan) {
		t.Fatalf("the first person did not promote free access: %v", kinds(evs))
	}
	if !m.FreeAccess() {
		t.Error("free access not active after the first person in")
	}

	// And it stays unlocked through the relock timer.
	if evs := m.Tick(t0.Add(time.Hour + time.Minute)); has(evs, EvRelocked) {
		t.Error("free access relocked on the unlock timer")
	}
	if m.State() != DoorUnlocked {
		t.Error("free access did not hold the door unlocked")
	}
}

// TestFreeAccessInteriorDoesNotWait: the first-person gate is a PERIMETER protection. An interior
// office door standing open during office hours is the point of the schedule.
func TestFreeAccessInteriorDoesNotWait(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))

	evs := m.SetFreeAccessSchedule(t0, true)
	if !has(evs, EvFreeAccessBegan) || !has(evs, EvUnlocked) {
		t.Fatalf("an interior door did not open on schedule: %v", kinds(evs))
	}
	if m.State() != DoorUnlocked {
		t.Error("interior free access did not unlock")
	}
}

// TestFreeAccessEndsAndRearms covers the close of the window, including that the next window
// requires a FRESH first person — yesterday's arrival does not open the building today.
func TestFreeAccessEndsAndRearms(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))
	m.SetFreeAccessSchedule(t0, true)
	m.Grant(t0.Add(time.Hour), 5) // promotes

	evs := m.SetFreeAccessSchedule(t0.Add(9*time.Hour), false)
	if !has(evs, EvFreeAccessEnded) || !has(evs, EvRelocked) {
		t.Fatalf("end of window emitted %v", kinds(evs))
	}
	if m.State() != DoorLocked || m.FreeAccess() {
		t.Fatal("the door did not relock at the end of free access")
	}

	// Next day's window: locked again until somebody arrives.
	if evs := m.SetFreeAccessSchedule(t0.Add(24*time.Hour), true); len(evs) != 0 {
		t.Errorf("the next window opened without a fresh first person: %v", kinds(evs))
	}
	if m.State() != DoorLocked {
		t.Error("yesterday's first person unlocked today's building")
	}
}

// TestFreeAccessOpeningDoesNotAlarm: a door standing open on a schedule is expected to be opened.
func TestFreeAccessOpeningDoesNotAlarm(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))
	m.SetFreeAccessSchedule(t0, true)

	if evs := m.ContactChanged(t0.Add(time.Minute), true); len(evs) != 0 {
		t.Errorf("an opening during free access alarmed: %v", kinds(evs))
	}
	if forced, _ := m.InAlarm(); forced {
		t.Error("free-access opening raised a forced alarm")
	}
}

// TestLockdownOverridesEverything covers the override that sits above grants and schedules alike.
func TestLockdownOverridesEverything(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))
	m.SetFreeAccessSchedule(t0, true)
	if m.State() != DoorUnlocked {
		t.Fatal("setup: free access should have unlocked the door")
	}

	evs := m.SetLockdown(t0.Add(time.Minute), true)
	if !has(evs, EvRelocked) || !has(evs, EvFreeAccessEnded) {
		t.Fatalf("lockdown emitted %v", kinds(evs))
	}
	if m.State() != DoorLocked || m.FreeAccess() {
		t.Fatal("lockdown did not seal the door")
	}

	// A grant during lockdown must do nothing. The decision path already denies, but this is the
	// second lock on the same door: no other caller can route around it.
	if evs := m.Grant(t0.Add(2*time.Minute), 5); len(evs) != 0 {
		t.Errorf("a grant opened a locked-down door: %v", kinds(evs))
	}
	if m.State() != DoorUnlocked && m.State() != DoorLocked {
		t.Fatal("unexpected state")
	}
	if m.State() == DoorUnlocked {
		t.Error("the strike fired during lockdown")
	}
}

// TestLockdownLiftDoesNotAutoOpen: coming out of lockdown must not fling a perimeter door open on
// the strength of a first-person-in from before the building was sealed.
func TestLockdownLiftDoesNotAutoOpen(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))
	m.SetFreeAccessSchedule(t0, true)
	m.Grant(t0.Add(time.Hour), 5) // promotes to free access
	m.SetLockdown(t0.Add(2*time.Hour), true)

	m.SetLockdown(t0.Add(3*time.Hour), false)
	if m.State() != DoorLocked {
		t.Error("lifting lockdown re-opened the door by itself")
	}
	if m.FreeAccess() {
		t.Error("free access resumed without a fresh first person")
	}
}

// TestLockdownStillAllowsEgress is a documentation test as much as a behavioural one.
//
// Nothing in this machine can prevent someone leaving, and that is the design: egress is the inside
// lever or a power-cut interlock, both hardware. A person opening the door from inside during
// lockdown produces an ALARM, never a locked door — because a Go panic, a lockdown, or a bug must
// not be able to trap somebody in a stairwell.
func TestLockdownStillAllowsEgress(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))
	m.SetLockdown(t0, true)

	// Somebody pushes the inside lever. The contact opens whatever software thinks.
	evs := m.ContactChanged(t0.Add(time.Minute), true)
	if !has(evs, EvForced) {
		t.Errorf("egress during lockdown should be reported, not silently ignored: %v", kinds(evs))
	}
	// The point: the machine reported it. It did not, and could not, prevent it.
}

// TestGrantSecondsFallBackToTheDoor covers the default when a caller passes nothing sensible.
func TestGrantSecondsFallBackToTheDoor(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassInterior))
	m.Grant(t0, 0)

	if evs := m.Tick(t0.Add(4 * time.Second)); has(evs, EvRelocked) {
		t.Error("relocked before the door's own unlock time")
	}
	if evs := m.Tick(t0.Add(5 * time.Second)); !has(evs, EvRelocked) {
		t.Error("did not relock at the door's unlock time")
	}
}

// TestContactChangeIsEdgeTriggered: a sensor reporting the same state repeatedly must not produce
// a stream of duplicate events. Contact hardware chatters.
func TestContactChangeIsEdgeTriggered(t *testing.T) {
	m := NewDoorMachine(testDoor(entities.ClassPerimeter))

	first := m.ContactChanged(t0, true)
	if !has(first, EvForced) {
		t.Fatal("setup: expected a forced alarm")
	}
	for i := range 5 {
		if evs := m.ContactChanged(t0.Add(time.Duration(i)*time.Second), true); len(evs) != 0 {
			t.Fatalf("repeat open report %d emitted %v", i, kinds(evs))
		}
	}
	if evs := m.ContactChanged(t0.Add(10*time.Second), false); !has(evs, EvClosed) {
		t.Errorf("close emitted %v", kinds(evs))
	}
	for i := range 3 {
		if evs := m.ContactChanged(t0.Add(time.Duration(20+i)*time.Second), false); len(evs) != 0 {
			t.Fatalf("repeat close report %d emitted %v", i, kinds(evs))
		}
	}
}
