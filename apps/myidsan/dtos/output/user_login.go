package output

// UserLoginDto is the READ projection of an account. It deliberately has NO
// password field: projection is destination-driven (domain/utils/dtos.Project
// copies only fields present here), so omitting it is what keeps the stored
// bcrypt hash out of every /api/user-credential response. Hashes must never
// leave the server — they enable offline cracking and would otherwise sit in
// browser caches, proxy logs and API logs. Writes use dtos/input.UserLoginDto,
// which does carry Userpwd.
type UserLoginDto struct {
	Id         int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Email      string `json:"email" form:"email" query:"email" ukey:"email" validate:"required"`
	FirstName  string `json:"firstName" form:"firstName" query:"firstName"`
	LastName   string `json:"lastName" form:"lastName" query:"lastName"`
	PicUrl     string `json:"picUrl" form:"picUrl" query:"picUrl"`
	UserRoleId         int64 `json:"userRoleId" form:"userRoleId" query:"userRoleId"`
	IsActive           bool  `json:"isActive" form:"isActive" query:"isActive"`
	MustChangePassword bool  `json:"mustChangePassword" form:"mustChangePassword" query:"mustChangePassword"`
	CreatedBy          int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy  int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt  int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
