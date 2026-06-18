package entities

// TrainingImage is one image in a training dataset, with its bounding-box
// annotations. Annotations is a JSON array of
// [{className, x, y, w, h, source:"auto"|"manual"}] in normalized 0..1 coords.
type TrainingImage struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	DatasetId   int64  `json:"datasetId" form:"datasetId" query:"datasetId" validate:"required"`
	FilePath    string `json:"filePath" form:"filePath" query:"filePath"`
	Width       int    `json:"width" form:"width" query:"width"`
	Height      int    `json:"height" form:"height" query:"height"`
	Source      string `json:"source" form:"source" query:"source"`
	Status      string `json:"status" form:"status" query:"status"`
	Annotations string `json:"annotations" form:"annotations" query:"annotations"`
	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
