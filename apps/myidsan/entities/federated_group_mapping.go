package entities

// FederatedGroupMapping maps a provider-side group (an AD group DN, later an OIDC
// groups-claim value) to a myidsan role. Matching is case-insensitive on GroupName;
// when several mappings match a login, the highest Priority wins (ties: lowest Id).
// A login matching NO mapping keeps the pending-clearance default — mappings only
// ever grant, never widen implicitly.
type FederatedGroupMapping struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Provider scopes the mapping ("ldap" today; "oidc:<key>" later).
	Provider string `json:"provider" form:"provider" query:"provider" validate:"required"`
	// GroupName is the provider's group identifier — for LDAP the full group DN as
	// delivered in memberOf.
	GroupName string `json:"groupName" form:"groupName" query:"groupName" validate:"required"`
	RoleId    int64  `json:"roleId" form:"roleId" query:"roleId" validate:"required"`
	Priority  int64  `json:"priority" form:"priority" query:"priority"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
