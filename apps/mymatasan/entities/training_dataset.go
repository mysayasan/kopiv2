package entities

// TrainingDataset is a named collection of labeled images used to train a custom
// detection model. Classes is a JSON string array of the class slugs this
// dataset teaches.
type TrainingDataset struct {
	Id           int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name         string `json:"name" form:"name" query:"name" validate:"required"`
	Description  string `json:"description" form:"description" query:"description"`
	Classes      string `json:"classes" form:"classes" query:"classes"`
	ImageCount   int    `json:"imageCount" form:"imageCount" query:"imageCount"`
	LabeledCount int    `json:"labeledCount" form:"labeledCount" query:"labeledCount"`
	Status       string `json:"status" form:"status" query:"status"`
	CreatedBy    int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt    int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy    int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt    int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
