package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
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
	label := alertBoxLabel(metadata, detectionType)
	// Crowd alerts carry every qualifying box in metadata; draw them all so the
	// snapshot outlines each member, each tagged with its own confidence.
	if boxes := boxesFromMetadata(metadata); len(boxes) > 0 {
		annotated := make([]vision.AnnotatedBox, 0, len(boxes))
		for _, b := range boxes {
			annotated = append(annotated, vision.AnnotatedBox{Box: b.Box, Label: boxLabel(b, label)})
		}
		return vision.AnnotateJPEG(image, annotated, 85)
	}
	if strings.TrimSpace(boundingBox) == "" {
		return image
	}
	var box vision.Box
	if err := json.Unmarshal([]byte(boundingBox), &box); err != nil {
		return image
	}
	return vision.AnnotateJPEG(image, []vision.AnnotatedBox{{Box: box, Label: label}}, 85)
}

// boxesFromMetadata extracts the optional "boxes" list an alert's metadata carries
// (crowd alerts record every qualifying box with its label + confidence), or nil
// when absent/unparseable.
func boxesFromMetadata(metadata string) []vision.MetaBox {
	if strings.TrimSpace(metadata) == "" {
		return nil
	}
	var parsed struct {
		Boxes []vision.MetaBox `json:"boxes"`
	}
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
		return nil
	}
	return parsed.Boxes
}

