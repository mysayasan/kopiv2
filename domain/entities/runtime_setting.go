package entities

// RuntimeSetting stores app settings that can be changed without restart.
type RuntimeSetting struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Key       string `json:"key" form:"key" query:"key" ukey:"key" validate:"required"`
	Value     string `json:"value" form:"value" query:"value"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
