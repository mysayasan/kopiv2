package vision

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func crowdPeople(n int) []ObjectCandidate {
	candidates := make([]ObjectCandidate, 0, n)
	for i := 0; i < n; i++ {
		candidates = append(candidates, ObjectCandidate{
			Label:      "person",
			Confidence: 0.9,
			Box:        Box{X: 0.1 + float64(i)*0.1, Y: 0.3, W: 0.05, H: 0.1},
		})
	}
	return candidates
}

func crowdRule() DetectionRule {
	return DetectionRule{
		Id:              7,
		CameraId:        9,
		DetectionType:   DetectionCrowd,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      `{"minCount":3}`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}
}

func TestCrowdFiresWhenCountMeetsMinimum(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: crowdPeople(3)}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{crowdRule()})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	if detections[0].DetectionType != DetectionCrowd {
		t.Fatalf("detectionType = %q", detections[0].DetectionType)
	}
	if !strings.Contains(detections[0].Label, "3 persons") {
		t.Fatalf("label = %q, want crowd count", detections[0].Label)
	}
	if !strings.Contains(detections[0].Metadata, `"crowdCount":3`) {
		t.Fatalf("metadata = %q, want crowdCount", detections[0].Metadata)
	}
}

func TestCrowdCarriesEveryBoxInMetadata(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: crowdPeople(3)}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{crowdRule()})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	var meta struct {
		Boxes []MetaBox `json:"boxes"`
	}
	if err := json.Unmarshal([]byte(detections[0].Metadata), &meta); err != nil {
		t.Fatalf("metadata unmarshal: %v", err)
	}
	if len(meta.Boxes) != 3 {
		t.Fatalf("metadata boxes = %d, want 3 (one per crowd member)", len(meta.Boxes))
	}
	// Each box carries its own label + confidence so the snapshot can tag it.
	for i, b := range meta.Boxes {
		if b.Label != "person" || b.Confidence <= 0 {
			t.Fatalf("box %d = %+v, want person label + positive confidence", i, b)
		}
	}
}

func TestCrowdDoesNotFireBelowMinimum(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: crowdPeople(2)}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{crowdRule()})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0 (below minCount)", len(detections))
	}
}

func TestCrowdDefaultMinCountIsTwo(t *testing.T) {
	rule := crowdRule()
	rule.RuleConfig = "" // no minCount -> default 2

	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: crowdPeople(2)}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1 (two people meets default minCount=2)", len(detections))
	}
}

func TestCrowdIgnoresOutOfZoneAndLowConfidence(t *testing.T) {
	candidates := crowdPeople(2) // two valid in-zone people
	// One person outside the zone polygon (centre well outside [0,0.4] box below).
	candidates = append(candidates, ObjectCandidate{Label: "person", Confidence: 0.9, Box: Box{X: 0.9, Y: 0.9, W: 0.05, H: 0.05}})
	// One low-confidence person inside the zone.
	candidates = append(candidates, ObjectCandidate{Label: "person", Confidence: 0.2, Box: Box{X: 0.2, Y: 0.2, W: 0.05, H: 0.05}})

	rule := crowdRule()
	rule.ZonePolygon = `[[0,0],[0.4,0],[0.4,0.4],[0,0.4]]`
	rule.RuleConfig = `{"minCount":3}`

	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: candidates}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0 (only 2 qualify, minCount=3)", len(detections))
	}
}

func TestValidateCrowdRuleRejectsMinCountBelowTwo(t *testing.T) {
	rule := crowdRule()
	rule.RuleConfig = `{"minCount":1}`
	if err := ValidateDetectionRule(rule); err == nil {
		t.Fatal("ValidateDetectionRule() = nil, want error for minCount < 2")
	}
}
