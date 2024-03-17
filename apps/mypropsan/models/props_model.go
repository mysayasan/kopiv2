package models

// ResidentialProp
type ResidentPropModel struct {
	Id            int64   `json:"id" form:"id" query:"id" validate:"required"`
	Title         string  `json:"title" form:"title" query:"title" validate:"required"`
	Description   string  `json:"description" form:"description" query:"description"`
	Price         float64 `json:"price" form:"price" query:"price"`
	CurrencyCode  string  `json:"currencyCode" form:"currencyCode" query:"currencyCode"`
	PropType      int     `json:"propType" form:"propType" query:"propType" validate:"required"`
	PropTitle     int     `json:"propTitle" form:"propTitle" query:"propTitle"`
	LandTitle     int     `json:"landTitle" form:"landTitle" query:"landTitle"`
	LandTenure    int     `json:"landTenure" form:"landTenure" query:"landTenure"`
	LandAreaSize  int     `json:"landAreaSize" form:"landAreaSize" query:"landAreaSize"`
	BuiltUpSize   int     `json:"builtUpSize" form:"builtUpSize" query:"builtUpSize"`
	BedroomCount  int     `json:"bedroomCount" form:"bedroomCount" query:"bedroomCount"`
	BathroomCount int     `json:"bathroomCount" form:"bathroomCount" query:"bathroomCount"`
	CountryAbbrev string  `json:"countryAbbrev" form:"countryAbbrev" query:"countryAbbrev"`
	StateAbbrev   string  `json:"stateAbbrev" form:"stateAbbrev" query:"stateAbbrev"`
	Locode        string  `json:"locode" form:"locode" query:"locode"`
	Postcode      int     `json:"postcode" form:"postcode" query:"postcode"`
	Lat           float64 `json:"lat" form:"lat" query:"lat"`
	Lon           float64 `json:"lon" form:"lon" query:"lon"`
	CreatedBy     int     `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedOn     int     `json:"createdOn" form:"createdOn" query:"createdOn"`
	UpdatedBy     int     `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedOn     int     `json:"updatedOn" form:"updatedOn" query:"updatedOn"`
}

// ResidentialProp
type ResidentPropListModel struct {
	Id            int64   `json:"id" form:"id" query:"id" validate:"required"`
	Title         string  `json:"title" form:"title" query:"title" validate:"required"`
	Description   string  `json:"description" form:"description" query:"description"`
	Price         float64 `json:"price" form:"price" query:"price"`
	PropType      int     `json:"propType" form:"propType" query:"propType" validate:"required"`
	PropTitle     int     `json:"propTitle" form:"propTitle" query:"propTitle"`
	LandTitle     int     `json:"landTitle" form:"landTitle" query:"landTitle"`
	LandTenure    int     `json:"landTenure" form:"landTenure" query:"landTenure"`
	LandAreaSize  int     `json:"landAreaSize" form:"landAreaSize" query:"landAreaSize"`
	BuiltUpSize   int     `json:"builtUpSize" form:"builtUpSize" query:"builtUpSize"`
	BedroomCount  int     `json:"bedroomCount" form:"bedroomCount" query:"bedroomCount"`
	BathroomCount int     `json:"bathroomCount" form:"bathroomCount" query:"bathroomCount"`
	CountryAbbrev string  `json:"countryAbbrev" form:"countryAbbrev" query:"countryAbbrev"`
	StateAbbrev   string  `json:"stateAbbrev" form:"stateAbbrev" query:"stateAbbrev"`
}
