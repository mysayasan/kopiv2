package services

import (
	"context"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

type deliveredItem struct {
	destId string
	n      notification.Notification
}

type capturingPublisher struct {
	published []notification.Notification
	delivered []deliveredItem
}

func (c *capturingPublisher) Publish(_ context.Context, n notification.Notification) notification.Notification {
	c.published = append(c.published, n)
	return n
}

func (c *capturingPublisher) DeliverTo(_ context.Context, destId string, n notification.Notification) {
	c.delivered = append(c.delivered, deliveredItem{destId: destId, n: n})
}

func TestNotifyVisionAlertStoresCanonicalAndDeliversPerDestination(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{
		Id: 7, RuleId: 3, CameraId: 42, DetectionType: "intrusion",
		Label: "person", Confidence: 0.91, BoundingBox: "[0,0,10,10]",
		ZonePolygon: "[[0,0],[1,1]]", SnapshotPath: "/snap/7.jpg",
	}
	// One destination with snapshot on but box off, so BuildAlertSnapshot returns
	// the raw frame unchanged (keeps the test independent of JPEG decoding).
	dest := NotificationDestination{
		Id: "d1", Name: "Hook", Type: DestinationTypeWebhook, Enabled: true,
		Fields: &AlertNotificationSettings{IncludeRuleName: true, IncludeLabel: true, IncludeConfidence: true, IncludeSnapshot: true},
	}

	NotifyVisionAlert(context.Background(), pub, alert, "Front Door", VisionAlertOptions{
		RuleName: "Kitchen entry", RawImage: []byte("jpegbytes"), Destinations: []NotificationDestination{dest},
	})

	// Exactly one canonical is stored/streamed, marked Internal so it is not
	// re-delivered outbound, with full fields and NO attachment (no raw frame).
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 canonical notification, got %d", len(pub.published))
	}
	canonical := pub.published[0]
	if !canonical.Internal {
		t.Error("canonical notification must be Internal (store-only)")
	}
	if canonical.Title != "Kitchen entry" || canonical.Data["ruleName"] != "Kitchen entry" {
		t.Errorf("canonical missing rule name: title=%q data=%v", canonical.Title, canonical.Data["ruleName"])
	}
	if canonical.Attachment != nil {
		t.Error("canonical must not carry an attachment")
	}
	if canonical.Data["cameraName"] != "Front Door" || !strings.Contains(canonical.Body, "Front Door") {
		t.Errorf("canonical missing camera name: data=%v body=%q", canonical.Data["cameraName"], canonical.Body)
	}

	// The destination receives its own tailored copy WITH the snapshot attached.
	if len(pub.delivered) != 1 || pub.delivered[0].destId != "d1" {
		t.Fatalf("expected 1 delivery to d1, got %+v", pub.delivered)
	}
	d := pub.delivered[0].n
	if d.Internal {
		t.Error("delivered copy must not be Internal")
	}
	if d.Attachment == nil || string(d.Attachment.Data) != "jpegbytes" {
		t.Errorf("delivered snapshot missing/wrong: %+v", d.Attachment)
	}
}

func TestNotifyVisionAlertPerDestinationFieldsAndCustomWins(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{
		Id: 9, RuleId: 3, CameraId: 1, Label: "person", Confidence: 0.8,
		BoundingBox: "[0,0,5,5]", ZonePolygon: "[[0,0]]", SnapshotPath: "/snap/9.jpg",
	}
	// Only rule name + confidence on; snapshot/box/zone/label off. Custom fields add
	// "site" and OVERRIDE the built-in "cameraName" (custom wins).
	dest := NotificationDestination{
		Id: "d1", Type: DestinationTypeWebhook, Enabled: true,
		Fields:       &AlertNotificationSettings{IncludeRuleName: true, IncludeConfidence: true},
		CustomFields: []NotificationCustomField{{Key: "site", Value: "Gate"}, {Key: "cameraName", Value: "OVERRIDDEN"}},
	}

	NotifyVisionAlert(context.Background(), pub, alert, "Cam", VisionAlertOptions{
		RuleName: "Orang masuk dapur", RawImage: []byte("jpeg"), Destinations: []NotificationDestination{dest},
	})

	if len(pub.delivered) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(pub.delivered))
	}
	d := pub.delivered[0].n
	if _, ok := d.Data["boundingBox"]; ok {
		t.Error("boundingBox should be excluded")
	}
	if _, ok := d.Data["label"]; ok {
		t.Error("label should be excluded")
	}
	if d.Data["confidence"] != 0.8 {
		t.Errorf("confidence should be included, got %v", d.Data["confidence"])
	}
	if d.Attachment != nil {
		t.Error("snapshot should not be attached when its field is off")
	}
	if d.Data["site"] != "Gate" {
		t.Errorf("custom field site missing: %v", d.Data["site"])
	}
	if d.Data["cameraName"] != "OVERRIDDEN" {
		t.Errorf("custom field should WIN over built-in cameraName, got %v", d.Data["cameraName"])
	}
}

