package entity

// User
type UserLoginEntity struct {
	Id        int64  `json:"id" form:"id" query:"id" autoinc:"true" validate:"required"`
	Email     string `json:"email" form:"email" query:"email" validate:"required"`
	Userpwd   string `json:"userpwd" form:"userpwd" query:"userpwd"`
	FirstName string `json:"firstName" form:"firstName" query:"firstName"`
	LastName  string `json:"lastName" form:"lastName" query:"lastName"`
	PicUrl    string `json:"picUrl" form:"picUrl" query:"picUrl"`
	GroupId   int32  `json:"groupId" form:"groupId" query:"groupId"`
	RoleId    int32  `json:"roleId" form:"roleId" query:"roleId"`
	IsActive  bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn int64  `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn int64  `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}
