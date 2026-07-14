package entities

// NodeAccessGrant authorizes a myseliasan role on a specific managed node over the command
// tunnel.
//
// The three flags are an ESCALATION LADDER, and each rung names the role the tunnel asserts
// at the node:
//
//	CanRead     -> "viewer"    watch live, see that an alert fired
//	CanOperate  -> "operator"  + review footage, acknowledge alerts, PTZ, talk-back
//	CanWrite    -> "admin"     everything, including deleting footage
//
// Higher rungs imply the lower ones (enforced on save), so a grant is really a single
// choice, expressed as flags for backward compatibility with the rows already in the field.
//
// CanOperate is the rung that was missing. Without it a control-plane user was either a
// viewer or an admin at the node, so the node's whole role model collapsed to a binary the
// moment a command crossed the tunnel — and a fleet operator who should have been able to
// review footage but not delete it had to be given the power to delete it.
//
// Grants are keyed per (role, node). A node's owning role (ManagedNode.OwnerRoleId) has full
// access without a grant; a role with neither grant nor ownership is denied.
type NodeAccessGrant struct {
	Id     int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RoleId int64  `json:"roleId" form:"roleId" query:"roleId" validate:"required"`
	NodeId string `json:"nodeId" form:"nodeId" query:"nodeId" validate:"required"`
	// CanRead is the lowest rung: the node's "viewer".
	CanRead bool `json:"canRead" form:"canRead" query:"canRead"`
	// CanOperate is the middle rung: the node's "operator". An existing row has this false,
	// which is correct — it was granted before the rung existed, and nothing silently gains
	// a capability on upgrade.
	CanOperate bool `json:"canOperate" form:"canOperate" query:"canOperate"`
	// CanWrite is the top rung: the node's "admin".
	CanWrite  bool  `json:"canWrite" form:"canWrite" query:"canWrite"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
