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
