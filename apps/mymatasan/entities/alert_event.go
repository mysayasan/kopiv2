package entities

// AlertEvent stores one detection alert raised by a camera rule.
type AlertEvent struct {
	Id             int64   `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RuleId         int64   `json:"ruleId" form:"ruleId" query:"ruleId" validate:"required"`
	CameraId       int64   `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required"`
	DetectionType  string  `json:"detectionType" form:"detectionType" query:"detectionType"`
	Label          string  `json:"label" form:"label" query:"label"`
	Confidence     float64 `json:"confidence" form:"confidence" query:"confidence"`
	ZonePolygon    string  `json:"zonePolygon" form:"zonePolygon" query:"zonePolygon"`
	BoundingBox    string  `json:"boundingBox" form:"boundingBox" query:"boundingBox"`
	SnapshotPath   string  `json:"snapshotPath" form:"snapshotPath" query:"snapshotPath"`
	Metadata       string  `json:"metadata" form:"metadata" query:"metadata"`
	IsAcknowledged bool    `json:"isAcknowledged" form:"isAcknowledged" query:"isAcknowledged"`
	AcknowledgedBy int64   `json:"acknowledgedBy" form:"acknowledgedBy" query:"acknowledgedBy"`
	AcknowledgedAt int64   `json:"acknowledgedAt" form:"acknowledgedAt" query:"acknowledgedAt"`
	CreatedBy      int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt      int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy      int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt      int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
