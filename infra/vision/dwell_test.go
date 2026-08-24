package vision

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

// The rules here are about TIME, so every test drives a clock and several samples. A test
// that checks one frame checks nothing about them.
//
// And every one of them makes the rule FIRE. A detector with no test that reaches an alert
// is a detector that may not work at all — the tamper MOVED verdict shipped dead for exactly
// that reason, passing a green suite the whole way.

type dwellHarness struct {
	t        *testing.T
	backend  *mutableObjectDetector
	detector *ObjectRuleDetector
	now      time.Time
	rule     DetectionRule
}

func newDwellHarness(t *testing.T, rule DetectionRule) *dwellHarness {
	t.Helper()
	backend := &mutableObjectDetector{}
	detector := NewObjectRuleDetector(backend, ObjectRuleDetectorOptions{})
	h := &dwellHarness{t: t, backend: backend, detector: detector, now: time.Unix(1000, 0), rule: rule}
	detector.now = func() time.Time { return h.now }
	return h
}

// step advances the clock and runs one detection pass with the given candidates.
func (h *dwellHarness) step(seconds int, candidates ...ObjectCandidate) []Detection {
	h.t.Helper()
	h.now = h.now.Add(time.Duration(seconds) * time.Second)
	h.backend.candidates = candidates
	got, err := h.detector.Detect(context.Background(), Frame{CameraId: h.rule.CameraId}, []DetectionRule{h.rule})
	if err != nil {
		h.t.Fatalf("Detect() error = %v", err)
	}
	return got
}

func person(x, y float64) ObjectCandidate {
	return ObjectCandidate{Label: "person", Confidence: 0.9, Box: boxFromCenter(x, y)}
}

// tracked is a candidate carrying the stable ByteTrack id the YOLO worker supplies in
// production. Identity beats geometry, and the rules that follow an object across the frame
// behave differently depending on which one is available — so the tests that care say which
// they are exercising rather than leaving it to the size of the step.
func tracked(c ObjectCandidate, id int64) ObjectCandidate {
	c.Metadata = map[string]any{"trackId": float64(id)}
	return c
}

func bag(x, y float64) ObjectCandidate {
	return ObjectCandidate{Label: "backpack", Confidence: 0.9, Box: boxFromCenter(x, y)}
}

func meta(t *testing.T, d Detection) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal([]byte(d.Metadata), &out); err != nil {
		t.Fatalf("metadata is not JSON: %v", err)
	}
	return out
}

func loiteringRule() DetectionRule {
	return DetectionRule{
		Id: 1, CameraId: 7, DetectionType: DetectionLoitering,
		ZonePolygon:     `[[0.2,0.2],[0.8,0.2],[0.8,0.8],[0.2,0.8]]`,
		RuleConfig:      `{"classes":["person"],"dwellSeconds":30}`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}
}

func TestLoiteringFiresOnlyAfterTheDwellThreshold(t *testing.T) {
	h := newDwellHarness(t, loiteringRule())

	if got := h.step(0, person(0.5, 0.5)); len(got) != 0 {
		t.Fatalf("arriving is not loitering: %d detections", len(got))
	}
	if got := h.step(20, person(0.51, 0.5)); len(got) != 0 {
		t.Fatalf("20s of a 30s rule must stay quiet: %d detections", len(got))
	}
	got := h.step(11, person(0.52, 0.5))
	if len(got) != 1 {
		t.Fatalf("31s of a 30s rule must fire: %d detections", len(got))
	}
	if got[0].DetectionType != DetectionLoitering {
		t.Fatalf("wrong detection type: %q", got[0].DetectionType)
	}
	// ONE ALERT PER OBJECT. Without the per-track latch, the same person re-fires on every
	// cooldown expiry for as long as they stand there — the rule's cooldown limits how
	// often the RULE speaks, not how often one object does.
	if again := h.step(40, person(0.53, 0.5)); len(again) != 0 {
		t.Fatalf("the same person must not re-fire: %d detections", len(again))
	}
}

// THE TIMESTAMP MEANS WHEN IT STARTED, NOT WHEN WE NOTICED. An alert that says "loitering at
// 14:05" sends somebody to 14:05 in the footage; the person arrived at 14:04:30, and the
// thirty seconds the rule waited are exactly the interesting ones. Same trap W2-2 found in
// the availability numbers.
func TestLoiteringReportsWhenTheDwellStartedNotWhenItWasNoticed(t *testing.T) {
	h := newDwellHarness(t, loiteringRule())
	h.step(0, person(0.5, 0.5))
	arrived := h.now.Unix()
	got := h.step(31, person(0.5, 0.5))
	if len(got) != 1 {
		t.Fatalf("expected one detection, got %d", len(got))
	}
	m := meta(t, got[0])
	if int64(m["dwellStartedAt"].(float64)) != arrived {
		t.Fatalf("dwellStartedAt = %v, want %d (when they arrived)", m["dwellStartedAt"], arrived)
	}
	if int64(m["dwellSeconds"].(float64)) != 31 {
		t.Fatalf("dwellSeconds = %v, want 31", m["dwellSeconds"])
	}
}

