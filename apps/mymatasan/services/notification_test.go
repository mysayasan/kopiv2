package services

import (
	"context"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

type capturingPublisher struct {
	published []notification.Notification
}

func (c *capturingPublisher) Publish(_ context.Context, n notification.Notification) notification.Notification {
	c.published = append(c.published, n)
	return n
}

func TestNotifyVisionAlertPublishesActionableAlert(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{
		Id:            7,
		RuleId:        3,
		CameraId:      42,
		DetectionType: "intrusion",
		Label:         "person",
		Confidence:    0.91,
		BoundingBox:   "[0,0,10,10]",
		ZonePolygon:   "[[0,0],[1,1]]",
		SnapshotPath:  "/snap/7.jpg",
	}

	NotifyVisionAlert(context.Background(), pub, alert, "Front Door", VisionAlertOptions{RuleName: "Kitchen entry", Snapshot: []byte("jpegbytes")})

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published notification, got %d", len(pub.published))
	}
	n := pub.published[0]
	// Rule name (default fields = all on) becomes the title, lands in Data, and the
	// snapshot is attached for media-capable channels.
	if n.Title != "Kitchen entry" {
		t.Errorf("title = %q, want rule name", n.Title)
	}
	if n.Data["ruleName"] != "Kitchen entry" {
		t.Errorf("ruleName not carried in Data: %v", n.Data["ruleName"])
	}
	if n.Attachment == nil || string(n.Attachment.Data) != "jpegbytes" {
		t.Errorf("snapshot attachment missing or wrong: %+v", n.Attachment)
	}
	if n.Category != notification.CategoryVisionAlert {
		t.Errorf("category = %q, want %q", n.Category, notification.CategoryVisionAlert)
	}
	if n.Severity != notification.Critical {
		t.Errorf("severity = %q, want critical", n.Severity)
	}
	if n.CameraId != 42 || n.RefId != 7 || n.RefType != "alert_event" {
		t.Errorf("unexpected linkage: cameraId=%d refId=%d refType=%q", n.CameraId, n.RefId, n.RefType)
	}
	// The body and Data must carry the real camera name, not "Camera <id>".
	if !strings.Contains(n.Body, "Front Door") {
		t.Errorf("body should contain camera name, got %q", n.Body)
	}
	if n.Data["cameraName"] != "Front Door" {
		t.Errorf("cameraName not carried in Data: %v", n.Data["cameraName"])
	}
	if n.Data["boundingBox"] != "[0,0,10,10]" {
		t.Errorf("boundingBox not carried in Data: %v", n.Data["boundingBox"])
	}
	if n.Data["confidence"] != 0.91 {
		t.Errorf("confidence not carried in Data: %v", n.Data["confidence"])
	}
}

func TestNotifyVisionAlertRespectsFieldConfig(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{
		Id: 9, RuleId: 3, CameraId: 1, Label: "person", Confidence: 0.8,
		BoundingBox: "[0,0,5,5]", ZonePolygon: "[[0,0]]", SnapshotPath: "/snap/9.jpg",
	}
	// Only rule name + confidence enabled; snapshot, box, zone, label excluded.
	fields := &AlertNotificationSettings{IncludeRuleName: true, IncludeConfidence: true}

	NotifyVisionAlert(context.Background(), pub, alert, "Cam", VisionAlertOptions{
		RuleName: "Orang masuk dapur",
		Snapshot: []byte("jpeg"),
		Fields:   fields,
	})

	n := pub.published[0]
	if n.Title != "Orang masuk dapur" {
		t.Errorf("title = %q, want rule name", n.Title)
	}
	if _, ok := n.Data["boundingBox"]; ok {
		t.Error("boundingBox should be excluded")
	}
	if _, ok := n.Data["zonePolygon"]; ok {
		t.Error("zonePolygon should be excluded")
	}
	if _, ok := n.Data["label"]; ok {
		t.Error("label should be excluded")
	}
	if n.Data["confidence"] != 0.8 {
		t.Errorf("confidence should be included, got %v", n.Data["confidence"])
	}
	if n.Attachment != nil {
		t.Error("snapshot should not be attached when IncludeSnapshot is false")
	}
}

func TestNotifyVisionAlertFallsBackToCameraId(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{Id: 1, CameraId: 4, Label: "person"}

	NotifyVisionAlert(context.Background(), pub, alert, "", VisionAlertOptions{})

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published notification, got %d", len(pub.published))
	}
	if body := pub.published[0].Body; !strings.Contains(body, "Camera 4") {
		t.Errorf("empty camera name should fall back to \"Camera 4\", got %q", body)
	}
}

func TestCameraDisplayName(t *testing.T) {
	cases := []struct {
		name, model, host, want string
	}{
		{"Front Door", "DS-2CD", "10.0.0.1", "Front Door"},
		{"", "DS-2CD", "10.0.0.1", "DS-2CD"},
		{"", "", "10.0.0.1", "10.0.0.1"},
		{"  ", "", "", ""},
	}
	for _, tc := range cases {
		if got := CameraDisplayName(tc.name, tc.model, tc.host); got != tc.want {
			t.Errorf("CameraDisplayName(%q,%q,%q) = %q, want %q", tc.name, tc.model, tc.host, got, tc.want)
		}
	}
}

func TestNotifyVisionAlertSkipsDiagnostics(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{
		Id:       1,
		CameraId: 1,
		Metadata: `{"diagnostic":true,"status":"sampled"}`,
	}

	NotifyVisionAlert(context.Background(), pub, alert, "Front Door", VisionAlertOptions{})

	if len(pub.published) != 0 {
		t.Fatalf("expected diagnostic alert to be skipped, got %d published", len(pub.published))
	}
}

func TestNotifyVisionAlertNilSafe(t *testing.T) {
	// Nil publisher and nil alert must not panic.
	NotifyVisionAlert(context.Background(), nil, &entities.AlertEvent{Id: 1}, "", VisionAlertOptions{})
	NotifyVisionAlert(context.Background(), &capturingPublisher{}, nil, "", VisionAlertOptions{})
}

func TestIsDiagnosticMetadata(t *testing.T) {
	cases := map[string]bool{
		"":                     false,
		`{"diagnostic":true}`:  true,
		`{"diagnostic":false}`: false,
		`{"source":"vision"}`:  false,
		`not json`:             false,
		`{"diagnostic":"yes"}`: false, // non-bool is not diagnostic
	}
	for input, want := range cases {
		if got := isDiagnosticMetadata(input); got != want {
			t.Errorf("isDiagnosticMetadata(%q) = %v, want %v", input, got, want)
		}
	}
}
