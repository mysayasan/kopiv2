package entities

// TeachSkill is one camera-taught detection skill created through the Teach
// wizard (e.g. "defective bottle cap" on the packing-line camera). It tracks the
// wizard's progress (Step) and lifecycle (Status: "draft" while the wizard is
// incomplete, then "collecting" → "trained" → "active" as later phases land).
// RoiPolygon uses the same normalized polygon format as DetectionRule.ZonePolygon
// so activation can copy it straight into the auto-created rule. DatasetId and
// ModelId link the backing training dataset/model once capture and training
// create them (0 until then). Sessions and Config are JSON blobs reserved for
// the capture sessions and derived training settings.
type TeachSkill struct {
	Id         int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId   int64  `json:"cameraId" form:"cameraId" query:"cameraId"`
	Name       string `json:"name" form:"name" query:"name" validate:"required"`
	SkillType  string `json:"skillType" form:"skillType" query:"skillType"`
	RoiPolygon string `json:"roiPolygon" form:"roiPolygon" query:"roiPolygon"`
	DatasetId  int64  `json:"datasetId" form:"datasetId" query:"datasetId"`
	ModelId    int64  `json:"modelId" form:"modelId" query:"modelId"`
	Status     string `json:"status" form:"status" query:"status"`
	Step       int    `json:"step" form:"step" query:"step"`
	Sessions   string `json:"sessions" form:"sessions" query:"sessions"`
	Config     string `json:"config" form:"config" query:"config"`
	CreatedBy  int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy  int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt  int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
