package entities

// ResidentPropPic
type ResidentPropPic struct {
	Id             int64  `json:"id" form:"id" query:"id" validate:"required"`
	ResidentPropId int64  `json:"residentPropId" form:"residentPropId" query:"residentPropId"`
	PicUrl         string `json:"picUrl" form:"picUrl" query:"picUrl" validate:"required"`
	CreatedBy      int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt      int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy      int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt      int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
