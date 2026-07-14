package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

// The rule evaluator. Pure logic: state in, decision out, no database and no clock of its
// own. Everything that made mymatasan's detector painful is carried over deliberately here
// rather than re-learned in production.
//
// The conditions:
//
//	above    the value is over the threshold
//	below    the value is under it
//	equals   the value is the threshold (a door contact reaching 1)
//	delta    the value moved more than the threshold across the window
//	rate     the value is changing faster than the threshold per minute
//	stuck    the value has NOT moved at all across the window — a sensor that has frozen
//	         reads perfectly healthy to every other condition, which is exactly why this
//	         exists. A flatlined temperature probe is a failure that hides as good news.
//	offline  nothing has been heard from the device for the window
//
// The three defences against a nuisance alert, all of which mymatasan needed:
//
//   - DEBOUNCE (ConsecutiveSamples): N readings in a row must satisfy the condition. A PIR
//     twitches, a contact bounces, a probe spikes for one sample. Firing on one reading wakes
//     an operator at 03:00 for a bounce, and an alert channel nobody trusts is worse than none.
//   - HYSTERESIS: once fired, the value must travel back PAST the threshold by this much
//     before the rule re-arms. Without it a value hovering on the threshold fires, clears,
//     fires, clears — the classic flapping alert.
//   - COOLDOWN, WHICH SURVIVES RESTART. This is the bug mymatasan actually shipped:
//     LastTriggeredAt was loaded from the database, carried into the detector, and then never
//     read — so every restart re-armed every rule and produced an alert storm. SeedCooldown
//     exists so that cannot happen here.

// RuleState is the per-rule memory the evaluator keeps between samples.
type ruleState struct {
	// consecutive counts readings in a row that satisfied the condition.
	consecutive int
	// firing is true between a fire and its hysteresis-gated reset. It is what stops a rule
	// re-firing on every subsequent sample while the condition is still true.
	firing bool
	// lastTriggered is when the rule last fired (unix seconds). Seeded from the database at
	// boot so the cooldown outlives a restart.
	lastTriggered int64
}

// seriesKey identifies one rule's state ON ONE DEVICE.
//
// The device half is load-bearing. A rule scoped to a TAG watches every device carrying it —
// "cold store above 5C" over ten fridges — and each fridge must get its own debounce, its own
// firing flag and its own cooldown. Keying the state by rule alone means the first fridge to
// trip the rule suppresses the other nine: fridge A alerts, fridges B through J are silently
// "already firing", and nobody is told they are defrosting. That is a MISSED alert, which is
// the one failure a monitoring product may never have — worse than a duplicate, worse than a
// storm. (Found by pointing an offline rule at 20 devices and watching exactly one fire.)
type seriesKey struct {
	ruleId   int64
	deviceId int64
}

// RuleEngine evaluates rules against readings.
type RuleEngine struct {
	mu    sync.Mutex
	state map[seriesKey]*ruleState
}

func NewRuleEngine() *RuleEngine {
	return &RuleEngine{state: map[seriesKey]*ruleState{}}
}

// SeedCooldown restores a rule's last-fired time from the database at startup.
//
// Without this call the cooldown is a lie: it holds for the process lifetime and resets the
// moment anybody restarts the app, so an appliance that reboots nightly re-fires every
// already-acknowledged rule every morning. mymatasan shipped exactly that bug — the field was
// loaded and then never read.
func (e *RuleEngine) SeedCooldown(ruleId, deviceId int64, lastTriggeredAt int64) {
	if ruleId <= 0 || lastTriggeredAt <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.stateFor(seriesKey{ruleId: ruleId, deviceId: deviceId})
	if lastTriggeredAt > st.lastTriggered {
		st.lastTriggered = lastTriggeredAt
	}
}

// Forget drops a rule's state on EVERY device — on delete, or on edit, where keeping a
// half-satisfied debounce counter from the OLD thresholds would be wrong.
func (e *RuleEngine) Forget(ruleId int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k := range e.state {
		if k.ruleId == ruleId {
			delete(e.state, k)
		}
	}
}

// stateFor must be called with the lock held.
func (e *RuleEngine) stateFor(k seriesKey) *ruleState {
	st, ok := e.state[k]
	if !ok {
		st = &ruleState{}
		e.state[k] = st
	}
	return st
}

