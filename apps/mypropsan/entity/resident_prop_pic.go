package entity

// ResidentPropPicEntity
type ResidentPropPicEntity struct {
	Id             int64  `json:"id" form:"id" query:"id" validate:"required"`
	ResidentPropId int64  `json:"residentPropId" form:"residentPropId" query:"residentPropId"`
	PicUrl         string `json:"picUrl" form:"picUrl" query:"picUrl" validate:"required"`
	CreatedBy      int    `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn      int64  `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy      int    `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn      int64  `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}
