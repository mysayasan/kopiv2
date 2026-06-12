package entities

import apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"

// ApiEndpointRbac
type ApiEndpointRbac struct {
	Id            int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
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

// ApiEndpointRbacJoinModel
type ApiEndpointRbacJoinModel struct {
	Id            int64                     `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" tblalias:"table0" validate:"required"`
	ApiEndpointId int64                     `json:"apiEndpointId" form:"apiEndpointId" query:"apiEndpointId" ukey:"ukey1" fkey:"fkey1" tablejoin:"table1" validate:"required"`
	UserRoleId    int64                     `json:"userRoleId" form:"userRoleId" query:"userRoleId" ukey:"ukey1" fkey:"fkey2" validate:"required"`
	AppCode       string                    `json:"appCode" form:"appCode" query:"appCode" tblalias:"table1"`
	Host          string                    `json:"host" form:"host" query:"host" tblalias:"table1"`
	Path          string                    `json:"path" form:"path" query:"path" tblalias:"table1"`
	Metadata      string                    `json:"metadata" form:"metadata" query:"metadata" tblalias:"table1"`
	AccessTier    apiaccessenums.AccessTier `json:"accessTier" form:"accessTier" query:"accessTier" tblalias:"table1"`
	CanGet        bool                      `json:"canGet" form:"canGet" query:"canGet"`
	CanPost       bool                      `json:"canPost" form:"canPost" query:"canPost"`
	CanPut        bool                      `json:"canPut" form:"canPut" query:"canPut"`
	CanDelete     bool                      `json:"canDelete" form:"canDelete" query:"canDelete"`
	IsActive      bool                      `json:"isActive" form:"isActive" query:"isActive" tblalias:"table0"`
	// CreatedBy     int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt" tblalias:"table0"`
	// UpdatedBy     int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	// UpdatedAt     int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// ApiEndpointRbacListModel is an enriched administration projection for RBAC rows.
type ApiEndpointRbacListModel struct {
	Id               int64                     `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" tblalias:"table0" validate:"required"`
	ApiEndpointId    int64                     `json:"apiEndpointId" form:"apiEndpointId" query:"apiEndpointId" ukey:"ukey1" fkey:"fkey1" tablejoin:"table1" validate:"required"`
	UserRoleId       int64                     `json:"userRoleId" form:"userRoleId" query:"userRoleId" ukey:"ukey1" fkey:"fkey2" tablejoin:"table2" validate:"required"`
	EndpointTitle    string                    `json:"endpointTitle" form:"endpointTitle" query:"endpointTitle" tblalias:"table1" dbcol:"title"`
	EndpointAppCode  string                    `json:"endpointAppCode" form:"endpointAppCode" query:"endpointAppCode" tblalias:"table1" dbcol:"app_code"`
	EndpointHost     string                    `json:"endpointHost" form:"endpointHost" query:"endpointHost" tblalias:"table1" dbcol:"host"`
	EndpointPath     string                    `json:"endpointPath" form:"endpointPath" query:"endpointPath" tblalias:"table1" dbcol:"path"`
	EndpointMetadata string                    `json:"endpointMetadata" form:"endpointMetadata" query:"endpointMetadata" tblalias:"table1" dbcol:"metadata"`
	EndpointTier     apiaccessenums.AccessTier `json:"endpointTier" form:"endpointTier" query:"endpointTier" tblalias:"table1" dbcol:"access_tier"`
	RoleTitle        string                    `json:"roleTitle" form:"roleTitle" query:"roleTitle" tblalias:"table2" dbcol:"title"`
	CanGet           bool                      `json:"canGet" form:"canGet" query:"canGet" tblalias:"table0"`
	CanPost          bool                      `json:"canPost" form:"canPost" query:"canPost" tblalias:"table0"`
	CanPut           bool                      `json:"canPut" form:"canPut" query:"canPut" tblalias:"table0"`
	CanDelete        bool                      `json:"canDelete" form:"canDelete" query:"canDelete" tblalias:"table0"`
	IsActive         bool                      `json:"isActive" form:"isActive" query:"isActive" tblalias:"table0"`
	CreatedAt        int64                     `json:"createdAt" form:"createdAt" query:"createdAt" tblalias:"table0"`
}
