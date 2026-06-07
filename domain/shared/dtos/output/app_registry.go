package output

type AppRegistryDto struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Code        string `json:"code" form:"code" query:"code" ukey:"ukey1" validate:"required"`
	Title       string `json:"title" form:"title" query:"title" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	BaseUrl     string `json:"baseUrl" form:"baseUrl" query:"baseUrl"`
	Audience    string `json:"audience" form:"audience" query:"audience" ukey:"ukey2" validate:"required"`
	IsActive    bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
