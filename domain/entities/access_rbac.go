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
	// Managed marks a row DERIVED from the role's page grants (see services.DerivePermissions)
	// rather than typed in by hand under "Advanced".
	//
	// Re-deriving a role replaces only its managed rows. That is the whole reason the column
	// exists: without it, an administrator's hand-written exception would be silently destroyed
	// the next time somebody ticked a page, and they would have no way to know.
	Managed   bool  `json:"managed" form:"managed" query:"managed"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// AccessRolePage is a role holding one page at one access level — what an administrator actually
// chose in the UI. The permission matrix is derived from these rows, never edited directly for a
// page-managed role, so this table is the record of intent and access_role_permission is the
// record of enforcement. Table is access_role_page.
type AccessRolePage struct {
	Id     int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RoleId int64  `json:"roleId" form:"roleId" query:"roleId" validate:"required"`
	PageId string `json:"pageId" form:"pageId" query:"pageId" validate:"required"`
	// Level is the page level id ("view", "use", "manage"). Levels are cumulative.
	Level     string `json:"level" form:"level" query:"level" validate:"required"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
