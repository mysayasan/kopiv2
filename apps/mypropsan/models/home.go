package models

// ImageGalleryEntity
type ResidentPropPicModel struct {
	Id             int64  `json:"id" form:"id" query:"id" validate:"required"`
	ResidentPropId int64  `json:"residentPropId" form:"residentPropId" query:"residentPropId"`
	PicUrl         string `json:"picUrl" form:"picUrl" query:"picUrl" validate:"required"`
}

// ResidentPropModel
type ResidentPropModel struct {
	Id            int64                  `json:"id" form:"id" query:"id" validate:"required"`
	Title         string                 `json:"title" form:"title" query:"title" validate:"required"`
	Description   string                 `json:"description" form:"description" query:"description"`
	CurrencyCode  string                 `json:"currencyCode" form:"currencyCode" query:"currencyCode"`
	Price         float64                `json:"price" form:"price" query:"price"`
	PropType      int                    `json:"propType" form:"propType" query:"propType" validate:"required"`
	PropTitle     int                    `json:"propTitle" form:"propTitle" query:"propTitle"`
	LandTitle     int                    `json:"landTitle" form:"landTitle" query:"landTitle"`
	LandTenure    int                    `json:"landTenure" form:"landTenure" query:"landTenure"`
	BuiltUpSize   float32                `json:"builtUpSize" form:"builtUpSize" query:"builtUpSize"`
	LandAreaSize  float32                `json:"landAreaSize" form:"landAreaSize" query:"landAreaSize"`
	BedroomCount  int                    `json:"bedroomCount" form:"bedroomCount" query:"bedroomCount"`
	BathroomCount int                    `json:"bathroomCount" form:"bathroomCount" query:"bathroomCount"`
	CountryAbbrev string                 `json:"countryAbbrev" form:"countryAbbrev" query:"countryAbbrev"`
	StateAbbrev   string                 `json:"stateAbbrev" form:"stateAbbrev" query:"stateAbbrev"`
	Locode        string                 `json:"locode" form:"locode" query:"locode"`
	Postcode      int                    `json:"postcode" form:"postcode" query:"postcode"`
	Lat           float64                `json:"lat" form:"lat" query:"lat"`
	Lon           float64                `json:"lon" form:"lon" query:"lon"`
	PostedOn      int64                  `json:"postedOn" form:"postedOn" query:"postedOn"`
	ExpiredOn     int64                  `json:"expiredOn" form:"expiredOn" query:"expiredOn"`
	Pics          []ResidentPropPicModel `json:"pics" form:"pics" query:"pics" datasrc:"resident_prop_pic" parents:"Id:ResidentPropId"`
}
