package services

import (
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

func aboveRule() entities.IotRule {
	return entities.IotRule{
		Id: 1, Name: "Cold store too warm", Enabled: true,
		Key: "temperature", Condition: "above", Threshold: 5,
		Severity: "critical",
	}
}

func TestRule_FiresWhenTheThresholdIsCrossed(t *testing.T) {
	e := NewRuleEngine()
	d := e.Evaluate(aboveRule(), Observation{Value: 8.4, Now: 1000})
	if !d.Fire {
		t.Fatalf("8.4 is above 5 and must fire; suppressed=%q", d.Suppressed)
	}
	if d.Reason == "" {
		t.Fatal("a fired alert must carry the sentence explaining it")
	}
}

func TestRule_DoesNotFireBelowTheThreshold(t *testing.T) {
	e := NewRuleEngine()
	if d := e.Evaluate(aboveRule(), Observation{Value: 3.0, Now: 1000}); d.Fire {
		t.Fatal("3.0 is not above 5")
	}
}

// The bounce. One spike must not wake anybody.
func TestRule_DebounceRequiresConsecutiveSamples(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.ConsecutiveSamples = 3

	if d := e.Evaluate(r, Observation{Value: 8, Now: 1}); d.Fire {
		t.Fatal("1 of 3 samples must not fire")
	}
	if d := e.Evaluate(r, Observation{Value: 8, Now: 2}); d.Fire {
		t.Fatal("2 of 3 samples must not fire")
	}
	if d := e.Evaluate(r, Observation{Value: 8, Now: 3}); !d.Fire {
		t.Fatal("3 of 3 consecutive samples must fire")
	}
}

// The counter must RESET on a sample that clears — otherwise three separate spikes an hour
// apart would add up to an alert, which is precisely the nuisance the debounce exists to stop.
func TestRule_DebounceResetsOnAClearSample(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.ConsecutiveSamples = 3

	e.Evaluate(r, Observation{Value: 8, Now: 1})
	e.Evaluate(r, Observation{Value: 8, Now: 2})
	e.Evaluate(r, Observation{Value: 2, Now: 3}) // clears
	if d := e.Evaluate(r, Observation{Value: 8, Now: 4}); d.Fire {
		t.Fatal("the run was broken by a clear sample; the debounce must start counting again")
	}
}

// The flapping alert: a value hovering on the threshold.
func TestRule_HysteresisStopsFlapping(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.Hysteresis = 1.0 // must fall to 4.0 before re-arming

	if d := e.Evaluate(r, Observation{Value: 5.5, Now: 1}); !d.Fire {
		t.Fatal("5.5 is above 5 and must fire")
	}
	// Dips just under the threshold, but not through the hysteresis band.
	e.Evaluate(r, Observation{Value: 4.9, Now: 2})
	if d := e.Evaluate(r, Observation{Value: 5.5, Now: 3}); d.Fire {
		t.Fatal("the value never cleared the hysteresis band, so this is the SAME event, not a new one")
	}
	// Now it genuinely recovers, and a later excursion is a new event.
	e.Evaluate(r, Observation{Value: 3.5, Now: 4})
	if d := e.Evaluate(r, Observation{Value: 5.5, Now: 5}); !d.Fire {
		t.Fatal("the value cleared the band and came back: that is a new event and must fire")
	}
}

func TestRule_CooldownSuppressesRefiring(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.CooldownSeconds = 300

	if d := e.Evaluate(r, Observation{Value: 8, Now: 1000}); !d.Fire {
		t.Fatal("first fire")
	}
	// Clear, then come back inside the cooldown.
	e.Evaluate(r, Observation{Value: 2, Now: 1100})
	if d := e.Evaluate(r, Observation{Value: 8, Now: 1200}); d.Fire {
		t.Fatalf("only 200s into a 300s cooldown; must not re-fire (suppressed=%q)", d.Suppressed)
	}
	e.Evaluate(r, Observation{Value: 2, Now: 1250})
	if d := e.Evaluate(r, Observation{Value: 8, Now: 1400}); !d.Fire {
		t.Fatal("400s > 300s cooldown; must fire again")
	}
}

// THE REGRESSION TEST FOR MYMATASAN'S ACTUAL BUG. LastTriggeredAt was loaded from the DB,
// carried into the detector, and never read — so every restart re-armed every rule and fired an
// alert storm at an operator who was already dealing with the incident. If the seeding is ever
// dropped, this test fails.
func TestRule_CooldownSurvivesRestart(t *testing.T) {
	r := aboveRule()
	r.CooldownSeconds = 3600

	// A brand-new engine: the process restarted. The cooldown is restored from the durable
	// record of the last fire (in production, the alert log — see RuleService.Reload).
	e := NewRuleEngine()
	e.SeedCooldown(r.Id, 7, 1000)

	// The condition is still true — the cold store is still warm — but the alert already went
	// out ten minutes ago and the operator is already on it.
	if d := e.Evaluate(r, Observation{DeviceId: 7, Value: 8, Now: 1600}); d.Fire {
		t.Fatal("the cooldown must SURVIVE RESTART; re-firing here is the alert storm mymatasan shipped")
	}
	if d := e.Evaluate(r, Observation{DeviceId: 7, Value: 8, Now: 4700}); !d.Fire {
		t.Fatal("once the restored cooldown genuinely expires, the rule must fire again")
	}
}

// THE MISSED-ALERT BUG, pinned.
//
// A tag-scoped rule watches every device carrying the tag — "cold store above 5C" over ten
// fridges — and each device MUST get its own debounce, firing flag and cooldown.
//
// Keying the engine's state by RULE alone (which is what the first version of this did) means
// the first fridge to trip the rule sets `firing`, and every other fridge is then silently
// suppressed as "already firing". Fridge A alerts; fridges B through J are defrosting and
// nobody is told. A MISSED alert is the one failure a monitoring product may never have — worse
// than a duplicate, worse than a storm.
//
// Found by pointing an offline rule at 20 real devices and watching exactly one of them fire.
func TestRule_TagScopedRuleFiresPerDeviceNotOnce(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.Tag = "cold-store"
	r.CooldownSeconds = 3600

	fired := 0
	for deviceId := int64(1); deviceId <= 10; deviceId++ {
		if d := e.Evaluate(r, Observation{DeviceId: deviceId, Value: 8.4, Now: 1000}); d.Fire {
			fired++
		}
	}
	if fired != 10 {
		t.Fatalf("all 10 devices are over the threshold and each must raise its own alert; only %d fired", fired)
	}
}

// And the cooldown is per-device too: fridge A having just alerted must not silence fridge B,
// which has not.
func TestRule_CooldownIsPerDevice(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.Tag = "cold-store"
	r.CooldownSeconds = 3600

	if d := e.Evaluate(r, Observation{DeviceId: 1, Value: 8, Now: 1000}); !d.Fire {
		t.Fatal("device 1 must fire")
	}
	// Device 1 is now cooling down. Device 2 has never fired and must not inherit that.
	if d := e.Evaluate(r, Observation{DeviceId: 2, Value: 8, Now: 1100}); !d.Fire {
		t.Fatalf("device 2 has its own cooldown and must fire (suppressed=%q)", d.Suppressed)
	}
	// Device 1 is still inside its own cooldown.
	e.Evaluate(r, Observation{DeviceId: 1, Value: 2, Now: 1200})
	if d := e.Evaluate(r, Observation{DeviceId: 1, Value: 8, Now: 1300}); d.Fire {
		t.Fatal("device 1 is still inside its own 3600s cooldown")
	}
}

func TestRule_StuckCatchesAFrozenSensor(t *testing.T) {
	e := NewRuleEngine()
	r := entities.IotRule{
		Id: 2, Enabled: true, Key: "temperature", Condition: "stuck", WindowSeconds: 3600,
	}
	// A perfectly plausible value that has not moved at all. Every other condition calls this
	// healthy; this one calls it broken.
	if d := e.Evaluate(r, Observation{Value: 21.4, Previous: 21.4, HasPrevious: true, ElapsedSeconds: 3600, Now: 5000}); !d.Fire {
		t.Fatal("an unchanged value across the window means the sensor has frozen and must fire")
	}
	e.Forget(2)
	if d := e.Evaluate(r, Observation{Value: 21.5, Previous: 21.4, HasPrevious: true, ElapsedSeconds: 3600, Now: 5000}); d.Fire {
		t.Fatal("a value that moved is not stuck")
	}
}

func TestRule_RateUsesRealElapsedTimeNotTheNominalWindow(t *testing.T) {
	e := NewRuleEngine()
	r := entities.IotRule{
		Id: 3, Enabled: true, Key: "temperature", Condition: "rate",
		Threshold: 10, WindowSeconds: 60, // more than 10 degrees per minute
	}
	// +2 degrees in 6 seconds = 20/min. Fast. Using the nominal 60s window instead would have
	// computed 2/min and missed it entirely.
	if d := e.Evaluate(r, Observation{Value: 22, Previous: 20, HasPrevious: true, ElapsedSeconds: 6, Now: 100}); !d.Fire {
		t.Fatal("20 degrees/min exceeds the 10/min threshold and must fire")
	}
}

func TestRule_OfflineFiresOnSilence(t *testing.T) {
	e := NewRuleEngine()
	r := entities.IotRule{Id: 4, Enabled: true, Condition: "offline", WindowSeconds: 600}
	if d := e.Evaluate(r, Observation{SilentSeconds: 300, Now: 100}); d.Fire {
		t.Fatal("5 minutes of silence is inside the 10-minute window")
	}
	if d := e.Evaluate(r, Observation{SilentSeconds: 900, Now: 200}); !d.Fire {
		t.Fatal("15 minutes of silence exceeds the window and must fire")
	}
}

func TestRule_DisabledNeverFires(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.Enabled = false
	if d := e.Evaluate(r, Observation{Value: 100, Now: 1}); d.Fire {
		t.Fatal("a disabled rule must never fire")
	}
}

// The overnight window is the one a security rule most wants, and the one an off-by-one in the
// midnight wrap would silently disable — and a rule that never fires is a rule nobody notices
// is broken.
func TestRule_ScheduleSpansMidnight(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.Condition = "equals"
	r.Threshold = 1
	r.Key = "contact"
	r.SchedulePolicy = "window"
	r.ScheduleStart = "22:00"
	r.ScheduleEnd = "06:00"

	at := func(h, m int) int64 {
		return time.Date(2026, 7, 14, h, m, 0, 0, time.Local).Unix()
	}

	// 03:00 — inside the overnight window. A door opening here is the alert.
	if d := e.Evaluate(r, Observation{Value: 1, Now: at(3, 0)}); !d.Fire {
		t.Fatalf("03:00 is inside a 22:00-06:00 window and must fire (suppressed=%q)", d.Suppressed)
	}
	e.Forget(r.Id)
	// 23:30 — also inside it, on the other side of midnight.
	if d := e.Evaluate(r, Observation{Value: 1, Now: at(23, 30)}); !d.Fire {
		t.Fatal("23:30 is inside a 22:00-06:00 window and must fire")
	}
	e.Forget(r.Id)
	// 09:00 — the loading bay is open for business; the same door opening is routine.
	if d := e.Evaluate(r, Observation{Value: 1, Now: at(9, 0)}); d.Fire {
		t.Fatal("09:00 is OUTSIDE a 22:00-06:00 window and must not fire")
	}
}

// A condition half-satisfied when the window closes must not carry over and fire on the first
// sample of the next window.
func TestRule_ScheduleResetsTheDebounceOnTheWayOut(t *testing.T) {
	e := NewRuleEngine()
	r := aboveRule()
	r.ConsecutiveSamples = 2
	r.SchedulePolicy = "window"
	r.ScheduleStart = "22:00"
	r.ScheduleEnd = "06:00"
	at := func(h, m int) int64 { return time.Date(2026, 7, 14, h, m, 0, 0, time.Local).Unix() }

	e.Evaluate(r, Observation{Value: 8, Now: at(5, 59)}) // 1 of 2, inside the window
	e.Evaluate(r, Observation{Value: 8, Now: at(7, 0)})  // outside: not watching, counter resets
	if d := e.Evaluate(r, Observation{Value: 8, Now: at(23, 0)}); d.Fire {
		t.Fatal("the debounce must restart in the new window, not inherit a stale count from the last one")
	}
}
