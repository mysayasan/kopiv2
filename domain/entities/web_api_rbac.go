package entities

// WebApiRbac
type WebApiRbac struct {
	Id         int64 `json:"id" form:"id" query:"id" ignoreOnInsert:"true" pkey:"true" validate:"required"`
	WebApiId   int64 `json:"webApiId" form:"webApiId" query:"webApiId" ukey:"true" validate:"required"`
	UserRoleId int64 `json:"userRoleId" form:"userRoleId" query:"userRoleId" ukey:"true" validate:"required"`
	CanGet     bool  `json:"canGet" form:"canGet" query:"canGet"`
	CanPost    bool  `json:"canPost" form:"canPost" query:"canPost"`
	CanPut     bool  `json:"canPut" form:"canPut" query:"canPut"`
	CanDelete  bool  `json:"canDelete" form:"canDelete" query:"canDelete"`
	IsActive   bool  `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy  int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy  int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt  int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
