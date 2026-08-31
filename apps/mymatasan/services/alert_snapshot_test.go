package services

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/mysayasan/kopiv2/infra/vision"
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

func TestBuildAlertSnapshotDrawsAllCrowdBoxes(t *testing.T) {
	img := tinyJPEG(t)
	primary := `{"x":0.1,"y":0.1,"w":0.2,"h":0.2}`
	// Crowd alert: metadata carries every qualifying box with label + confidence.
	crowdMeta := `{"objectLabel":"person","crowdCount":3,"boxes":[` +
		`{"x":0.1,"y":0.1,"w":0.2,"h":0.2,"label":"person","confidence":0.92},` +
		`{"x":0.4,"y":0.4,"w":0.2,"h":0.2,"label":"person","confidence":0.81},` +
		`{"x":0.7,"y":0.1,"w":0.2,"h":0.2,"label":"person","confidence":0.7}]}`

	drawn := BuildAlertSnapshot(img, primary, crowdMeta, "crowd", nil)
	if bytes.Equal(drawn, img) {
		t.Fatal("crowd snapshot should be annotated")
	}
	if _, err := jpeg.Decode(bytes.NewReader(drawn)); err != nil {
		t.Fatalf("annotated image not valid jpeg: %v", err)
	}

	// boxesFromMetadata returns every box; a single-box alert returns none.
	boxes := boxesFromMetadata(crowdMeta)
	if len(boxes) != 3 {
		t.Fatalf("boxesFromMetadata = %d, want 3", len(boxes))
	}
	if got := boxesFromMetadata(`{"objectLabel":"person"}`); len(got) != 0 {
		t.Fatalf("boxesFromMetadata (no boxes) = %d, want 0", len(got))
	}

	// Per-box label renders the box's own confidence; empty box falls back.
	if got := boxLabel(boxes[0], "Crowd"); got != "Person 92%" {
		t.Fatalf("boxLabel = %q, want %q", got, "Person 92%")
	}
	if got := boxLabel(vision.MetaBox{}, "Crowd"); got != "Crowd" {
		t.Fatalf("boxLabel fallback = %q, want %q", got, "Crowd")
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
// A face alert's box says WHO. The model's class name is "face", so before this the
// snapshot burned "Face" over a recognized person while the notification row beside it
// named them — and the picture is the copy that goes to Telegram, into a case export, and
// in front of somebody who never sees the row.
func TestAlertBoxLabelNamesThePerson(t *testing.T) {
	recognized := `{"objectLabel":"face","recognized":true,"personName":"Aisyah Rahman",` +
		`"personId":7,"faceConfidence":0.94}`
	if got := alertBoxLabel(recognized, "face"); got != "Aisyah Rahman" {
		t.Errorf("recognized face box = %q, want the person's name", got)
	}

	// A stranger is labelled, not left as the class name: "a face appeared and we do not
	// know whose" is a statement worth making.
	stranger := `{"objectLabel":"face","recognized":false,"personName":"","faceConfidence":0}`
	if got := alertBoxLabel(stranger, "face"); got != unknownFaceLabel {
		t.Errorf("unrecognized face box = %q, want %q", got, unknownFaceLabel)
	}

	// A name is drawn AS ENROLLED. Title-casing is right for a class name and wrong here:
	// it is what turns "van der Berg" into "Van Der Berg".
	lower := `{"objectLabel":"face","recognized":true,"personName":"van der Berg","faceConfidence":0.9}`
	if got := alertBoxLabel(lower, "face"); got != "van der Berg" {
		t.Errorf("enrolled name should not be re-cased, got %q", got)
	}

	// Everything that is not a face alert is untouched. `recognized` is the marker the
	// face detector writes and nothing else does; without it there is no identity to read.
	if got := alertBoxLabel(`{"objectLabel":"person"}`, "person"); got != "Person" {
		t.Errorf("non-face alert changed: %q", got)
	}
	// A recognized:true with an empty name is a stranger, not a nameless label.
	if _, ok := faceBoxLabel(`{"objectLabel":"person"}`); ok {
		t.Error("faceBoxLabel claimed a non-face alert")
	}
	if got, ok := faceBoxLabel(`{"recognized":true,"personName":"  "}`); !ok || got != unknownFaceLabel {
		t.Errorf("blank name = %q/%v, want %q/true", got, ok, unknownFaceLabel)
	}
}

// The identity is written onto the box only when there is ONE box to write it onto. A
// MetaBox carries a class label and a confidence, never a person, so labelling several
// boxes from a single top-level identity would name the wrong people.
func TestBuildAlertSnapshotFaceBoxIdentity(t *testing.T) {
	img := tinyJPEG(t)
	primary := `{"x":0.1,"y":0.1,"w":0.2,"h":0.2}`

	one := `{"objectLabel":"face","recognized":true,"personName":"Aisyah Rahman","faceConfidence":0.94,` +
		`"boxes":[{"x":0.1,"y":0.1,"w":0.2,"h":0.2,"label":"face","confidence":0.94}]}`
	if drawn := BuildAlertSnapshot(img, primary, one, "face", nil); bytes.Equal(drawn, img) {
		t.Fatal("single-box face snapshot should be annotated")
	}
	label, ok := faceBoxLabel(one)
	if !ok || label != "Aisyah Rahman" {
		t.Fatalf("single-box face label = %q/%v", label, ok)
	}

	// With several boxes the per-box labels stand.
	many := `{"objectLabel":"face","recognized":true,"personName":"Aisyah Rahman","faceConfidence":0.94,` +
		`"boxes":[{"x":0.1,"y":0.1,"w":0.2,"h":0.2,"label":"face","confidence":0.94},` +
		`{"x":0.5,"y":0.5,"w":0.2,"h":0.2,"label":"face","confidence":0.6}]}`
	boxes := boxesFromMetadata(many)
	if len(boxes) != 2 {
		t.Fatalf("boxesFromMetadata = %d, want 2", len(boxes))
	}
	if got := boxLabel(boxes[1], label); got != "Face 60%" {
		t.Fatalf("second box label = %q, want %q", got, "Face 60%")
	}
	if drawn := BuildAlertSnapshot(img, primary, many, "face", nil); bytes.Equal(drawn, img) {
		t.Fatal("multi-box face snapshot should be annotated")
	}
}
