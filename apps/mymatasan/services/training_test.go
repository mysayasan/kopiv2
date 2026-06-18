package services

import (
	"math"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

func TestAnnotationFromAlertUsesObjectLabel(t *testing.T) {
	alert := &entities.AlertEvent{
		DetectionType: "presence",
		BoundingBox:   `{"x":0.1,"y":0.2,"w":0.3,"h":0.4}`,
		Metadata:      `{"objectLabel":"courier"}`,
	}
	ann := annotationFromAlert(alert)
	if ann == nil {
		t.Fatal("expected an annotation from a boxed alert")
	}
	if ann.ClassName != "courier" {
		t.Fatalf("className = %q, want courier (from metadata objectLabel)", ann.ClassName)
	}
	if ann.X != 0.1 || ann.Y != 0.2 || ann.W != 0.3 || ann.H != 0.4 {
		t.Fatalf("box not copied: %#v", ann)
	}
	if ann.Source != "auto" {
		t.Fatalf("source = %q, want auto", ann.Source)
	}
}

func TestAnnotationFromAlertFallsBackToDetectionType(t *testing.T) {
	alert := &entities.AlertEvent{
		DetectionType: "Person",
		BoundingBox:   `{"x":0,"y":0,"w":0.5,"h":0.5}`,
	}
	ann := annotationFromAlert(alert)
	if ann == nil || ann.ClassName != "person" {
		t.Fatalf("expected fallback to lowercased detectionType, got %#v", ann)
	}
}

func TestNormalizeAnnotationsClampsAndFilters(t *testing.T) {
	in := []TrainingAnnotation{
		{ClassName: " Person ", X: -0.1, Y: 0.2, W: 0.5, H: 0.3},          // clamp x to 0, lowercase+trim
		{ClassName: "courier", X: 0.8, Y: 0.1, W: 0.5, H: 0.2},            // w overflows -> clamped to 0.2
		{ClassName: "noise", X: 0.1, Y: 0.1, W: 0, H: 0.2},                // degenerate -> dropped
		{ClassName: "", X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Source: "manual"}, // empty class -> dropped
	}
	out := normalizeAnnotations(in, "auto")
	if len(out) != 2 {
		t.Fatalf("expected 2 valid annotations, got %d (%#v)", len(out), out)
	}
	if out[0].ClassName != "person" || out[0].X != 0 {
		t.Fatalf("first annotation not normalized: %#v", out[0])
	}
	if out[0].Source != "auto" {
		t.Fatalf("empty source should default to auto, got %q", out[0].Source)
	}
	// 0.8 + 0.5 overflows; width clamped so x+w == 1.
	if math.Abs((out[1].X+out[1].W)-1) > 1e-9 {
		t.Fatalf("overflowing width not clamped: x=%v w=%v", out[1].X, out[1].W)
	}
}

func TestAnnotationFromAlertNilWithoutBox(t *testing.T) {
	if annotationFromAlert(&entities.AlertEvent{DetectionType: "person"}) != nil {
		t.Fatal("expected nil annotation when alert has no bounding box")
	}
	// Zero-area box is not a usable label.
	if annotationFromAlert(&entities.AlertEvent{BoundingBox: `{"x":0,"y":0,"w":0,"h":0}`}) != nil {
		t.Fatal("expected nil annotation for a zero-area box")
	}
}
