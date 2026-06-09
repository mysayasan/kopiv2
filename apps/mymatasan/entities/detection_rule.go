package entities

// DetectionRule stores one AI detection rule for a saved camera.
type DetectionRule struct {
	Id              int64   `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId        int64   `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required"`
	Name            string  `json:"name" form:"name" query:"name"`
	DetectionType   string  `json:"detectionType" form:"detectionType" query:"detectionType" validate:"required"`
	ZonePolygon     string  `json:"zonePolygon" form:"zonePolygon" query:"zonePolygon"`
	SchedulePolicy  string  `json:"schedulePolicy" form:"schedulePolicy" query:"schedulePolicy"`
	Threshold       float64 `json:"threshold" form:"threshold" query:"threshold"`
	MinFrames       int     `json:"minFrames" form:"minFrames" query:"minFrames"`
	CooldownSeconds int     `json:"cooldownSeconds" form:"cooldownSeconds" query:"cooldownSeconds"`
	SoundEnabled    bool    `json:"soundEnabled" form:"soundEnabled" query:"soundEnabled"`
	IsEnabled       bool    `json:"isEnabled" form:"isEnabled" query:"isEnabled"`
	LastTriggeredAt int64   `json:"lastTriggeredAt" form:"lastTriggeredAt" query:"lastTriggeredAt"`
	CreatedBy       int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt       int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy       int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt       int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