// Observation is what the evaluator needs to know about the world for one rule check.
type Observation struct {
	// DeviceId is WHICH device this observation is about. A tag-scoped rule is evaluated once
	// per device, and each device carries its own debounce, firing flag and cooldown.
	DeviceId int64
	// Value is the current reading.
	Value float64
	// Previous is the value at the start of the rule's window, for delta/rate/stuck. Only
	// meaningful when HasPrevious is set.
	Previous    float64
	HasPrevious bool
	// ElapsedSeconds is the actual span between Previous and Value. rate needs the real
	// elapsed time, not the nominal window: two samples 8 seconds apart in a 60-second window
	// describe a much steeper rate than the window would imply.
	ElapsedSeconds float64
	// SilentSeconds is how long since anything at all was heard from the device (for offline).
	SilentSeconds float64
	// Now is the evaluation time (unix seconds), passed in so this is testable.
	Now int64
}

// Decision is the outcome of evaluating one rule.
type Decision struct {
	// Fire is true when the rule should raise an alert now.
	Fire bool
	// Reason is the human sentence for the alert body.
	Reason string
	// Suppressed, when non-empty, explains why a satisfied condition did NOT fire. It exists
	// so the UI can answer "the value is clearly over the line, why is there no alert?" —
	// which is otherwise the single most common support question about a rules engine.
	Suppressed string
}

// Evaluate decides whether a rule fires for one observation.
func (e *RuleEngine) Evaluate(rule entities.IotRule, obs Observation) Decision {
	if !rule.Enabled {
		return Decision{Suppressed: "rule is disabled"}
	}
	key := seriesKey{ruleId: rule.Id, deviceId: obs.DeviceId}

	if !withinSchedule(rule, obs.Now) {
		// Outside its window the rule is not merely quiet — it is NOT WATCHING. Reset the
		// debounce so a condition that was half-satisfied when the window closed does not
		// carry across into the next window and fire on the first sample of the morning.
		e.mu.Lock()
		e.stateFor(key).consecutive = 0
		e.mu.Unlock()
		return Decision{Suppressed: "outside the rule's schedule"}
	}

	satisfied, reason := conditionMet(rule, obs)

	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.stateFor(key)

	if !satisfied {
		// The condition is false. Re-arm the rule only once the value has cleared the
		// hysteresis band, so a value dithering on the threshold does not flap.
		if st.firing && clearedHysteresis(rule, obs.Value) {
			st.firing = false
		}
		st.consecutive = 0
		return Decision{}
	}

	st.consecutive++

	need := rule.ConsecutiveSamples
	if need < 1 {
		need = 1
	}
	if st.consecutive < need {
		return Decision{Suppressed: fmt.Sprintf("debouncing: %d of %d consecutive samples", st.consecutive, need)}
	}

	if st.firing {
		return Decision{Suppressed: "already firing; the value has not cleared the hysteresis band"}
	}

	if rule.CooldownSeconds > 0 && st.lastTriggered > 0 {
		if elapsed := obs.Now - st.lastTriggered; elapsed < int64(rule.CooldownSeconds) {
			return Decision{Suppressed: fmt.Sprintf("cooling down: %ds of %ds", elapsed, rule.CooldownSeconds)}
		}
	}

	st.firing = true
	st.lastTriggered = obs.Now
	return Decision{Fire: true, Reason: reason}
}

// LastTriggered reports when a rule last fired ON A DEVICE.
func (e *RuleEngine) LastTriggered(ruleId, deviceId int64) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st, ok := e.state[seriesKey{ruleId: ruleId, deviceId: deviceId}]; ok {
		return st.lastTriggered
	}
	return 0
}

