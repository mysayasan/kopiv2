package entities

// ApiEndpointRbac
type ApiEndpointRbac struct {
	Id            int64 `json:"id" form:"id" query:"id" params:"id" ignoreOnInsert:"true" pkey:"true" validate:"required"`
	ApiEndpointId int64 `json:"apiEndpointId" form:"apiEndpointId" query:"apiEndpointId" ukey:"ukey1" fkey:"fkey1" validate:"required"`
	UserRoleId    int64 `json:"userRoleId" form:"userRoleId" query:"userRoleId" ukey:"ukey1" fkey:"fkey2" validate:"required"`
	CanGet        bool  `json:"canGet" form:"canGet" query:"canGet"`
	CanPost       bool  `json:"canPost" form:"canPost" query:"canPost"`
	CanPut        bool  `json:"canPut" form:"canPut" query:"canPut"`
	CanDelete     bool  `json:"canDelete" form:"canDelete" query:"canDelete"`
	IsActive      bool  `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy     int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt     int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy     int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt     int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// ApiEndpointRbacVwModel
type ApiEndpointRbacVwModel struct {
	Id            int64  `json:"id" form:"id" query:"id" params:"id" ignoreOnInsert:"true" pkey:"true" tblalias:"table0" validate:"required"`
	ApiEndpointId int64  `json:"apiEndpointId" form:"apiEndpointId" query:"apiEndpointId" ukey:"ukey1" fkey:"fkey1" tablejoin:"table1" validate:"required"`
	UserRoleId    int64  `json:"userRoleId" form:"userRoleId" query:"userRoleId" ukey:"ukey1" fkey:"fkey2" validate:"required"`
	Host          string `json:"host" form:"host" query:"host" tblalias:"table1"`
	Path          string `json:"path" form:"path" query:"path" tblalias:"table1"`
	CanGet        bool   `json:"canGet" form:"canGet" query:"canGet"`
	CanPost       bool   `json:"canPost" form:"canPost" query:"canPost"`
	CanPut        bool   `json:"canPut" form:"canPut" query:"canPut"`
	CanDelete     bool   `json:"canDelete" form:"canDelete" query:"canDelete"`
	IsActive      bool   `json:"isActive" form:"isActive" query:"isActive" tblalias:"table0"`
	// CreatedBy     int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt" tblalias:"table0"`
	// UpdatedBy     int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	// UpdatedAt     int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
