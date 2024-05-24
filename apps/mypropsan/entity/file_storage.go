package entity

// FileStorageEntity
type FileStorageEntity struct {
	Id          int64  `json:"id" form:"id" query:"id" autoinc:"true" validate:"required"`
	Title       string `json:"title" form:"title" query:"title" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	Guid        string `json:"guid" form:"guid" query:"guid"`
	MimeType    string `json:"mimeType" form:"mimeType" query:"mimeType"`
	VrPath      string `json:"vrPath" form:"vrPath" query:"vrPath"`
	Sha1Chksum  string `json:"sha1Chksum" form:"sha1Chksum" query:"sha1Chksum"`
	SecurityLvl int32  `json:"securityLvl" form:"securityLvl" query:"securityLvl"`
	ExpiredOn   int64  `json:"expiredOn" form:"expiredOn" query:"expiredOn"`
	CreatedBy   string `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn   int64  `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy   string `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn   int64  `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}
