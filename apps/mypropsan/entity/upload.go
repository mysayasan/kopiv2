package entity

// UploadEntity
type UploadEntity struct {
	Id          int64  `json:"id" form:"id" query:"id" validate:"required"`
	Title       string `json:"title" form:"title" query:"title" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	Guid        string `json:"guid" form:"guid" query:"guid"`
	MimeType    string `json:"mimeType" form:"mimeType" query:"mimeType"`
	Vpath       string `json:"vpath" form:"vpath" query:"vpath"`
	Sha1Chksum  string `json:"sha1Chksum" form:"sha1Chksum" query:"sha1Chksum"`
	ExpiredOn   int64  `json:"expiredOn" form:"expiredOn" query:"expiredOn"`
	CreatedBy   int    `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn   int64  `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy   int    `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn   int64  `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}
