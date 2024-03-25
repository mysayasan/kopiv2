package entity

// ResidentPropPicEntity
type ResidentPropPicEntity struct {
	Id               int64   `json:"id" form:"id" query:"id" validate:"required"`
	Title            string  `json:"title" form:"title" query:"title" validate:"required"`
	Description      string  `json:"description" form:"description" query:"description"`
	PhysicalFilePath string  `json:"physicalFilePath" form:"physicalFilePath" query:"physicalFilePath"`
	VirtualFilePath  float64 `json:"virtualFilePath" form:"virtualFilePath" query:"virtualFilePath"`
	ExpiredOn        int64   `json:"expiredOn" form:"expiredOn" query:"expiredOn"`
	CreatedBy        int     `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn        int64   `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy        int     `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn        int64   `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}
