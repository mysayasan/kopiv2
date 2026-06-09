package vision

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

func TestMotionDetectorDetectsMotionInsideZone(t *testing.T) {
	detector := NewMotionDetector()
	detector.now = func() time.Time { return time.Unix(100, 0) }
	rule := DetectionRule{
		Id:              1,
		CameraId:        9,
		DetectionType:   DetectionIntrusion,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		Threshold:       0.01,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}

	first := testJPEG(t, false)
	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9, Data: first}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("first Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("first Detect() detections = %d, want 0", len(detections))
	}

	second := testJPEG(t, true)
	detections, err = detector.Detect(context.Background(), Frame{CameraId: 9, Data: second}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("second Detect() error = %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("second Detect() detections = %d, want 1", len(detections))
	}
	if detections[0].RuleId != rule.Id || detections[0].CameraId != rule.CameraId {
		t.Fatalf("unexpected detection = %#v", detections[0])
	}
}

func TestMotionDetectorIgnoresMotionOutsideZone(t *testing.T) {
	detector := NewMotionDetector()
	rule := DetectionRule{
		Id:            1,
		CameraId:      9,
		DetectionType: DetectionIntrusion,
		ZonePolygon:   `[[0,0],[0.4,0],[0.4,1],[0,1]]`,
		Threshold:     0.01,
		MinFrames:     1,
		IsEnabled:     true,
	}

	_, _ = detector.Detect(context.Background(), Frame{CameraId: 9, Data: testJPEG(t, false)}, []DetectionRule{rule})
	detections, err := detector.Detect(context.Background(), Frame{CameraId: 9, Data: testJPEG(t, true)}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0", len(detections))
	}
}

func testJPEG(t *testing.T, withMotion bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 20, B: 20, A: 255})
		}
	}
	if withMotion {
		for y := 20; y < 42; y++ {
			for x := 48; x < 70; x++ {
				img.Set(x, y, color.RGBA{R: 230, G: 230, B: 230, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}
	return buf.Bytes()
}
