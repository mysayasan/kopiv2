# Module: apps/mymatasan/services/notification.go

## Purpose

Maps MyMataSan domain events (AI detection alerts, camera health transitions) into unified `domain/notification` notifications and publishes them through the hub.

## Responsibilities

- `NotifyVisionAlert(ctx, publisher, alert, cameraName, opts VisionAlertOptions)` — publish an actionable AI detection alert; skip diagnostic (sampling) alerts; no-op on nil publisher/alert.
- `buildVisionAlertNotification` — build the notification from the alert, applying the `VisionAlertOptions.Fields` inclusion config:
  - Rule name (when included) becomes the notification **Title**; otherwise it falls back to the detection label.
  - Each optional field (label, confidence, bounding box, zone polygon, snapshot path) is added to `Data` only when its toggle is on. Identifiers (`alertId`, `ruleId`, `cameraName`, `detectionType`) are always included.
  - When `IncludeSnapshot` is on and snapshot bytes are supplied, the JPEG is attached via `notification.Attachment` for media-capable outbound channels (Telegram photo, webhook base64). The attachment is non-persisted (`json:"-"`), so store/SSE/log channels skip it.
- `BuildAlertSnapshot(image, boundingBox, metadata, detectionType, fields)` — returns the snapshot bytes to attach: nil when the snapshot field is off; the raw image when the bounding-box field is off; otherwise the image with the detection box + object-label tag drawn on via `vision.AnnotateJPEG`, so the notification picture matches the AI Log detail overlay (which draws the box as a frontend overlay on the raw image).
- `NotifyCameraOffline` / `NotifyCameraRecovered` — publish camera health-check notifications with downtime context.

## LPR fields in rendered alerts

`renderVisionAlert` calls `plateInfoFromMetadata` to extract `plate`, `vehicleType`, `color`, and `watchlisted` from the alert's top-level metadata (promoted there by `infra/vision/object.go`). When `plate` is non-empty:
- If the label is not shown in the body (`!fields.IncludeLabel`), a `"• plate WXY1234 (white car)"` line is appended so the plate text always reaches text-only destinations (Telegram).
- `plate`, `watchlisted`, and (when present) `vehicleType` / `color` are added to the structured `data` map for webhook/MQTT consumers.
- Template context gains `{{plate}}`, `{{vehicleType}}`, `{{color}}`, and `{{watchlisted}}` tokens; on non-LPR alerts these resolve to the empty string.

## Notes

- `VisionAlertOptions` carries `RuleName`, `Snapshot []byte`, and `Fields *AlertNotificationSettings`. A nil `Fields` means "include everything" (matches the runtime default).
- The background monitor (`vision_monitor.go`) supplies the rule name, the captured frame bytes, and the live `vision.alertNotification` config. The manual create-alert API (`apis/vision.go`) achieves parity by reading the config from settings, resolving the rule name via `GetRules`, and loading the snapshot from `alert.SnapshotPath` on disk.
- The snapshot delivered to channels is the same downscaled JPEG the AI Log detail shows.
