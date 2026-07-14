package entities

// LocalUser stores standalone mymatasan login credentials.
type LocalUser struct {
	Id           int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Username     string `json:"username" form:"username" query:"username" ukey:"username" validate:"required"`
	PasswordHash string `json:"-" form:"passwordHash" query:"passwordHash"`
	DisplayName  string `json:"displayName" form:"displayName" query:"displayName"`
	// RoleId is the authority on what this user may do — it points at an access_role, and
	// that role's permission matrix decides every request.
	//
	// It replaces IsAdmin, which was a single bool: admin got everything, everyone else got
	// every GET plus a hardcoded six-entry allow-list. "May watch cameras but may not delete
	// recordings" — the property that makes an NVR evidentiary rather than a camera viewer —
	// was not expressible at all.
	RoleId int64 `json:"roleId" form:"roleId" query:"roleId"`
	// IsAdmin is LEGACY and is no longer the authority. It is kept because existing rows
	// carry it and it is what the role backfill reads (see ILocalUserService.BackfillRoles).
	// Authorization reads RoleId; the AuthenticatedUser.IsAdmin the handlers see is DERIVED
	// from the role's IsSuperadmin flag, so there is exactly one source of truth.
	//
	// Once every install has been backfilled, this column can be dropped with a migration —
	// and until it is, the schema-drift check will not complain, because it is still declared.
	IsAdmin  bool `json:"isAdmin" form:"isAdmin" query:"isAdmin"`
	IsActive bool `json:"isActive" form:"isActive" query:"isActive"`
	// MustChangePassword forces the user through a password change before any
	// other route is reachable. Seeded true for the default admin so the shipped
	// credentials cannot remain in use.
	MustChangePassword bool  `json:"mustChangePassword" form:"mustChangePassword" query:"mustChangePassword"`
	LastLoginAt        int64 `json:"lastLoginAt" form:"lastLoginAt" query:"lastLoginAt"`
	CreatedBy          int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt          int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy          int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt          int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
