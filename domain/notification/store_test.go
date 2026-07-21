package notification

import (
	"encoding/json"
	"testing"

	infranotif "github.com/mysayasan/kopiv2/infra/notification"
)

func TestToEntitySerializesDataIntoMetadata(t *testing.T) {
	n := infranotif.Notification{
		Category: CategoryVisionAlert,
		Severity: Critical,
		Title:    "Detection: person",
		Body:     "Camera 42",
		Source:   "vision-monitor",
		CameraId: 42,
		RefType:  "alert_event",
		RefId:    7,
		Link:     "/api/vision/alerts/7/snapshot",
		Data: map[string]any{
			"boundingBox": "[0,0,10,10]",
			"confidence":  0.91,
		},
	}

	entity := toEntity(n)

	if entity.Category != CategoryVisionAlert || entity.Severity != string(Critical) {
		t.Fatalf("category/severity not mapped: %+v", entity)
	}
	if entity.CameraId != 42 || entity.RefId != 7 || entity.RefType != "alert_event" {
		t.Fatalf("linkage not mapped: %+v", entity)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(entity.Metadata), &parsed); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	if parsed["boundingBox"] != "[0,0,10,10]" {
		t.Errorf("boundingBox lost in metadata: %v", parsed["boundingBox"])
	}
}

func TestToEntityEmptyDataYieldsEmptyMetadata(t *testing.T) {
	entity := toEntity(infranotif.Notification{Title: "hi"})
	if entity.Metadata != "" {
		t.Errorf("expected empty metadata for nil Data, got %q", entity.Metadata)
	}
}

// The engine id must be folded into the persisted Metadata under OriginIDKey (alongside caller
// Data), so a cross-process replay dedups on the same key the live push carries.
func TestToEntityPersistsOriginId(t *testing.T) {
	entity := toEntity(infranotif.Notification{ID: "eng-9", Title: "t", Data: map[string]any{"x": 1}})
	var m map[string]any
	if err := json.Unmarshal([]byte(entity.Metadata), &m); err != nil {
		t.Fatalf("metadata not valid json: %v (%q)", err, entity.Metadata)
	}
	if m[OriginIDKey] != "eng-9" {
		t.Fatalf("origin id not persisted: %q", entity.Metadata)
	}
	if _, ok := m["x"]; !ok {
		t.Fatalf("caller data dropped: %q", entity.Metadata)
	}
}

// An id but no caller Data still persists just the origin id (so a replay can dedup it).
func TestToEntityOriginIdWithoutData(t *testing.T) {
	entity := toEntity(infranotif.Notification{ID: "eng-1", Title: "t"})
	var m map[string]any
	if err := json.Unmarshal([]byte(entity.Metadata), &m); err != nil {
		t.Fatalf("metadata not valid json: %v (%q)", err, entity.Metadata)
	}
	if m[OriginIDKey] != "eng-1" {
		t.Fatalf("origin id not persisted: %q", entity.Metadata)
	}
}
