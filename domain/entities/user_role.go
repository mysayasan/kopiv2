package entities

// UserRole
type UserRole struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" ignoreOnInsert:"true" pkey:"true" validate:"required"`
	Title       string `json:"title" form:"title" query:"title" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	ParentId    int64  `json:"parentId" form:"parentId" query:"parentId" validate:"required"`
	GroupId     int64  `json:"groupId" form:"groupId" query:"groupId" validate:"groupId"`
	IsActive    bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}