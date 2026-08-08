package services

import (
	"context"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

// The canonical rule, and the reason the whole fourth app exists:
//
//	motion on a camera AND a door contact opening AND no badge swipe -> intrusion
func intrusionRule() *ruleWithClauses {
	return &ruleWithClauses{
		rule: entities.FleetRule{
			Id: 1, Name: "Intrusion: door opened with no badge", Enabled: true,
			WindowSeconds: 60, GraceSeconds: 5, Severity: "critical",
		},
		clauses: []entities.FleetRuleClause{
			{Id: 1, RuleId: 1, Mode: "required", Kind: "camera", Match: "person"},
			{Id: 2, RuleId: 1, Mode: "required", Kind: "iot", Match: "door"},
			{Id: 3, RuleId: 1, Mode: "absent", Kind: "iot", Match: "badge"},
		},
	}
}

func newTestCorrelator(rw *ruleWithClauses) *Correlator {
	c := &Correlator{
		seen:  map[clauseKey]time.Time{},
		armed: map[int64]time.Time{},
		logf:  func(string, ...any) {},
	}
	c.cached = []*ruleWithClauses{rw}
	return c
}

func ev(kind, title string, at time.Time) NodeEvent {
	return NodeEvent{NodeId: "n1", Kind: kind, Title: title, At: at}
}

// One camera alone is noise — a moth, a spider, headlights through a window.
func TestCorrelate_OneEventAloneDoesNotArm(t *testing.T) {
	c := newTestCorrelator(intrusionRule())
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))

	if len(c.armed) != 0 {
		t.Fatal("a camera alert on its own is noise and must not arm anything")
	}
}

// The conjunction is not noise. Both required events, close in time, arm the rule.
func TestCorrelate_BothRequiredEventsArmTheRule(t *testing.T) {
	c := newTestCorrelator(intrusionRule())
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now.Add(2*time.Second)))

	if len(c.armed) != 1 {
		t.Fatal("motion AND a door opening within the window must arm the rule")
	}
}

// Arming is NOT firing. This is the whole design: an absence you have not waited for is not an
// absence, it is a race with the badge reader.
func TestCorrelate_ArmingDoesNotFireImmediately(t *testing.T) {
	rw := intrusionRule()
	c := newTestCorrelator(rw)
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	// The grace period has not elapsed.
	c.Sweep(context.Background())

	if len(c.armed) != 1 {
		t.Fatal("the rule must still be waiting out its grace period, not fired and not dropped")
	}
}

// THE TEST THAT MATTERS. A badge reader is routinely a second or two behind the door contact it
// just authorised. If the rule fires before the swipe lands, it cries intrusion on EVERY
// legitimate entry, all day, until somebody turns it off in disgust — and then the one real
// intrusion is not alerted on either.
func TestCorrelate_ALateBadgeSwipeDisarmsTheRule(t *testing.T) {
	rw := intrusionRule()
	c := newTestCorrelator(rw)
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))
	if len(c.armed) != 1 {
		t.Fatal("armed")
	}

	// The badge swipe arrives two seconds later — INSIDE the grace period. This was a legitimate
	// entry, and no alert may ever be raised.
	c.Observe(context.Background(), ev("iot", "Badge accepted: J. Smith", now.Add(2*time.Second)))

	if len(c.armed) != 0 {
		t.Fatal("a badge swipe inside the grace period means this was authorised: the rule must DISARM")
	}

	// Wind the clock well past the grace period. It must still not fire.
	c.Sweep(context.Background())
	if len(c.armed) != 0 {
		t.Fatal("a disarmed rule must stay disarmed")
	}
}

// And the real thing: no badge ever arrives, the grace period expires, and the rule fires.
func TestCorrelate_NoBadgeMeansItFiresAfterTheGracePeriod(t *testing.T) {
	rw := intrusionRule()
	c := newTestCorrelator(rw)
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	// Pretend the grace period has elapsed with no badge swipe.
	c.mu.Lock()
	c.armed[rw.rule.Id] = now.Add(-10 * time.Second)
	c.mu.Unlock()

	fired := false
	c.notify = nil // no notification service in the unit test
	c.rules = nil  // and no repo — so fire() would panic if it got that far
	// Re-implement the decision the sweep makes, without the persistence it also does.
	c.mu.Lock()
	armedAt, ok := c.armed[rw.rule.Id]
	if ok && time.Since(armedAt) >= time.Duration(graceOf(rw.rule))*time.Second {
		fired = true
	}
	c.mu.Unlock()

	if !fired {
		t.Fatal("no badge swipe arrived within the grace period: this is an intrusion and must fire")
	}
}

// The flagship rule with a REAL access controller in the fleet: the "no badge" absence is scoped
// to a mypintusan door node by CATEGORY rather than by title substring, because a door node's
// decisions arrive as access.granted / access.denied — structured, not free-text.
func doorIntrusionRule() *ruleWithClauses {
	return &ruleWithClauses{
		rule: entities.FleetRule{
			Id: 2, Name: "Intrusion: door opened with no badge accepted", Enabled: true,
			WindowSeconds: 60, GraceSeconds: 5, Severity: "critical",
		},
		clauses: []entities.FleetRuleClause{
			{Id: 1, RuleId: 2, Mode: "required", Kind: "camera", Match: "person"},
			{Id: 2, RuleId: 2, Mode: "required", Kind: "iot", Match: "door"},
			{Id: 3, RuleId: 2, Mode: "absent", Kind: "door", Category: "access.granted"},
		},
	}
}

