package services

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 64)), nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestBuildAlertSnapshotGating(t *testing.T) {
	img := tinyJPEG(t)
	box := `{"x":0.1,"y":0.1,"w":0.3,"h":0.3}`
	meta := `{"objectLabel":"person"}`

	// Snapshot disabled -> nil.
	if got := BuildAlertSnapshot(img, box, meta, "person", &AlertNotificationSettings{IncludeSnapshot: false}); got != nil {
		t.Error("snapshot disabled should return nil")
	}

	// Snapshot on, bounding box off -> raw image unchanged.
	raw := BuildAlertSnapshot(img, box, meta, "person", &AlertNotificationSettings{IncludeSnapshot: true, IncludeBoundingBox: false})
	if !bytes.Equal(raw, img) {
		t.Error("box disabled should return the original image")
	}

	// Snapshot + box on -> annotated (differs from source, still valid jpeg).
	drawn := BuildAlertSnapshot(img, box, meta, "person", &AlertNotificationSettings{IncludeSnapshot: true, IncludeBoundingBox: true})
	if bytes.Equal(drawn, img) {
		t.Error("box enabled should annotate the image")
	}
	if _, err := jpeg.Decode(bytes.NewReader(drawn)); err != nil {
		t.Errorf("annotated image not valid jpeg: %v", err)
	}

	// Nil fields = include everything -> annotated.
	if got := BuildAlertSnapshot(img, box, meta, "person", nil); bytes.Equal(got, img) {
		t.Error("nil fields should annotate (include all)")
	}

	// Empty image -> nil.
	if got := BuildAlertSnapshot(nil, box, meta, "person", nil); got != nil {
		t.Error("empty image should return nil")
	}
}

func TestAlertBoxLabel(t *testing.T) {
	if got := alertBoxLabel(`{"objectLabel":"person"}`, "line_crossing"); got != "Person" {
		t.Errorf("objectLabel should win and be title-cased, got %q", got)
	}
	if got := alertBoxLabel("", "multi_line_crossing"); got != "Multi line crossing" {
		t.Errorf("fallback to detection type, got %q", got)
	}
}
