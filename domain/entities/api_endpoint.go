package entities

import apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"

// ApiEndpoint
type ApiEndpoint struct {
	Id          int64                     `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Title       string                    `json:"title" form:"title" query:"title" validate:"required"`
	Description string                    `json:"description" form:"description" query:"description"`
	Metadata    string                    `json:"metadata" form:"metadata" query:"metadata"`
	AppCode     string                    `json:"appCode" form:"appCode" query:"appCode" ukey:"ukey1" validate:"required"`
	Host        string                    `json:"host" form:"host" query:"host" ukey:"ukey1" validate:"required"`
	Path        string                    `json:"path" form:"path" query:"path" ukey:"ukey1" validate:"required"`
	AccessTier  apiaccessenums.AccessTier `json:"accessTier" form:"accessTier" query:"accessTier"`
	IsActive    bool                      `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy   int64                     `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64                     `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64                     `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64                     `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
