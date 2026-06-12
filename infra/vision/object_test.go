package vision

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeObjectDetector struct {
	candidates []ObjectCandidate
}

func (d fakeObjectDetector) DetectObjects(ctx context.Context, frame Frame) ([]ObjectCandidate, error) {
	return d.candidates, nil
}

func TestObjectRuleDetectorMapsVehicleCandidateToRule(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{
		candidates: []ObjectCandidate{
			{
				Label:      "car",
				Confidence: 0.88,
				Box:        Box{X: 0.2, Y: 0.2, W: 0.2, H: 0.2},
			},
		},
	}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	rule := DetectionRule{
		Id:              4,
		CameraId:        9,
		DetectionType:   DetectionVehicle,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	if detections[0].DetectionType != DetectionVehicle || !strings.Contains(detections[0].Label, "car") {
		t.Fatalf("unexpected detection = %#v", detections[0])
	}
	if !strings.Contains(detections[0].BoundingBox, `"x":0.2`) {
		t.Fatalf("boundingBox = %q", detections[0].BoundingBox)
	}
}

func TestObjectRuleDetectorMapsAnimalCandidateToRule(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{
		candidates: []ObjectCandidate{
			{
				Label:      "dog",
				Confidence: 0.82,
				Box:        Box{X: 0.3, Y: 0.3, W: 0.2, H: 0.2},
			},
		},
	}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }

	rule := DetectionRule{
		Id:              5,
		CameraId:        9,
		DetectionType:   DetectionAnimal,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 30,
		IsEnabled:       true,
	}

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	if detections[0].DetectionType != DetectionAnimal || !strings.Contains(detections[0].Label, "dog") {
		t.Fatalf("unexpected detection = %#v", detections[0])
	}
}

func TestObjectRuleDetectorRejectsCandidateOutsideZone(t *testing.T) {
	detector := NewObjectRuleDetector(fakeObjectDetector{
		candidates: []ObjectCandidate{
			{
				Label:      "person",
				Confidence: 0.95,
				Box:        Box{X: 0.7, Y: 0.2, W: 0.1, H: 0.1},
			},
		},
	}, ObjectRuleDetectorOptions{})

	rule := DetectionRule{
		Id:            1,
		CameraId:      9,
		DetectionType: DetectionPerson,
		ZonePolygon:   `[[0,0],[0.5,0],[0.5,1],[0,1]]`,
		Threshold:     0.5,
		MinFrames:     1,
		IsEnabled:     true,
	}

	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0", len(detections))
	}
}

func TestParseObjectCandidatesAcceptsWrappedDetections(t *testing.T) {
	candidates, err := parseObjectCandidates(strings.NewReader(`{"detections":[{"label":"Person","confidence":0.92,"box":{"x":0.1,"y":0.2,"w":0.3,"h":0.4}}]}`))
	if err != nil {
		t.Fatalf("parseObjectCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].Label != DetectionPerson || candidates[0].Confidence != 0.92 {
		t.Fatalf("candidate = %#v", candidates[0])
	}
}

func TestParseObjectCandidatesReportsWorkerError(t *testing.T) {
	_, err := parseObjectCandidates(strings.NewReader(`{"error":"model failed"}`))
	if err == nil || !strings.Contains(err.Error(), "model failed") {
		t.Fatalf("parseObjectCandidates() error = %v, want worker error", err)
	}
}

func TestNewPersistentObjectDetectorRequiresCommand(t *testing.T) {
	_, err := NewPersistentObjectDetector(PersistentObjectDetectorOptions{})
	if err == nil {
		t.Fatalf("NewPersistentObjectDetector() error = nil, want command error")
	}
}
