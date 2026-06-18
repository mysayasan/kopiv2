package entities

// TrainingModel is one custom model: either imported (a best.pt produced
// offline) or, in a later phase, trained in-app. A model belongs to a dataset
// (0 for a standalone import) and lists the classes it can detect.
type TrainingModel struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name      string `json:"name" form:"name" query:"name" validate:"required"`
	DatasetId int64  `json:"datasetId" form:"datasetId" query:"datasetId"`
	BaseModel string `json:"baseModel" form:"baseModel" query:"baseModel"`
	Epochs    int    `json:"epochs" form:"epochs" query:"epochs"`
	Imgsz     int    `json:"imgsz" form:"imgsz" query:"imgsz"`
	// Status: "imported", "pending", "running", "completed", "failed".
	Status string `json:"status" form:"status" query:"status"`
	// Progress is the training completion percentage (0-100) while running.
	Progress int `json:"progress" form:"progress" query:"progress"`
	// Classes is a JSON string array of the class slugs this model detects.
	Classes string `json:"classes" form:"classes" query:"classes"`
	// FilePath is the on-disk path to the model weights (best.pt).
	FilePath string `json:"filePath" form:"filePath" query:"filePath"`
	// Metrics is a JSON blob (e.g. {"map50":..,"map5095":..}).
	Metrics   string `json:"metrics" form:"metrics" query:"metrics"`
	IsActive  bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
