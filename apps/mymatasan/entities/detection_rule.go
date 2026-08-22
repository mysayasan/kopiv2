package entities

// DetectionRule stores one AI detection rule for a saved camera.
type DetectionRule struct {
	Id              int64   `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId        int64   `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required"`
	Name            string  `json:"name" form:"name" query:"name"`
	DetectionType   string  `json:"detectionType" form:"detectionType" query:"detectionType" validate:"required"`
	ZonePolygon     string  `json:"zonePolygon" form:"zonePolygon" query:"zonePolygon"`
	RuleConfig      string  `json:"ruleConfig" form:"ruleConfig" query:"ruleConfig"`
	SchedulePolicy  string  `json:"schedulePolicy" form:"schedulePolicy" query:"schedulePolicy"`
	Threshold       float64 `json:"threshold" form:"threshold" query:"threshold"`
	MinFrames       int     `json:"minFrames" form:"minFrames" query:"minFrames"`
	CooldownSeconds int     `json:"cooldownSeconds" form:"cooldownSeconds" query:"cooldownSeconds"`
	SoundEnabled    bool    `json:"soundEnabled" form:"soundEnabled" query:"soundEnabled"`
	// ArchiveClip asks the control plane to keep a copy of this rule's event clip and
	// snapshot, off the appliance.
	//
	// Per RULE, not per camera and not fleet-wide, because that is the granularity at
	// which "this matters" is actually known: a line-crossing rule on the perimeter gate
	// at night is evidence, and the same camera's daytime person-detection is noise.
	// Archiving everything would fill the control plane with footage nobody will ever
	// open and bury the events that matter inside it — so this defaults to FALSE and an
	// operator turns it on for the rules they would want to keep after the appliance is
	// stolen, burned, or simply wiped by whoever triggered the alert.
	//
	// The flag lives here rather than on the control plane because the fleet has no way
	// to know what a rule means; the person who wrote the rule does. It rides upstream on
	// each alert (notification Data "archiveClip"), which is how the control plane learns
	// to fetch the clip without having to read the node's rule table.
	ArchiveClip     bool    `json:"archiveClip" form:"archiveClip" query:"archiveClip"`
	IsEnabled       bool    `json:"isEnabled" form:"isEnabled" query:"isEnabled"`
	LastTriggeredAt int64   `json:"lastTriggeredAt" form:"lastTriggeredAt" query:"lastTriggeredAt"`
	CreatedBy       int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt       int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy       int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt       int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
