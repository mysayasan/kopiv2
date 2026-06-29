package entities

// NodeAccessGrant authorizes a myseliasan role to read and/or write a specific
// managed node over the command tunnel. read+write maps to admin on the node,
// read-only maps to viewer; write implies read (enforced on save). Grants are keyed
// per (role, node); a node's owning role (ManagedNode.OwnerRoleId) has full access
// without a grant, and a role with no grant and no ownership is denied.
type NodeAccessGrant struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RoleId    int64  `json:"roleId" form:"roleId" query:"roleId" validate:"required"`
	NodeId    string `json:"nodeId" form:"nodeId" query:"nodeId" validate:"required"`
	CanRead   bool   `json:"canRead" form:"canRead" query:"canRead"`
	CanWrite  bool   `json:"canWrite" form:"canWrite" query:"canWrite"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
