package vision

import (
	"context"
	"fmt"
	"math"
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

// runDirectionalCross drives a track across a vertical line at x=0.5 from x1 to x2
// under the given direction and returns how many detections fired. For this line the
// positive signedArea (arrow) side is the left (x<0.5), so "forward" fires on a
// right->left crossing and "reverse" on a left->right crossing.
func runDirectionalCross(t *testing.T, direction string, x1, x2 float64) int {
	t.Helper()
	backend := &mutableObjectDetector{}
	detector := NewObjectRuleDetector(backend, ObjectRuleDetectorOptions{})
	now := time.Unix(100, 0)
	detector.now = func() time.Time { return now }

	rule := DetectionRule{
		Id: 21, CameraId: 3, DetectionType: DetectionLineCrossing,
		ZonePolygon: `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:  `{"classes":["person"],"direction":"` + direction + `","lines":[{"id":"gate","points":[[0.5,0.1],[0.5,0.9]]}]}`,
		Threshold:   0.5, MinFrames: 1, CooldownSeconds: 1, IsEnabled: true,
	}

	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(x1, 0.5)}}
	detector.Detect(context.Background(), Frame{CameraId: 3}, []DetectionRule{rule})
	now = now.Add(2 * time.Second)
	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(x2, 0.5)}}
	d, err := detector.Detect(context.Background(), Frame{CameraId: 3}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	return len(d)
}

func TestLineCrossingHonorsDirection(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		x1, x2    float64
		want      int
	}{
		{"forward blocks left->right", "forward", 0.4, 0.6, 0},
		{"reverse fires left->right", "reverse", 0.4, 0.6, 1},
		{"forward fires right->left", "forward", 0.6, 0.4, 1},
		{"reverse blocks right->left", "reverse", 0.6, 0.4, 0},
		{"both fires either way", "both", 0.4, 0.6, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runDirectionalCross(t, tc.direction, tc.x1, tc.x2); got != tc.want {
				t.Fatalf("direction=%s %.2f->%.2f fired=%d, want %d", tc.direction, tc.x1, tc.x2, got, tc.want)
			}
		})
	}
}

// crossLineAtAngle draws a line through the frame centre at lineAngleDeg (its axis)
// and drives an object from (x1,y1) to (x2,y2) through the real detector under the
// given direction, returning how many detections fired.
func crossLineAtAngle(t *testing.T, lineAngleDeg float64, direction string, x1, y1, x2, y2 float64) int {
	t.Helper()
	backend := &mutableObjectDetector{}
	detector := NewObjectRuleDetector(backend, ObjectRuleDetectorOptions{})
	now := time.Unix(100, 0)
	detector.now = func() time.Time { return now }

	rad := lineAngleDeg * math.Pi / 180
	h := 0.4 // half-length; endpoints stay within [0.1,0.9]
	ax, ay := 0.5-h*math.Cos(rad), 0.5-h*math.Sin(rad)
	bx, by := 0.5+h*math.Cos(rad), 0.5+h*math.Sin(rad)
	cfg := fmt.Sprintf(`{"classes":["person"],"direction":"%s","maxTrackDistance":0.95,"lines":[{"id":"gate","points":[[%g,%g],[%g,%g]]}]}`, direction, ax, ay, bx, by)
	rule := DetectionRule{
		Id: 41, CameraId: 5, DetectionType: DetectionLineCrossing,
		ZonePolygon: `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:  cfg,
		Threshold:   0.5, MinFrames: 1, CooldownSeconds: 1, IsEnabled: true,
	}

	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(x1, y1)}}
	detector.Detect(context.Background(), Frame{CameraId: 5}, []DetectionRule{rule})
	now = now.Add(2 * time.Second)
	backend.candidates = []ObjectCandidate{{Label: "person", Confidence: 0.9, Box: boxFromCenter(x2, y2)}}
	d, err := detector.Detect(context.Background(), Frame{CameraId: 5}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	return len(d)
}

func TestLineCrossingDirectionAtVariousAngles(t *testing.T) {
	// The object always travels horizontally (left↔right at y=0.5) across a line drawn
	// at each angle. The gate must (a) always fire "both", (b) fire exactly one of
	// forward/reverse per motion, and (c) FLIP which one when the motion reverses —
	// proving the side detection is correct at any line orientation, not just axis-aligned.
	for _, angle := range []float64{30, 70, 110, 160, 270} {
		fwdLR := crossLineAtAngle(t, angle, "forward", 0.15, 0.5, 0.85, 0.5)
		revLR := crossLineAtAngle(t, angle, "reverse", 0.15, 0.5, 0.85, 0.5)
		bothLR := crossLineAtAngle(t, angle, "both", 0.15, 0.5, 0.85, 0.5)
		fwdRL := crossLineAtAngle(t, angle, "forward", 0.85, 0.5, 0.15, 0.5)
		revRL := crossLineAtAngle(t, angle, "reverse", 0.85, 0.5, 0.15, 0.5)
		bothRL := crossLineAtAngle(t, angle, "both", 0.85, 0.5, 0.15, 0.5)
		t.Logf("line %3.0f°:  L→R fwd=%d rev=%d both=%d  |  R→L fwd=%d rev=%d both=%d", angle, fwdLR, revLR, bothLR, fwdRL, revRL, bothRL)

		if bothLR != 1 || bothRL != 1 {
			t.Fatalf("angle %.0f: both should fire either way, got L→R=%d R→L=%d", angle, bothLR, bothRL)
		}
		if fwdLR+revLR != 1 {
			t.Fatalf("angle %.0f: L→R must trigger exactly one of forward/reverse, got fwd=%d rev=%d", angle, fwdLR, revLR)
		}
		if fwdRL+revRL != 1 {
			t.Fatalf("angle %.0f: R→L must trigger exactly one of forward/reverse, got fwd=%d rev=%d", angle, fwdRL, revRL)
		}
		if fwdLR == fwdRL {
			t.Fatalf("angle %.0f: forward must fire for exactly ONE motion direction, got L→R=%d R→L=%d", angle, fwdLR, fwdRL)
		}
	}
}

func TestLineCrossingHorizontalMotionParallelToHorizontalLine(t *testing.T) {
	// A horizontal line (180°) with horizontal motion runs PARALLEL — the object never
	// crosses, so nothing must fire (no false trigger).
	if got := crossLineAtAngle(t, 180, "both", 0.15, 0.5, 0.85, 0.5); got != 0 {
		t.Fatalf("horizontal motion parallel to a horizontal line must not fire, got %d", got)
	}
}

func TestLineCrossingIgnoresJitterWithinBand(t *testing.T) {
	// A sub-band wobble straddling the line must not fire in either direction.
	if got := runDirectionalCross(t, "forward", 0.505, 0.495); got != 0 {
		t.Fatalf("forward jitter fired=%d, want 0", got)
	}
	if got := runDirectionalCross(t, "reverse", 0.495, 0.505); got != 0 {
		t.Fatalf("reverse jitter fired=%d, want 0", got)
	}
}

func boxFromCenter(x float64, y float64) Box {
	return Box{X: x - 0.05, Y: y - 0.05, W: 0.1, H: 0.1}
}