// A DROPPED FRAME IS NOT AN EXIT. Confidence dips and brief occlusions are the normal
// condition of a real camera; if either restarted the timer, a thirty-second rule would
// never fire anywhere anybody walks behind a pillar.
func TestLoiteringSurvivesAMissedSample(t *testing.T) {
	h := newDwellHarness(t, loiteringRule())
	h.step(0, person(0.5, 0.5))
	h.step(10, person(0.5, 0.5))
	h.step(3) // the detector saw nothing at all this pass
	got := h.step(20, person(0.5, 0.5))
	if len(got) != 1 {
		t.Fatalf("a single missed sample must not reset the dwell: %d detections", len(got))
	}
}

// BUT BEING SEEN OUTSIDE THE ZONE IS AN EXIT, and it resets immediately. Not being seen is
// missing information; being seen somewhere else is information. Treating them the same
// either resets on every flicker or lets somebody who steps out and back accumulate a dwell
// they never had.
func TestLoiteringResetsWhenTheObjectIsSeenOutsideTheZone(t *testing.T) {
	h := newDwellHarness(t, loiteringRule())
	// With a ByteTrack id the tracker follows the SAME person out of the zone and back,
	// which is what exercises the exit reset. Without one, a walk that far is a new track
	// and the reset never runs — so this test says which case it is testing.
	h.step(0, tracked(person(0.5, 0.5), 42))
	h.step(20, tracked(person(0.5, 0.5), 42))
	h.step(2, tracked(person(0.05, 0.05), 42)) // stepped out of the zone, still on camera
	if got := h.step(15, tracked(person(0.5, 0.5), 42)); len(got) != 0 {
		t.Fatalf("leaving the zone must restart the dwell: %d detections", len(got))
	}
	if got := h.step(31, tracked(person(0.5, 0.5), 42)); len(got) != 1 {
		t.Fatalf("and the new dwell must then fire on its own clock: %d detections", len(got))
	}
}

// A track nobody has matched for more PASSES than the grace allows is FORGOTTEN, timers and
// all. Counted in passes, not seconds: what is being tolerated is a dropped detection, and
// detections only happen when the detector runs.
func TestLoiteringForgetsATrackAfterEnoughMissedSamples(t *testing.T) {
	h := newDwellHarness(t, loiteringRule())
	h.step(0, person(0.5, 0.5))
	h.step(5, person(0.5, 0.5))
	for i := 0; i < 4; i++ { // four consecutive empty passes, past the grace of three
		h.step(2)
	}
	if got := h.step(2, person(0.5, 0.5)); len(got) != 0 {
		t.Fatalf("a forgotten track must start its dwell again: %d detections", len(got))
	}
	if got := h.step(31, person(0.5, 0.5)); len(got) != 1 {
		t.Fatalf("and then fire on the new clock: %d detections", len(got))
	}
}

func leftBehindRule() DetectionRule {
	return DetectionRule{
		Id: 2, CameraId: 7, DetectionType: DetectionLeftBehind,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      `{"classes":["backpack"],"stillSeconds":60,"driftTolerance":0.05,"personRadius":0.2}`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}
}

// A bag being CARRIED is not a bag left behind, and the difference is movement — not
// presence. A rule that fired on presence would alert on every commuter.
func TestLeftBehindIgnoresAnObjectThatKeepsMoving(t *testing.T) {
	h := newDwellHarness(t, leftBehindRule())
	for i := 0; i < 10; i++ {
		x := 0.1 + float64(i)*0.07
		if got := h.step(20, bag(x, 0.5)); len(got) != 0 {
			t.Fatalf("a moving bag must not be 'left behind' (step %d): %d detections", i, len(got))
		}
	}
}

func TestLeftBehindFiresOnceTheObjectHasSettled(t *testing.T) {
	h := newDwellHarness(t, leftBehindRule())
	h.step(0, bag(0.5, 0.5))
	if got := h.step(30, bag(0.5, 0.5)); len(got) != 0 {
		t.Fatalf("30s of a 60s rule must stay quiet: %d detections", len(got))
	}
	got := h.step(31, bag(0.505, 0.5))
	if len(got) != 1 {
		t.Fatalf("61s still must fire: %d detections", len(got))
	}
	m := meta(t, got[0])
	if int64(m["stillSeconds"].(float64)) < 60 {
		t.Fatalf("stillSeconds = %v, want at least 60", m["stillSeconds"])
	}
}

// A BAG WITH ITS OWNER STANDING NEXT TO IT IS NOT ABANDONED. This is the check that stops
// the rule alerting on every waiting passenger, and it is the one most likely to be dropped
// as an optimisation.
func TestLeftBehindStaysQuietWhileSomebodyIsStandingWithIt(t *testing.T) {
	h := newDwellHarness(t, leftBehindRule())
	h.step(0, bag(0.5, 0.5), person(0.54, 0.5))
	if got := h.step(70, bag(0.5, 0.5), person(0.54, 0.5)); len(got) != 0 {
		t.Fatalf("an attended bag must not alert: %d detections", len(got))
	}
	// The owner walks away; now it is abandoned.
	got := h.step(5, bag(0.5, 0.5), person(0.95, 0.1))
	if len(got) != 1 {
		t.Fatalf("once nobody is with it, it must alert: %d detections", len(got))
	}
	if n := int(meta(t, got[0])["peopleNearby"].(float64)); n != 0 {
		t.Fatalf("peopleNearby = %d, want 0", n)
	}
}