// conditionMet is the actual predicate, and the human sentence that goes with it.
func conditionMet(rule entities.IotRule, obs Observation) (bool, string) {
	switch strings.ToLower(strings.TrimSpace(rule.Condition)) {
	case "above":
		return obs.Value > rule.Threshold,
			fmt.Sprintf("%s is %s, above %s", label(rule), num(obs.Value), num(rule.Threshold))

	case "below":
		return obs.Value < rule.Threshold,
			fmt.Sprintf("%s is %s, below %s", label(rule), num(obs.Value), num(rule.Threshold))

	case "equals":
		return obs.Value == rule.Threshold,
			fmt.Sprintf("%s is %s", label(rule), num(obs.Value))

	case "delta":
		if !obs.HasPrevious {
			return false, ""
		}
		moved := math.Abs(obs.Value - obs.Previous)
		return moved > rule.Threshold,
			fmt.Sprintf("%s moved %s (from %s to %s), more than %s",
				label(rule), num(moved), num(obs.Previous), num(obs.Value), num(rule.Threshold))

	case "rate":
		// Per MINUTE, computed from the real elapsed time. Using the nominal window instead
		// would understate a fast change measured across two close samples.
		if !obs.HasPrevious || obs.ElapsedSeconds <= 0 {
			return false, ""
		}
		perMin := (obs.Value - obs.Previous) / obs.ElapsedSeconds * 60
		return math.Abs(perMin) > rule.Threshold,
			fmt.Sprintf("%s is changing at %s per minute, faster than %s",
				label(rule), num(perMin), num(rule.Threshold))

	case "stuck":
		// A sensor that has frozen reports a perfectly plausible value forever, so every
		// other condition says it is healthy. This is the one that catches it.
		if !obs.HasPrevious {
			return false, ""
		}
		return obs.Value == obs.Previous,
			fmt.Sprintf("%s has not changed from %s for %s — the sensor may have stopped reporting",
				label(rule), num(obs.Value), humanSeconds(rule.WindowSeconds))

	case "offline":
		if rule.WindowSeconds <= 0 {
			return false, ""
		}
		return obs.SilentSeconds > float64(rule.WindowSeconds),
			fmt.Sprintf("no reading for %s", humanSeconds(int(obs.SilentSeconds)))
	}
	return false, ""
}

// clearedHysteresis reports whether a firing rule's value has travelled far enough back
// through the threshold to re-arm.
//
// For a rule with no hysteresis this is just "the condition is false", which is the caller's
// precondition — so the rule re-arms as soon as the value crosses back. With hysteresis it
// must cross back AND clear the band.
func clearedHysteresis(rule entities.IotRule, value float64) bool {
	if rule.Hysteresis <= 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(rule.Condition)) {
	case "above":
		return value <= rule.Threshold-rule.Hysteresis
	case "below":
		return value >= rule.Threshold+rule.Hysteresis
	default:
		// delta/rate/stuck/equals/offline are not "level" conditions; there is no band to
		// travel back through, so falsity alone re-arms them.
		return true
	}
}

// withinSchedule reports whether the rule is watching right now.
//
// A loading-bay door opening is routine at 09:00 and an intrusion at 03:00; that is the
// entire reason this exists. A window whose end is before its start (22:00 to 06:00) spans
// MIDNIGHT, which is precisely when a security rule is most likely to be wanted — getting
// that wrong would silently disable the overnight rules, and the rules that never fire are
// the ones nobody notices are broken.
func withinSchedule(rule entities.IotRule, now int64) bool {
	policy := strings.ToLower(strings.TrimSpace(rule.SchedulePolicy))
	if policy == "" || policy == "always" {
		return true
	}
	if policy != "window" {
		return true
	}

	t := time.Unix(now, 0)

	if days := strings.TrimSpace(rule.ScheduleDays); days != "" {
		today := int(t.Weekday())
		found := false
		for _, part := range strings.Split(days, ",") {
			if d, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && d == today {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	start, okStart := parseClock(rule.ScheduleStart)
	end, okEnd := parseClock(rule.ScheduleEnd)
	if !okStart || !okEnd {
		return true // an unparseable window is not a reason to silently stop watching
	}

	mins := t.Hour()*60 + t.Minute()
	if start == end {
		return true // a zero-width window means "all day", not "never"
	}
	if start < end {
		return mins >= start && mins < end
	}
	// Spans midnight: 22:00-06:00 means late evening OR early morning.
	return mins >= start || mins < end
}

// parseClock reads "HH:MM" into minutes since midnight.
func parseClock(v string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func label(rule entities.IotRule) string {
	if k := strings.TrimSpace(rule.Key); k != "" {
		return k
	}
	return "value"
}

// num formats a reading without trailing noise: 21.4, not 21.399999999999999.
func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func humanSeconds(s int) string {
	switch {
	case s >= 3600:
		return fmt.Sprintf("%dh", s/3600)
	case s >= 60:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
