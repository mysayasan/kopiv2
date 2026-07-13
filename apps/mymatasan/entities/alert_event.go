package entities

// AlertEvent stores one detection alert raised by a camera rule.
//
// This is the highest-write table in the app: every rule, on every sampled frame, on
// every camera lands here — real detections plus "sampled"/"capture_failed" diagnostics.
// It is read by the alerts grid (filter by camera, newest first) and scanned by the
// retention purge (created_at < cutoff), so it carries two indexes:
//   - cam_time (camera_id, created_at) for the camera-scoped grid
//   - time     (created_at)            for the purge and the unfiltered grid sort
//
// Without them both degraded into full table scans within weeks of install, and the
// purge's scan-per-delete contended with the recorder's segment writes for the SQLite
// writer lock — which surfaced as failed segment saves, i.e. lost footage.
type AlertEvent struct {
	Id             int64   `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RuleId         int64   `json:"ruleId" form:"ruleId" query:"ruleId" validate:"required"`
	CameraId       int64   `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required" idx:"cam_time"`
	DetectionType  string  `json:"detectionType" form:"detectionType" query:"detectionType"`
	Label          string  `json:"label" form:"label" query:"label"`
	Confidence     float64 `json:"confidence" form:"confidence" query:"confidence"`
	ZonePolygon    string  `json:"zonePolygon" form:"zonePolygon" query:"zonePolygon"`
	BoundingBox    string  `json:"boundingBox" form:"boundingBox" query:"boundingBox"`
	SnapshotPath   string  `json:"snapshotPath" form:"snapshotPath" query:"snapshotPath"`
	Metadata       string  `json:"metadata" form:"metadata" query:"metadata"`
	IsDiagnostic   bool    `json:"isDiagnostic" form:"isDiagnostic" query:"isDiagnostic"`
	IsAcknowledged bool    `json:"isAcknowledged" form:"isAcknowledged" query:"isAcknowledged"`
	AcknowledgedBy int64   `json:"acknowledgedBy" form:"acknowledgedBy" query:"acknowledgedBy"`
	AcknowledgedAt int64   `json:"acknowledgedAt" form:"acknowledgedAt" query:"acknowledgedAt"`
	CreatedBy      int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt      int64   `json:"createdAt" form:"createdAt" query:"createdAt" idx:"cam_time,time"`
	UpdatedBy      int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt      int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
