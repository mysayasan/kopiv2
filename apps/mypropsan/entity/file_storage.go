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
	ExpiredAt   int64  `json:"expiredAt" form:"expiredAt" query:"expiredAt"`
	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
