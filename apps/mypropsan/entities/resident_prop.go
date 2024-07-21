package entities

// ResidentProp
type ResidentProp struct {
	Id            int64   `json:"id" form:"id" query:"id" params:"id" ignoreOnInsert:"true" pkey:"true" validate:"required"`
	Title         string  `json:"title" form:"title" query:"title" validate:"required"`
	Description   string  `json:"description" form:"description" query:"description"`
	CurrencyCode  string  `json:"currencyCode" form:"currencyCode" query:"currencyCode"`
	Price         float64 `json:"price" form:"price" query:"price"`
	PropType      int     `json:"propType" form:"propType" query:"propType" validate:"required"`
	PropTitle     int     `json:"propTitle" form:"propTitle" query:"propTitle"`
	LandTitle     int     `json:"landTitle" form:"landTitle" query:"landTitle"`
	LandTenure    int     `json:"landTenure" form:"landTenure" query:"landTenure"`
	BuiltUpSize   float32 `json:"builtUpSize" form:"builtUpSize" query:"builtUpSize"`
	LandAreaSize  float32 `json:"landAreaSize" form:"landAreaSize" query:"landAreaSize"`
	BedroomCount  int     `json:"bedroomCount" form:"bedroomCount" query:"bedroomCount"`
	BathroomCount int     `json:"bathroomCount" form:"bathroomCount" query:"bathroomCount"`
	CountryAbbrev string  `json:"countryAbbrev" form:"countryAbbrev" query:"countryAbbrev"`
	StateAbbrev   string  `json:"stateAbbrev" form:"stateAbbrev" query:"stateAbbrev"`
	Locode        string  `json:"locode" form:"locode" query:"locode"`
	Postcode      int     `json:"postcode" form:"postcode" query:"postcode"`
	Lat           float64 `json:"lat" form:"lat" query:"lat"`
	Lon           float64 `json:"lon" form:"lon" query:"lon"`
	PostedAt      int64   `json:"postedAt" form:"postedAt" query:"postedAt"`
	ExpiredAt     int64   `json:"expiredAt" form:"expiredAt" query:"expiredAt"`
	CreatedBy     int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt     int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy     int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt     int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
