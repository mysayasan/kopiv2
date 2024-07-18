package entities

// WebApi
type WebApi struct {
	Id          int64  `json:"id" form:"id" query:"id" ignoreOnInsert:"true" pkey:"true" validate:"required"`
	Title       string `json:"title" form:"title" query:"title" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	ApiPath     string `json:"apiPath" form:"apiPath" query:"apiPath" validate:"required"`
	IsActive    bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