func TestNotifyVisionAlertCustomFieldTemplating(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{Id: 5, RuleId: 2, CameraId: 8, Label: "person", Confidence: 0.93}
	dest := NotificationDestination{
		Id: "d1", Type: DestinationTypeWebhook, Enabled: true,
		CustomFields: []NotificationCustomField{
			{Key: "title", Value: "{{ruleName}} @ {{cameraName}}"},
			{Key: "note", Value: "{{label}} {{confidence}}"},
			{Key: "literal", Value: "no-tokens"},
			{Key: "unknown", Value: "x={{nope}}"},
		},
	}
	NotifyVisionAlert(context.Background(), pub, alert, "Front Gate", VisionAlertOptions{
		RuleName: "Loitering", Destinations: []NotificationDestination{dest},
	})
	d := pub.delivered[0].n
	if d.Data["title"] != "Loitering @ Front Gate" {
		t.Errorf("title template = %v", d.Data["title"])
	}
	if d.Data["note"] != "person 93%" {
		t.Errorf("note template = %v", d.Data["note"])
	}
	if d.Data["literal"] != "no-tokens" {
		t.Errorf("literal value changed: %v", d.Data["literal"])
	}
	if d.Data["unknown"] != "x=" {
		t.Errorf("unknown token should expand to empty: %v", d.Data["unknown"])
	}
	// Custom fields must also appear in the Body so text-only channels (Telegram)
	// render them, not just the structured Data a webhook receives.
	if !strings.Contains(d.Body, "title: Loitering @ Front Gate") {
		t.Errorf("custom field not surfaced in body: %q", d.Body)
	}
}

func TestNotifyVisionAlertSnapshotLinkMode(t *testing.T) {
	alert := &entities.AlertEvent{Id: 7, CameraId: 1, Label: "fire", SnapshotPath: "/snap/7.jpg"}
	raw := []byte("jpegbytes")

	// Inline (default): image bytes attached.
	pubInline := &capturingPublisher{}
	NotifyVisionAlert(context.Background(), pubInline, alert, "Cam", VisionAlertOptions{
		RawImage: raw, Destinations: []NotificationDestination{{Id: "d1", Type: DestinationTypeWebhook, Enabled: true, SnapshotMode: SnapshotModeInline}},
	})
	if pubInline.delivered[0].n.Attachment == nil {
		t.Error("inline mode should attach the snapshot bytes")
	}

	// Link: no bytes, but the reference (link + snapshotPath) is still present.
	pubLink := &capturingPublisher{}
	NotifyVisionAlert(context.Background(), pubLink, alert, "Cam", VisionAlertOptions{
		RawImage: raw, Destinations: []NotificationDestination{{Id: "d1", Type: DestinationTypeWebhook, Enabled: true, SnapshotMode: SnapshotModeLink}},
	})
	d := pubLink.delivered[0].n
	if d.Attachment != nil {
		t.Error("link mode must not attach snapshot bytes")
	}
	if d.Link == "" || d.Data["snapshotPath"] != "/snap/7.jpg" {
		t.Errorf("link mode should still carry the reference: link=%q path=%v", d.Link, d.Data["snapshotPath"])
	}
}

func TestNotifyVisionAlertPerRuleRouting(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{Id: 1, CameraId: 1, Label: "person"}
	dests := []NotificationDestination{
		{Id: "a", Type: DestinationTypeWebhook, Enabled: true},
		{Id: "b", Type: DestinationTypeWebhook, Enabled: true},
		{Id: "c", Type: DestinationTypeWebhook, Enabled: true},
	}
	// Rule routes only to a + c.
	NotifyVisionAlert(context.Background(), pub, alert, "Cam", VisionAlertOptions{
		Destinations: dests, RuleDestinations: []string{"a", "c"},
	})
	got := map[string]bool{}
	for _, d := range pub.delivered {
		got[d.destId] = true
	}
	if !got["a"] || !got["c"] || got["b"] || len(pub.delivered) != 2 {
		t.Fatalf("per-rule routing wrong, delivered to: %v", got)
	}
}

func TestNotifyVisionAlertSkipsByCategoryAndEnabled(t *testing.T) {
	alert := &entities.AlertEvent{Id: 1, CameraId: 4, Label: "person"}

	// Destination subscribed only to health: vision alert must not be delivered.
	pub := &capturingPublisher{}
	healthOnly := NotificationDestination{Id: "d1", Type: DestinationTypeWebhook, Enabled: true, Categories: []string{notification.CategoryHealthCheck}}
	NotifyVisionAlert(context.Background(), pub, alert, "Cam", VisionAlertOptions{Destinations: []NotificationDestination{healthOnly}})
	if len(pub.delivered) != 0 {
		t.Errorf("health-only destination must not receive vision alerts, got %d", len(pub.delivered))
	}
	if len(pub.published) != 1 {
		t.Errorf("canonical should still be stored once, got %d", len(pub.published))
	}

	// Disabled destination is skipped.
	pub2 := &capturingPublisher{}
	disabled := NotificationDestination{Id: "d1", Type: DestinationTypeWebhook, Enabled: false}
	NotifyVisionAlert(context.Background(), pub2, alert, "Cam", VisionAlertOptions{Destinations: []NotificationDestination{disabled}})
	if len(pub2.delivered) != 0 {
		t.Errorf("disabled destination must be skipped, got %d", len(pub2.delivered))
	}
}

func TestNotifyVisionAlertFallsBackToCameraId(t *testing.T) {
	pub := &capturingPublisher{}
	alert := &entities.AlertEvent{Id: 1, CameraId: 4, Label: "person"}

	NotifyVisionAlert(context.Background(), pub, alert, "", VisionAlertOptions{})

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 canonical notification, got %d", len(pub.published))
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
	alert := &entities.AlertEvent{Id: 1, CameraId: 1, Metadata: `{"diagnostic":true,"status":"sampled"}`}

	NotifyVisionAlert(context.Background(), pub, alert, "Front Door", VisionAlertOptions{
		Destinations: []NotificationDestination{{Id: "d1", Type: DestinationTypeWebhook, Enabled: true}},
	})

	if len(pub.published) != 0 || len(pub.delivered) != 0 {
		t.Fatalf("diagnostic alert must be skipped, got published=%d delivered=%d", len(pub.published), len(pub.delivered))
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
