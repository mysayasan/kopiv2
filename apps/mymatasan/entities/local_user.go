package entities

// LocalUser stores standalone mymatasan login credentials.
type LocalUser struct {
	Id           int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Username     string `json:"username" form:"username" query:"username" ukey:"username" validate:"required"`
	PasswordHash string `json:"-" form:"passwordHash" query:"passwordHash"`
	DisplayName  string `json:"displayName" form:"displayName" query:"displayName"`
	IsAdmin      bool   `json:"isAdmin" form:"isAdmin" query:"isAdmin"`
	IsActive     bool   `json:"isActive" form:"isActive" query:"isActive"`
	LastLoginAt  int64  `json:"lastLoginAt" form:"lastLoginAt" query:"lastLoginAt"`
	CreatedBy    int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt    int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy    int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt    int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
