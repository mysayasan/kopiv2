package vision

import (
	"context"
	"testing"
	"time"
)

func TestSeedCooldown_CarriesPersistedTriggerTimeIntoAFreshProcess(t *testing.T) {
	lastTriggered := map[int64]int64{} // fresh process: the map is always empty on boot
	rule := DetectionRule{Id: 7, LastTriggeredAt: 1_000}

	if !cooldownActive(lastTriggered, rule, 1_030, 60) {
		t.Fatal("a rule that fired 30s ago with a 60s cooldown must still be cooling after a restart")
	}
	if cooldownActive(lastTriggered, rule, 1_120, 60) {
		t.Fatal("a rule whose cooldown has elapsed must be allowed to fire")
	}
}

func TestSeedCooldown_LiveStateWinsOverPersisted(t *testing.T) {
	// The rule fired again in THIS process; the newer in-memory time must govern.
	lastTriggered := map[int64]int64{7: 2_000}
	rule := DetectionRule{Id: 7, LastTriggeredAt: 1_000}

	if !cooldownActive(lastTriggered, rule, 2_030, 60) {
		t.Fatal("the live in-process trigger time must win over the older persisted one")
	}
	if got := lastTriggered[7]; got != 2_000 {
		t.Fatalf("seeding overwrote live state: got %d, want 2000", got)
	}
}

func TestSeedCooldown_NeverTriggeredRuleFiresImmediately(t *testing.T) {
	lastTriggered := map[int64]int64{}
	rule := DetectionRule{Id: 7, LastTriggeredAt: 0}

	if cooldownActive(lastTriggered, rule, 1_000, 60) {
		t.Fatal("a rule that has never fired must not be held in cooldown")
	}
	if _, seeded := lastTriggered[7]; seeded {
		t.Fatal("a never-triggered rule must not be seeded into the cooldown map")
	}
}

// The regression test, through the real detector.
//
// A fresh ObjectRuleDetector is exactly what a restart produces: empty cooldown state.
// Before this fix, cooldown read as zero on boot, so a rule re-fired the instant minFrames
// was met — and a crash-restart loop became an alert storm, a notification storm, and (as
// every alert extracts a clip) an ffmpeg storm.
func TestObjectRuleDetector_HonoursPersistedCooldownAfterRestart(t *testing.T) {
	newDetector := func() *ObjectRuleDetector {
		d := NewObjectRuleDetector(fakeObjectDetector{
			candidates: []ObjectCandidate{
				{Label: "car", Confidence: 0.9, Box: Box{X: 0.2, Y: 0.2, W: 0.2, H: 0.2}},
			},
		}, ObjectRuleDetectorOptions{})
		d.now = func() time.Time { return time.Unix(1_030, 0) }
		return d
	}

	rule := DetectionRule{
		Id:              4,
		CameraId:        9,
		DetectionType:   DetectionVehicle,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 60,
		IsEnabled:       true,
		// Persisted: this rule fired 30 seconds ago, before the process restarted.
		LastTriggeredAt: 1_000,
	}

	detections, err := newDetector().Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("rule re-fired %d time(s) despite being 30s into a 60s cooldown — the restart reset it", len(detections))
	}

	// Once the persisted cooldown has genuinely elapsed, it must fire again.
	past := newDetector()
	past.now = func() time.Time { return time.Unix(1_100, 0) }
	detections, err = past.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("expected the rule to fire once its cooldown elapsed, got %d detections", len(detections))
	}
}
