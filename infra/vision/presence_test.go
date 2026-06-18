package vision

import (
	"context"
	"strings"
	"testing"
	"time"
)

func presenceRule(classesJSON string) DetectionRule {
	return DetectionRule{
		Id:              21,
		CameraId:        9,
		DetectionType:   DetectionPresence,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      classesJSON,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}
}

func TestPresenceMatchesExplicitTrainedClass(t *testing.T) {
	// "courier" is not in the default classMap; a presence rule must still match
	// it via its resolved ruleConfig.classes list.
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: []ObjectCandidate{
		{Label: "courier", Confidence: 0.9, Box: Box{X: 0.3, Y: 0.3, W: 0.1, H: 0.1}},
	}}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{presenceRule(`{"classes":["courier"]}`)})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 || !strings.Contains(detections[0].Label, "courier") {
		t.Fatalf("expected a courier presence detection, got %#v", detections)
	}
}

func TestPresenceStarMatchesAnyObject(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: []ObjectCandidate{
		{Label: "elephant", Confidence: 0.9, Box: Box{X: 0.3, Y: 0.3, W: 0.1, H: 0.1}},
	}}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{presenceRule(`{"classes":["*"]}`)})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("expected 1 detection for wildcard target, got %d", len(detections))
	}
}

func TestPresenceRejectsUnlistedClass(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: []ObjectCandidate{
		{Label: "dog", Confidence: 0.9, Box: Box{X: 0.3, Y: 0.3, W: 0.1, H: 0.1}},
	}}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{presenceRule(`{"classes":["person"]}`)})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("expected no detection (dog not in target list), got %d", len(detections))
	}
}

func TestLegacyObjectRuleStillUsesClassMap(t *testing.T) {
	// A legacy person rule has no ruleConfig.classes and must keep matching via
	// the static classMap.
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: []ObjectCandidate{
		{Label: "person", Confidence: 0.9, Box: Box{X: 0.3, Y: 0.3, W: 0.1, H: 0.1}},
	}}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	rule := DetectionRule{
		Id: 22, CameraId: 9, DetectionType: DetectionPerson,
		ZonePolygon: `[[0,0],[1,0],[1,1],[0,1]]`, Threshold: 0.5, MinFrames: 1, CooldownSeconds: 1, IsEnabled: true,
	}
	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("expected legacy person rule to fire, got %d", len(detections))
	}
}
