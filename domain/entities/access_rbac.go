package entities

// AccessRole is one authorization role for the shared accessrbac core (used by any
// app that owns its own RBAC — myseliasan, myidsan, ...). A role flagged
// IsSuperadmin bypasses the permission matrix entirely. There is deliberately NO
// app_code dimension: each app has its own roles in its own database. Table is
// access_role (avoids the reserved word "role").
type AccessRole struct {
	Id           int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name         string `json:"name" form:"name" query:"name" ukey:"name" validate:"required"`
	Description  string `json:"description" form:"description" query:"description"`
	IsSuperadmin bool   `json:"isSuperadmin" form:"isSuperadmin" query:"isSuperadmin"`
	Builtin      bool   `json:"builtin" form:"builtin" query:"builtin"`
	CreatedBy    int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt    int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy    int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt    int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// AccessRolePermission grants a role access to an endpoint path per HTTP verb.
// Path is prefix-matched (longest match wins; no match = deny). Table is
// access_role_permission.
type AccessRolePermission struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RoleId    int64  `json:"roleId" form:"roleId" query:"roleId" validate:"required"`
	Path      string `json:"path" form:"path" query:"path" validate:"required"`
	CanGet    bool   `json:"canGet" form:"canGet" query:"canGet"`
	CanPost   bool   `json:"canPost" form:"canPost" query:"canPost"`
	CanPut    bool   `json:"canPut" form:"canPut" query:"canPut"`
	CanDelete bool   `json:"canDelete" form:"canDelete" query:"canDelete"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