func directionRule(heading string) DetectionRule {
	return DetectionRule{
		Id: 3, CameraId: 7, DetectionType: DetectionDirection,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      `{"classes":["person"],"heading":"` + heading + `","toleranceDegrees":45,"minTravel":0.2}`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}
}

func TestDirectionFiresOnTheWantedHeadingAndNotTheOpposite(t *testing.T) {
	// "up" means up the IMAGE, and image y grows downward — so travelling up is y
	// DECREASING. Getting that negation wrong fires on exactly the traffic the rule is
	// meant to ignore, which is the worst possible failure for a wrong-way rule.
	// WALKED, not teleported. The first version of this test moved the person 0.4 of the
	// frame in one sample — further than the tracker's own matching radius — so it became a
	// NEW track every step and never accumulated any travel at all. A synthetic scene has to
	// reproduce the real condition, not just contain the right answer; same lesson as W1-5's
	// 3px bands and W3-2's orthogonal crowd.
	up := newDwellHarness(t, directionRule("up"))
	up.step(0, person(0.5, 0.85))
	up.step(2, person(0.5, 0.75))
	up.step(2, person(0.5, 0.68))
	got := up.step(2, person(0.5, 0.6))
	if len(got) != 1 {
		t.Fatalf("travelling up the frame must fire: %d detections", len(got))
	}
	if word, _ := meta(t, got[0])["wantedHeading"].(float64); word != 0 {
		t.Fatalf("wantedHeading = %v, want 0 (up)", word)
	}

	down := newDwellHarness(t, directionRule("up"))
	down.step(0, person(0.5, 0.15))
	down.step(2, person(0.5, 0.25))
	down.step(2, person(0.5, 0.32))
	if got := down.step(2, person(0.5, 0.4)); len(got) != 0 {
		t.Fatalf("travelling down must NOT fire an up rule: %d detections", len(got))
	}
}

// Jitter has a random bearing, so without a minimum travel distance a stationary object
// eventually satisfies any heading by accident.
func TestDirectionIgnoresMovementTooSmallToHaveADirection(t *testing.T) {
	h := newDwellHarness(t, directionRule("up"))
	h.step(0, person(0.5, 0.5))
	for i := 0; i < 8; i++ {
		if got := h.step(3, person(0.5, 0.5-float64(i)*0.005)); len(got) != 0 {
			t.Fatalf("sub-threshold drift must not read as a direction (step %d)", i)
		}
	}
}

func TestBearingIsMeasuredUpTheImage(t *testing.T) {
	cases := []struct {
		name string
		from point2D
		to   point2D
		want float64
	}{
		{"up", point2D{0.5, 0.8}, point2D{0.5, 0.2}, 0},
		{"right", point2D{0.2, 0.5}, point2D{0.8, 0.5}, 90},
		{"down", point2D{0.5, 0.2}, point2D{0.5, 0.8}, 180},
		{"left", point2D{0.8, 0.5}, point2D{0.2, 0.5}, 270},
	}
	for _, tc := range cases {
		if got := bearingDegrees(tc.from, tc.to); math.Abs(got-tc.want) > 0.001 {
			t.Fatalf("%s: bearing = %.1f, want %.1f", tc.name, got, tc.want)
		}
	}
	// 350° and 10° are twenty degrees apart, not three hundred and forty.
	if d := angularDistance(350, 10); math.Abs(d-20) > 0.001 {
		t.Fatalf("angularDistance(350,10) = %.1f, want 20", d)
	}
}

// A RULE THAT CANNOT FIRE IS WORSE THAN NO RULE: somebody believes an area is watched. These
// three types have no entry in the static class map, so a rule saved with no classes would
// match nothing forever, and silently.
func TestADwellRuleWithNoClassesIsRefused(t *testing.T) {
	rule := loiteringRule()
	rule.RuleConfig = `{"dwellSeconds":30}`
	if err := ValidateDetectionRule(rule); err == nil {
		t.Fatal("a loitering rule with no classes must be refused")
	}
}

func TestADirectionRuleWithNoHeadingIsRefused(t *testing.T) {
	rule := directionRule("up")
	rule.RuleConfig = `{"classes":["person"]}`
	if err := ValidateDetectionRule(rule); err == nil {
		t.Fatal("a direction rule with no heading must be refused — there is no wrong way")
	}
}

func TestDwellRulesAcceptTheirOwnDefaults(t *testing.T) {
	for _, rule := range []DetectionRule{loiteringRule(), leftBehindRule(), directionRule("north")} {
		if err := ValidateDetectionRule(rule); err != nil {
			t.Fatalf("%s: %v", rule.DetectionType, err)
		}
	}
}
