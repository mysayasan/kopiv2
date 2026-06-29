package entities

// ControlUser is a myseliasan control-plane user. Two kinds coexist:
//   - "local": the stock superadmin (username + bcrypt password, must-change on
//     first login) used only to bootstrap; can be disabled after handoff.
//   - "federated": a myidsan-authenticated user, auto-provisioned on first login
//     (keyed by SsoUserId) and assigned the viewer role.
//
// RoleId points at a ControlRole. Authorization keys on this user's role, resolved
// at login and stamped into the session. Table is control_user (avoids reserved
// word "user"). Uniqueness (username for local, SsoUserId for federated) is
// enforced by the service, not a DB unique key, since each is empty for the other
// kind.
type ControlUser struct {
	Id                 int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Kind               string `json:"kind" form:"kind" query:"kind"`
	Username           string `json:"username" form:"username" query:"username"`
	SsoUserId          int64  `json:"ssoUserId" form:"ssoUserId" query:"ssoUserId"`
	Email              string `json:"email" form:"email" query:"email"`
	Name               string `json:"name" form:"name" query:"name"`
	PasswordHash       string `json:"-" form:"passwordHash" query:"passwordHash"`
	RoleId             int64  `json:"roleId" form:"roleId" query:"roleId"`
	MustChangePassword bool   `json:"mustChangePassword" form:"mustChangePassword" query:"mustChangePassword"`
	Disabled           bool   `json:"disabled" form:"disabled" query:"disabled"`
	IsStock            bool   `json:"isStock" form:"isStock" query:"isStock"`
	LastLoginAt        int64  `json:"lastLoginAt" form:"lastLoginAt" query:"lastLoginAt"`
	CreatedAt          int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