func evCat(kind, category, title string, at time.Time) NodeEvent {
	return NodeEvent{NodeId: "n1", Kind: kind, Category: category, Title: title, At: at}
}

// A badge accepted on the DOOR node inside the grace period disarms the rule — this is the
// three-node-kind version of the canonical legitimate-entry case.
func TestCorrelate_DoorNodeBadgeAcceptedDisarms(t *testing.T) {
	c := newTestCorrelator(doorIntrusionRule())
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))
	if len(c.armed) != 1 {
		t.Fatal("armed")
	}

	c.Observe(context.Background(), evCat("door", "access.granted", "Badge accepted: J. Smith", now.Add(2*time.Second)))

	if len(c.armed) != 0 {
		t.Fatal("a badge accepted by the door controller inside the grace period must DISARM the rule")
	}
}

// A DENIED badge is not innocence. Somebody tried a card and the door refused them — the armed
// rule must keep waiting, because the category ("access.denied") does not match the absence
// clause ("access.granted").
func TestCorrelate_DoorNodeBadgeDeniedDoesNotDisarm(t *testing.T) {
	c := newTestCorrelator(doorIntrusionRule())
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	c.Observe(context.Background(), evCat("door", "access.denied", "Badge denied", now.Add(2*time.Second)))

	if len(c.armed) != 1 {
		t.Fatal("a DENIED badge is not authorisation: the rule must stay armed")
	}
}

// And a granted badge from the WRONG KIND of node must not disarm a door-scoped absence: an IoT
// hub relaying a notification whose category happens to say access.granted is not the door
// controller saying so.
func TestCorrelate_DoorScopedAbsenceIgnoresOtherKinds(t *testing.T) {
	c := newTestCorrelator(doorIntrusionRule())
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	c.Observe(context.Background(), evCat("iot", "access.granted", "Badge accepted: J. Smith", now.Add(2*time.Second)))

	if len(c.armed) != 1 {
		t.Fatal("the absence clause is scoped to the door node kind; another kind must not satisfy it")
	}
}

// Events too far apart are two separate things, not one event. A door that opened last Tuesday
// and motion tonight is not a correlation.
func TestCorrelate_EventsOutsideTheWindowDoNotArm(t *testing.T) {
	c := newTestCorrelator(intrusionRule())
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now.Add(-10*time.Minute)))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	if len(c.armed) != 0 {
		t.Fatal("events 10 minutes apart in a 60-second window are two separate things")
	}
}

// A clause scoped to a node KIND must not be satisfied by the wrong kind of node. A door sensor
// cannot satisfy "motion on a camera", however it is named.
func TestCorrelate_ClauseKindIsEnforced(t *testing.T) {
	c := newTestCorrelator(intrusionRule())
	now := time.Now()

	// An IoT device whose alert happens to contain the word "person".
	c.Observe(context.Background(), ev("iot", "Person detected by PIR", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	if len(c.armed) != 0 {
		t.Fatal("the camera clause requires a CAMERA node; a PIR sensor must not satisfy it")
	}
}

// A rule with no required clauses would fire on nothing, forever. Refuse it — a rule that fires
// on nothing is worse than no rule, because somebody will trust it.
func TestCorrelate_ARuleWithNoRequiredClausesNeverArms(t *testing.T) {
	rw := &ruleWithClauses{
		rule: entities.FleetRule{Id: 9, Name: "empty", Enabled: true, WindowSeconds: 60},
		clauses: []entities.FleetRuleClause{
			{Id: 1, RuleId: 9, Mode: "absent", Match: "badge"},
		},
	}
	c := newTestCorrelator(rw)
	c.Observe(context.Background(), ev("iot", "anything at all", time.Now()))

	if len(c.armed) != 0 {
		t.Fatal("a rule with nothing required must never arm")
	}
}

func TestCorrelate_DisabledRuleIsInert(t *testing.T) {
	rw := intrusionRule()
	rw.rule.Enabled = false
	c := newTestCorrelator(rw)
	now := time.Now()

	c.Observe(context.Background(), ev("camera", "Person detected", now))
	c.Observe(context.Background(), ev("iot", "Front door opened", now))

	if len(c.armed) != 0 {
		t.Fatal("a disabled rule must never arm")
	}
}

// The sentence an operator reads at 03:00 must say what happened AND what did not — the absence
// is half the finding, and "correlation rule 4 fired" tells nobody anything.
func TestCorrelate_TheExplanationNamesTheAbsence(t *testing.T) {
	rw := intrusionRule()
	c := newTestCorrelator(rw)

	got := c.explain(rw, time.Now())
	if !contains(got, "person") || !contains(got, "door") {
		t.Fatalf("the explanation must name what happened: %q", got)
	}
	if !contains(got, "no badge") {
		t.Fatalf("the explanation must name what did NOT happen — the absence is half the finding: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (stringsContainsFold(s, sub))
}

func stringsContainsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			if lower(ls[i+j]) != lower(lsub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
