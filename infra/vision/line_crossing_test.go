package vision

import (
	"context"
	"strings"
	"testing"
	"time"
)

type mutableObjectDetector struct {
	candidates []ObjectCandidate
}

func (d *mutableObjectDetector) DetectObjects(ctx context.Context, frame Frame) ([]ObjectCandidate, error) {
	return d.candidates, nil
}

func TestLineCrossingDetectorTriggersOnSingleLineCross(t *testing.T) {
	backend := &mutableObjectDetector{}
	detector := NewObjectRuleDetector(backend, ObjectRuleDetectorOptions{})
	now := time.Unix(100, 0)
	detector.now = func() time.Time { return now }

	rule := DetectionRule{
		Id:              11,
		CameraId:        7,
		DetectionType:   DetectionLineCrossing,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      `{"classes":["person"],"lines":[{"id":"gate","points":[[0.5,0.1],[0.5,0.9]]}]}`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}

	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(0.4, 0.5)}}
	detections, err := detector.Detect(context.Background(), Frame{CameraId: 7}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("first detections = %d, want 0", len(detections))
	}

	now = now.Add(2 * time.Second)
	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(0.6, 0.5)}}
	detections, err = detector.Detect(context.Background(), Frame{CameraId: 7}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	if detections[0].DetectionType != DetectionLineCrossing || !strings.Contains(detections[0].Metadata, `"lineId":"gate"`) {
		t.Fatalf("unexpected detection = %#v", detections[0])
	}
}

func TestMultiLineCrossingRequiresConfiguredSequence(t *testing.T) {
	backend := &mutableObjectDetector{}
	detector := NewObjectRuleDetector(backend, ObjectRuleDetectorOptions{})
	now := time.Unix(200, 0)
	detector.now = func() time.Time { return now }

	rule := DetectionRule{
		Id:              12,
		CameraId:        8,
		DetectionType:   DetectionMultiLineCrossing,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      `{"classes":["person"],"lines":[{"id":"start","points":[[0.4,0.1],[0.4,0.9]]},{"id":"end","points":[[0.6,0.1],[0.6,0.9]]}]}`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}

	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(0.3, 0.5)}}
	if detections, err := detector.Detect(context.Background(), Frame{CameraId: 8}, []DetectionRule{rule}); err != nil || len(detections) != 0 {
		t.Fatalf("initial Detect() detections = %d err = %v, want 0 nil", len(detections), err)
	}

	now = now.Add(2 * time.Second)
	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(0.45, 0.5)}}
	if detections, err := detector.Detect(context.Background(), Frame{CameraId: 8}, []DetectionRule{rule}); err != nil || len(detections) != 0 {
		t.Fatalf("start line Detect() detections = %d err = %v, want 0 nil", len(detections), err)
	}

	now = now.Add(2 * time.Second)
	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(0.65, 0.5)}}
	detections, err := detector.Detect(context.Background(), Frame{CameraId: 8}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	if detections[0].DetectionType != DetectionMultiLineCrossing || !strings.Contains(detections[0].Metadata, `"lineCount":2`) {
		t.Fatalf("unexpected detection = %#v", detections[0])
	}
}

func TestValidateLineCrossingRejectsTooManyLines(t *testing.T) {
	err := ValidateDetectionRule(DetectionRule{
		CameraId:        1,
		DetectionType:   DetectionMultiLineCrossing,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		RuleConfig: `{"lines":[
			{"points":[[0.1,0],[0.1,1]]},
			{"points":[[0.2,0],[0.2,1]]},
			{"points":[[0.3,0],[0.3,1]]},
			{"points":[[0.4,0],[0.4,1]]},
			{"points":[[0.5,0],[0.5,1]]},
			{"points":[[0.6,0],[0.6,1]]}
		]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "at most 5") {
		t.Fatalf("ValidateDetectionRule() error = %v, want max-lines error", err)
	}
}

func boxFromCenter(x float64, y float64) Box {
	return Box{X: x - 0.05, Y: y - 0.05, W: 0.1, H: 0.1}
}