// boxLabel renders a per-box tag from its own label + confidence (e.g.
// "Person 92%"), falling back to the alert's label when the box has none.
func boxLabel(b vision.MetaBox, fallback string) string {
	name := strings.TrimSpace(b.Label)
	if name == "" {
		return fallback
	}
	name = strings.ToUpper(name[:1]) + name[1:]
	if b.Confidence > 0 {
		return fmt.Sprintf("%s %d%%", name, int(math.Round(b.Confidence*100)))
	}
	return name
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

// plateInfoFromMetadata extracts the license-plate fields the LPR detector
// promotes to an alert's top-level metadata. All are "" when absent (non-LPR
// alerts), so callers can simply test plate != "".
func plateInfoFromMetadata(metadata string) (plate, vehicleType, color string, watchlisted bool) {
	if strings.TrimSpace(metadata) == "" {
		return "", "", "", false
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
		return "", "", "", false
	}
	str := func(key string) string {
		if v, ok := parsed[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	wl, _ := parsed["watchlisted"].(bool)
	return str("plate"), str("vehicleType"), str("color"), wl
}

// INotificationPublisher is the slice of the notification service the app uses
// to emit events. The domain *notification.Service satisfies it. Publish stores +
// streams + fans to destinations (generic path); DeliverTo sends a tailored
// payload to one destination only (per-destination vision-alert path).
type INotificationPublisher interface {
	Publish(ctx context.Context, n notification.Notification) notification.Notification
	DeliverTo(ctx context.Context, destinationId string, n notification.Notification)
}

// VisionAlertOptions carries the extra per-alert context the notification needs
// beyond the persisted AlertEvent: the triggering rule's name, the RAW snapshot
// frame (each destination renders its own annotated/cropped copy via
// BuildAlertSnapshot using its own field config), and the delivery destinations.
// Fields is the legacy/global default used for the stored canonical notification
// (nil = include all). Destinations drives per-destination tailored delivery.
type VisionAlertOptions struct {
	RuleName string
	RawImage []byte
	Fields   *AlertNotificationSettings
	// Destinations is the full set of configured delivery destinations.
	Destinations []NotificationDestination
	// RuleDestinations restricts delivery to these destination ids (per-rule
	// routing). Empty/nil means the rule routes to ALL destinations.
	RuleDestinations []string
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
	ruleName := strings.TrimSpace(opts.RuleName)

	// One canonical notification is persisted + streamed (in-app feed) per alert,
	// rendered with all fields so the feed stays complete. Internal=true keeps it
	// out of outbound delivery — each destination receives its own tailored copy.
	canonical := renderVisionAlert(alert, cameraName, ruleName, defaultAlertNotificationSettings(), nil, nil, SnapshotModeInline)
	canonical.Internal = true
	publisher.Publish(ctx, canonical)

	// Deliver a separately rendered payload to each enabled destination that
	// subscribes to vision alerts: its own field toggles, its own snapshot
	// rendering, and its custom fields merged on top (custom wins).
	for _, d := range opts.Destinations {
		if !d.Enabled || !d.AllowsCategory(notification.CategoryVisionAlert) {
			continue
		}
		// Per-rule routing: when the rule names destinations, deliver only to those.
		if len(opts.RuleDestinations) > 0 && !containsString(opts.RuleDestinations, d.Id) {
			continue
		}
		n := renderVisionAlert(alert, cameraName, ruleName, d.Fields, opts.RawImage, d.CustomFields, d.SnapshotMode)
		publisher.DeliverTo(ctx, d.Id, n)
	}
}

// containsString reports whether v is in list.
func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// renderVisionAlert maps an AlertEvent into a notification for one recipient. The
// fields config (nil = all-inclusive) decides which detection details land in the
// Data map and whether a snapshot is attached; rawImage is the raw frame that
// BuildAlertSnapshot annotates/strips per the same fields; customFields are merged
// last so a custom key overrides the built-in field of the same name (custom wins).
// snapshotMode "link" suppresses the image bytes (the payload still carries the
// snapshot link + path) so the recipient fetches the image itself.
func renderVisionAlert(alert *entities.AlertEvent, cameraName, ruleName string, fields *AlertNotificationSettings, rawImage []byte, customFields []NotificationCustomField, snapshotMode string) notification.Notification {
	if fields == nil {
		fields = defaultAlertNotificationSettings()
	}

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
	// License-plate alerts carry the plate (and vehicle type/color) in metadata. The
	// alert label for an LPR rule already IS the plate descriptor ("Plate WXY1234
	// (white car)"), so only add an explicit plate line when the label is NOT shown —
	// otherwise it would duplicate. Either way the plate stays in the structured Data
	// map below for webhook/MQTT consumers.
	plate, vehicleType, color, watchlisted := plateInfoFromMetadata(alert.Metadata)
	if plate != "" && !fields.IncludeLabel {
		descriptor := strings.TrimSpace(color + " " + vehicleType)
		if descriptor != "" {
			body = fmt.Sprintf("%s • plate %s (%s)", body, plate, descriptor)
		} else {
			body = fmt.Sprintf("%s • plate %s", body, plate)
		}
	}

	// Identifiers are always included so consumers can correlate and fetch detail.
	data := map[string]any{
		"alertId":       alert.Id,
		"ruleId":        alert.RuleId,
		"cameraName":    camera,
		"detectionType": alert.DetectionType,
	}
	if plate != "" {
		data["plate"] = plate
		data["watchlisted"] = watchlisted
		if vehicleType != "" {
			data["vehicleType"] = vehicleType
		}
		if color != "" {
			data["color"] = color
		}
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

	// Custom fields are merged last and OVERRIDE any built-in field of the same
	// key (custom wins). The UI disables the corresponding AI-field toggle so the
	// user can see the stock field has been bypassed. Values may use {{token}}
	// placeholders expanded from this alert's context. The expanded lines are also
	// appended to the body so they show in text-only channels (e.g. Telegram only
	// renders Title+Body, not the structured Data map a webhook receives).
	if lines := appendCustomFields(data, customFields, alertTemplateContext(alert, ruleName, camera)); len(lines) > 0 {
		body = body + "\n" + strings.Join(lines, "\n")
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
	// Render the snapshot for this recipient from the raw frame using its own
	// field config (annotated, raw, or omitted), and attach the bytes — unless the
	// destination wants link-only delivery, where the payload's link + snapshotPath
	// reference the image instead (no bytes embedded).
	if !strings.EqualFold(snapshotMode, SnapshotModeLink) {
		if snap := BuildAlertSnapshot(rawImage, alert.BoundingBox, alert.Metadata, alert.DetectionType, fields); len(snap) > 0 {
			n.Attachment = &notification.Attachment{
				Filename:    fmt.Sprintf("alert-%d.jpg", alert.Id),
				ContentType: "image/jpeg",
				Data:        snap,
			}
		}
	}
	return n
}

// appendCustomFields writes each static custom field into the payload data,
// overwriting any existing (built-in) key of the same name so custom fields take
// priority. Each value's {{token}} placeholders are expanded from ctx. It returns
// the expanded "key: value" lines (in order) so callers can also surface them in
// text-only channels. Empty keys are ignored.
func appendCustomFields(data map[string]any, customFields []NotificationCustomField, ctx map[string]string) []string {
	var lines []string
	for _, f := range customFields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			continue
		}
		val := expandTemplate(f.Value, ctx)
		data[key] = val
		lines = append(lines, key+": "+val)
	}
	return lines
}

// templateToken matches {{ token }} placeholders in a custom field value.
var templateToken = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// expandTemplate replaces {{token}} placeholders with values from ctx. Unknown
// tokens expand to empty so a stray placeholder never leaks into the payload.
func expandTemplate(value string, ctx map[string]string) string {
	if !strings.Contains(value, "{{") {
		return value
	}
	return templateToken.ReplaceAllStringFunc(value, func(match string) string {
		sub := templateToken.FindStringSubmatch(match)
		if len(sub) < 2 {
			return ""
		}
		return ctx[sub[1]]
	})
}

// alertTemplateContext builds the token values available to custom-field
// templates for a vision alert (e.g. {{ruleName}}, {{cameraName}}).
func alertTemplateContext(alert *entities.AlertEvent, ruleName, cameraName string) map[string]string {
	plate, vehicleType, color, watchlisted := plateInfoFromMetadata(alert.Metadata)
	// watchlisted is only meaningful on a plate alert; leave it empty otherwise so
	// the token resolves to nothing for non-LPR alerts.
	watch := ""
	if plate != "" {
		watch = fmt.Sprintf("%t", watchlisted)
	}
	return map[string]string{
		"ruleName":      ruleName,
		"cameraName":    cameraName,
		"label":         alert.Label,
		"detectionType": alert.DetectionType,
		"confidence":    fmt.Sprintf("%.0f%%", alert.Confidence*100),
		"alertId":       fmt.Sprintf("%d", alert.Id),
		"ruleId":        fmt.Sprintf("%d", alert.RuleId),
		"cameraId":      fmt.Sprintf("%d", alert.CameraId),
		// LPR tokens — empty for non-plate alerts.
		"plate":       plate,
		"vehicleType": vehicleType,
		"color":       color,
		"watchlisted": watch,
	}
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

// AuthLockoutInfo describes a triggered failed-login lockout for notification.
type AuthLockoutInfo struct {
	// Username is the account that was being targeted ("" if none supplied).
	Username string
	// IP is the source address that tripped the lockout.
	IP string
	// LockSeconds is how long the lockout lasts.
	LockSeconds int
}

// NotifyAuthLockout publishes a critical system event when repeated failed
// sign-ins trip a lockout, so the operator (and any subscribed webhook/Telegram
// destination) is warned of a possible break-in attempt. A nil publisher is a
// no-op.
func NotifyAuthLockout(ctx context.Context, publisher INotificationPublisher, info AuthLockoutInfo) {
	if publisher == nil {
		return
	}
	who := strings.TrimSpace(info.Username)
	if who == "" {
		who = "an unknown account"
	} else {
		who = fmt.Sprintf("%q", who)
	}
	source := strings.TrimSpace(info.IP)
	if source == "" {
		source = "an unknown address"
	}
	body := fmt.Sprintf("Repeated failed sign-ins for %s from %s — login locked for %s.", who, source, formatHealthDuration(int64(info.LockSeconds)))
	publisher.Publish(ctx, notification.Notification{
		Category: notification.CategorySystem,
		Severity: notification.Critical,
		Title:    "Failed login lockout",
		Body:     body,
		Source:   "local-auth",
		Data: map[string]any{
			"username":    strings.TrimSpace(info.Username),
			"ip":          source,
			"lockSeconds": info.LockSeconds,
		},
	})
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
