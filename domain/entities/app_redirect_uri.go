package entities

// AppRedirectUri stores exact callback URLs allowed for an app auth client.
type AppRedirectUri struct {
	Id              int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	AppAuthConfigId int64  `json:"appAuthConfigId" form:"appAuthConfigId" query:"appAuthConfigId" ukey:"uri" validate:"required"`
	RedirectUri     string `json:"redirectUri" form:"redirectUri" query:"redirectUri" ukey:"uri" validate:"required"`
	IsActive        bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy       int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt       int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy       int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt       int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
