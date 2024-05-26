package entity

// User
type User struct {
	Id        int64  `json:"id" form:"id" query:"id" autoinc:"true" validate:"required"`
	Email     string `json:"email" form:"email" query:"email" validate:"required"`
	Userpwd   string `json:"userpwd" form:"userpwd" query:"userpwd"`
	FirstName string `json:"firstName" form:"firstName" query:"firstName"`
	LastName  string `json:"lastName" form:"lastName" query:"lastName"`
	GroupId   string `json:"groupId" form:"groupId" query:"groupId"`
	RoleId    string `json:"roleId" form:"roleId" query:"roleId"`
	IsXtive   int32  `json:"isXtive" form:"isXtive" query:"isXtive"`
	CreatedBy string `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn int64  `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy string `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn int64  `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}
