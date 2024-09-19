package entities

// CameraStream
type CameraStream struct {
	Id             int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Url            string `json:"url" form:"url" query:"url" validate:"required"`
	StreamProtocol int    `json:"stream_protocol" form:"stream_protocol" query:"stream_protocol"`
	OutStreamFmt   int    `json:"out_stream_fmt" form:"out_stream_fmt" query:"out_stream_fmt"`
	AutoStart      bool   `json:"autoStart" form:"autoStart" query:"autoStart"`
	IsActive       bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy      int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt      int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy      int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt      int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
