package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// BuildAlertSnapshot returns the snapshot image bytes to attach to an alert
// notification, honoring the field config:
//   - nil when the snapshot field is disabled or no image is available;
//   - the raw image when the bounding-box field is disabled;
//   - otherwise the image with the detection box + object label drawn on, matching
//     the AI Log detail overlay shown in the web UI.
//
// A nil fields argument means "include everything" (draw the box).
func BuildAlertSnapshot(image []byte, boundingBox, metadata, detectionType string, fields *AlertNotificationSettings) []byte {
	if len(image) == 0 {
		return nil
	}
	if fields != nil && !fields.IncludeSnapshot {
		return nil
	}
	if fields != nil && !fields.IncludeBoundingBox {
		return image
	}
	if strings.TrimSpace(boundingBox) == "" {
		return image
	}
	var box vision.Box
	if err := json.Unmarshal([]byte(boundingBox), &box); err != nil {
		return image
	}
	return vision.AnnotateJPEG(image, []vision.AnnotatedBox{{Box: box, Label: alertBoxLabel(metadata, detectionType)}}, 85)
}

// alertBoxLabel derives the box tag text: the detected object label from the
// alert metadata when present, otherwise the detection type, title-cased to match
// the UI overlay (e.g. "Person").
func alertBoxLabel(metadata, detectionType string) string {
	label := objectLabelFromMetadata(metadata)
	if label == "" {
		label = strings.ReplaceAll(detectionType, "_", " ")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

// objectLabelFromMetadata extracts the "objectLabel" string from an alert's
// metadata JSON, or "" when absent or unparseable.
func objectLabelFromMetadata(metadata string) string {
	if strings.TrimSpace(metadata) == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
		return ""
	}
	if label, ok := parsed["objectLabel"].(string); ok {
		return strings.TrimSpace(label)
	}
	return ""
}

// INotificationPublisher is the slice of the notification service the app uses
// to emit events. The domain *notification.Service satisfies it.
type INotificationPublisher interface {
	Publish(ctx context.Context, n notification.Notification) notification.Notification
}

// VisionAlertOptions carries the extra per-alert context the notification needs
// beyond the persisted AlertEvent: the triggering rule's name, the snapshot image
// bytes, and the field-inclusion config. Fields is nil-safe (nil = include all).
type VisionAlertOptions struct {
	RuleName string
	Snapshot []byte
	Fields   *AlertNotificationSettings
}

// NotifyVisionAlert publishes a notification for a persisted AI detection alert.
// cameraName is the human-readable camera name used in the notification body;
// when empty it falls back to "Camera <id>". opts supplies the rule name, the
// snapshot image, and which fields/media to include. Diagnostic (sampling) alerts
// are skipped so the notification feed only carries actionable events. A nil
// publisher or nil alert is a no-op.
func NotifyVisionAlert(ctx context.Context, publisher INotificationPublisher, alert *entities.AlertEvent, cameraName string, opts VisionAlertOptions) {
	if publisher == nil || alert == nil {
		return
	}
	if isDiagnosticMetadata(alert.Metadata) {
		return
	}
	publisher.Publish(ctx, buildVisionAlertNotification(alert, cameraName, opts))
}

// buildVisionAlertNotification maps an AlertEvent into a unified notification.
// The configured fields (opts.Fields, defaulting to all-inclusive) decide which
// detection details land in the Data map and whether the snapshot image is
// attached for media-capable channels.
func buildVisionAlertNotification(alert *entities.AlertEvent, cameraName string, opts VisionAlertOptions) notification.Notification {
	fields := opts.Fields
	if fields == nil {
		fields = defaultAlertNotificationSettings()
	}
	ruleName := strings.TrimSpace(opts.RuleName)

	label := alert.Label
	if label == "" {
		label = alert.DetectionType
	}
	// Title prefers the rule name (the "alarm" the user configured) when included,
	// otherwise the detection label.
	title := "Detection alert"
	if fields.IncludeRuleName && ruleName != "" {
		title = ruleName
	} else if label != "" {
		title = fmt.Sprintf("Detection: %s", label)
	}

	camera := strings.TrimSpace(cameraName)
	if camera == "" {
		camera = fmt.Sprintf("Camera %d", alert.CameraId)
	}
	body := camera
	if fields.IncludeLabel && alert.Label != "" {
		body = fmt.Sprintf("%s • %s", body, alert.Label)
	}
	if fields.IncludeConfidence && alert.Confidence > 0 {
		body = fmt.Sprintf("%s • %.0f%% confidence", body, alert.Confidence*100)
	}

	// Identifiers are always included so consumers can correlate and fetch detail.
	data := map[string]any{
		"alertId":       alert.Id,
		"ruleId":        alert.RuleId,
		"cameraName":    camera,
		"detectionType": alert.DetectionType,
	}
	if fields.IncludeRuleName && ruleName != "" {
		data["ruleName"] = ruleName
	}
	if fields.IncludeLabel {
		data["label"] = alert.Label
	}
	if fields.IncludeConfidence {
		data["confidence"] = alert.Confidence
	}
	if fields.IncludeBoundingBox {
		data["boundingBox"] = alert.BoundingBox
	}
	if fields.IncludeZonePolygon {
		data["zonePolygon"] = alert.ZonePolygon
	}
	if fields.IncludeSnapshot {
		data["snapshotPath"] = alert.SnapshotPath
	}

	n := notification.Notification{
		Category: notification.CategoryVisionAlert,
		Severity: notification.Critical,
		Title:    title,
		Body:     body,
		Source:   "vision-monitor",
		CameraId: alert.CameraId,
		RefType:  "alert_event",
		RefId:    alert.Id,
		Link:     fmt.Sprintf("/api/vision/alerts/%d/snapshot", alert.Id),
		Data:     data,
	}
	if fields.IncludeSnapshot && len(opts.Snapshot) > 0 {
		n.Attachment = &notification.Attachment{
			Filename:    fmt.Sprintf("alert-%d.jpg", alert.Id),
			ContentType: "image/jpeg",
			Data:        opts.Snapshot,
		}
	}
	return n
}

// NotifyCameraOffline publishes a critical notification when the health monitor
// detects a camera has gone offline. cameraName is the human-readable name; when
// empty it falls back to the camera's model/host, then "Camera <id>". A nil
// publisher or nil camera is a no-op.
func NotifyCameraOffline(ctx context.Context, publisher INotificationPublisher, cam *CameraDetail, cameraName string) {
	if publisher == nil || cam == nil {
		return
	}
	publisher.Publish(ctx, buildCameraHealthNotification(cam, cameraName, false, 0))
}

// NotifyCameraRecovered publishes an informational notification when a camera
// comes back online. offlineFor is the downtime in seconds (0 omits it).
func NotifyCameraRecovered(ctx context.Context, publisher INotificationPublisher, cam *CameraDetail, cameraName string, offlineFor int64) {
	if publisher == nil || cam == nil {
		return
	}
	publisher.Publish(ctx, buildCameraHealthNotification(cam, cameraName, true, offlineFor))
}

// buildCameraHealthNotification maps a camera reachability transition into a
// unified notification under the health-check category.
func buildCameraHealthNotification(cam *CameraDetail, cameraName string, recovered bool, offlineFor int64) notification.Notification {
	name := strings.TrimSpace(cameraName)
	if name == "" {
		name = CameraDisplayName(cam.Name, cam.Model, cam.Host)
	}
	if name == "" {
		name = fmt.Sprintf("Camera %d", cam.Camera.Id)
	}
	host := strings.TrimSpace(cam.Host)

	severity := notification.Critical
	title := "Camera offline"
	body := fmt.Sprintf("%s is offline", name)
	status := "offline"
	if recovered {
		severity = notification.Info
		title = "Camera back online"
		body = fmt.Sprintf("%s is back online", name)
		if offlineFor > 0 {
			body = fmt.Sprintf("%s (offline for %s)", body, formatHealthDuration(offlineFor))
		}
		status = "online"
	} else if host != "" {
		body = fmt.Sprintf("%s (%s)", body, host)
	}

	return notification.Notification{
		Category: notification.CategoryHealthCheck,
		Severity: severity,
		Title:    title,
		Body:     body,
		Source:   "camera-health-monitor",
		CameraId: cam.Camera.Id,
		RefType:  "camera",
		RefId:    cam.Camera.Id,
		Data: map[string]any{
			"cameraId":   cam.Camera.Id,
			"cameraName": name,
			"host":       host,
			"status":     status,
			"offlineFor": offlineFor,
		},
	}
}

// formatHealthDuration renders a downtime in seconds as a compact human string.
func formatHealthDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	d := time.Duration(seconds) * time.Second
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// CameraDisplayName returns the best human-readable name for a camera, trying
// name, then model, then host. Returns "" when none are set so callers can apply
// their own fallback.
func CameraDisplayName(name, model, host string) string {
	for _, value := range []string{name, model, host} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// isDiagnosticMetadata reports whether an alert's metadata marks it as a
// vision-monitor diagnostic sample (matching the frontend's actionable filter).
func isDiagnosticMetadata(metadata string) bool {
	if metadata == "" {
		return false
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
		return false
	}
	diagnostic, _ := parsed["diagnostic"].(bool)
	return diagnostic
}
