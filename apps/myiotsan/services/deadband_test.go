package services

import "testing"

const (
	sec = int64(1000)
	min = 60 * sec
)

func TestDeadband_FirstSampleAlwaysPasses(t *testing.T) {
	g := NewDeadbandGate()
	if !g.Admit(1, "temperature", GateRule{Deadband: 0.5, Numeric: true}, 21.4, "", 0) {
		t.Fatal("the first sample of a series must be stored: there is nothing to compare it to")
	}
}

func TestDeadband_SuppressesNoiseBelowTheBand(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 0.5, Numeric: true}
	g.Admit(1, "temperature", rule, 21.4, "", 0)

	// A temperature probe jittering in the last decimal place. This is the case that would
	// otherwise write a row per second, per key, per device, forever.
	for i, v := range []float64{21.41, 21.38, 21.44, 21.40, 21.45} {
		if g.Admit(1, "temperature", rule, v, "", int64(i+1)*sec) {
			t.Fatalf("value %.2f moved less than the 0.5 deadband and must not be stored", v)
		}
	}
}

func TestDeadband_AdmitsARealMove(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 0.5, Numeric: true}
	g.Admit(1, "temperature", rule, 21.4, "", 0)

	if !g.Admit(1, "temperature", rule, 22.0, "", sec) {
		t.Fatal("a 0.6 move exceeds the 0.5 deadband and must be stored")
	}
	// And the baseline moved with it: 22.0 -> 22.3 is now only 0.3.
	if g.Admit(1, "temperature", rule, 22.3, "", 2*sec) {
		t.Fatal("the deadband must be measured against the LAST STORED value, not the original")
	}
}

// A drift that never exceeds the deadband in one step, but walks a long way overall, is
// intended to be suppressed: the deadband is a resolution, not a leash. This test pins that
// down so nobody "fixes" it later without meaning to.
func TestDeadband_SlowDriftIsSuppressedUntilItAccumulates(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 1.0, Numeric: true}
	g.Admit(1, "temperature", rule, 20.0, "", 0)

	stored := 0
	v := 20.0
	for i := 1; i <= 20; i++ { // +0.3 twenty times: 20.0 -> 26.0
		v += 0.3
		if g.Admit(1, "temperature", rule, v, "", int64(i)*sec) {
			stored++
		}
	}
	// Each admit re-baselines, so it stores roughly every 1.0 of travel: 6 degrees / 1.0.
	if stored < 4 || stored > 7 {
		t.Fatalf("a 6-degree drift with a 1.0 deadband should store ~6 rows, stored %d", stored)
	}
}

func TestDeadband_HeartbeatForcesAFlatLineThrough(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 0.5, HeartbeatSeconds: 300, Numeric: true}
	g.Admit(1, "temperature", rule, 21.4, "", 0)

	if g.Admit(1, "temperature", rule, 21.4, "", 4*min) {
		t.Fatal("an unchanged value before the heartbeat elapses must not be stored")
	}
	if !g.Admit(1, "temperature", rule, 21.4, "", 5*min) {
		t.Fatal("the heartbeat must force a row through: a flat line has to be distinguishable from a dead device")
	}
	// The heartbeat re-baselines the clock, so the next one is another 5 minutes away.
	if g.Admit(1, "temperature", rule, 21.4, "", 6*min) {
		t.Fatal("the heartbeat clock must restart from the last STORED row")
	}
}

// A door contact has no deadband: every transition is the event. But a device that
// republishes its unchanged state every second must still not write a row every second.
func TestDeadband_ZeroDeadbandStoresTransitionsNotRepeats(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 0, Numeric: true}

	if !g.Admit(2, "contact", rule, 0, "", 0) {
		t.Fatal("first sample must store")
	}
	if g.Admit(2, "contact", rule, 0, "", sec) {
		t.Fatal("an identical repeat must not be stored, even with no deadband")
	}
	if !g.Admit(2, "contact", rule, 1, "", 2*sec) {
		t.Fatal("the door OPENED — with no deadband every transition must be stored")
	}
	if !g.Admit(2, "contact", rule, 0, "", 3*sec) {
		t.Fatal("the door CLOSED — that is a transition too")
	}
}

func TestDeadband_StringKeysCompareByEquality(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Numeric: false}
	g.Admit(3, "state", rule, 0, "idle", 0)

	if g.Admit(3, "state", rule, 0, "idle", sec) {
		t.Fatal("an unchanged string must not be stored")
	}
	if !g.Admit(3, "state", rule, 0, "running", 2*sec) {
		t.Fatal("a changed string must be stored")
	}
}

// Series must not bleed across devices or keys — the bug that would make one noisy sensor
// suppress another sensor's readings.
func TestDeadband_SeriesAreIndependent(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 0.5, Numeric: true}

	g.Admit(1, "temperature", rule, 21.0, "", 0)
	if !g.Admit(2, "temperature", rule, 21.0, "", 0) {
		t.Fatal("a different DEVICE is a different series and its first sample must store")
	}
	if !g.Admit(1, "humidity", rule, 21.0, "", 0) {
		t.Fatal("a different KEY is a different series and its first sample must store")
	}
	if g.Size() != 3 {
		t.Fatalf("expected 3 independent series, got %d", g.Size())
	}
}

func TestDeadband_ForgetDropsADeletedDevice(t *testing.T) {
	g := NewDeadbandGate()
	rule := GateRule{Deadband: 0.5, Numeric: true}
	g.Admit(1, "temperature", rule, 21.0, "", 0)
	g.Admit(2, "temperature", rule, 21.0, "", 0)

	g.Forget(1)

	if g.Size() != 1 {
		t.Fatalf("Forget must drop only that device's series, %d left", g.Size())
	}
	// Device 1's baseline is gone, so its next sample is a first-sample again.
	if !g.Admit(1, "temperature", rule, 21.0, "", sec) {
		t.Fatal("after Forget, the device's next sample must be treated as first-seen")
	}
}
