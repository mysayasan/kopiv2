package entities

// ApiLog
type ApiLogEntity struct {
	Id             int64  `json:"id" form:"id" query:"id" autoinc:"true" validate:"required"`
	StatsCode      int    `json:"statsCode" form:"statsCode" query:"statsCode"`
	LogMsg         string `json:"logMsg" form:"logMsg" query:"logMsg"`
	ClientIpAddrV4 string `json:"clientIpAddrV4" form:"clientIpAddrV4" query:"clientIpAddrV4"`
	ClientIpAddrV6 string `json:"clientIpAddrV6" form:"clientIpAddrV6" query:"clientIpAddrV6"`
	RequestUrl     string `json:"requestUrl" form:"requestUrl" query:"requestUrl"`
	CreatedBy      int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt      int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy      int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt      int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
