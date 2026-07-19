package entities

// Site is a named physical location — a building, campus, or yard — that groups one or
// more floor plans. It is the container an operator drags cameras and nodes onto in the
// non-geographic (indoor) view, the counterpart to the lat/lon geographic map.
type Site struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	// Ordinal orders sites in the picker; lower shows first.
	Ordinal   int   `json:"ordinal" form:"ordinal" query:"ordinal"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// FloorPlan is one uploaded plan image belonging to a site — a floor, a wing, a yard
// layout. The image bytes are stored encrypted at rest on disk (see the sites service);
// only the metadata and pixel dimensions live in the database.
//
// Width/Height are the image's pixel dimensions, captured at upload. They ARE the extent of
// the OpenLayers pixel projection the frontend renders the plan in — image coordinates are
// map coordinates, so a camera dropped at pixel (412, 380) is stored as exactly that.
type FloorPlan struct {
	Id     int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	SiteId int64  `json:"siteId" form:"siteId" query:"siteId" idx:"site" validate:"required"`
	Name   string `json:"name" form:"name" query:"name" validate:"required"`
	// Ordinal orders floors within a site (ground floor below first floor, etc.).
	Ordinal int `json:"ordinal" form:"ordinal" query:"ordinal"`
	// ImagePath is the on-disk location of the ENCRYPTED plan image, relative to the data
	// dir. It is never served directly; GET /api/floors/{id}/image decrypts on the fly.
	ImagePath   string `json:"-" form:"imagePath" query:"imagePath"`
	// BgPath is the PRISTINE background image (an uploaded plan photo, or empty for a plan drawn
	// on a blank canvas). ImagePath is the RENDERED plan (background + drawn shapes) shown
	// everywhere; BgPath is loaded as the designer's canvas background so re-editing draws on the
	// original image, never on an already-composited one.
	BgPath      string `json:"-" form:"bgPath" query:"bgPath"`
	ContentType string `json:"contentType" form:"contentType" query:"contentType"`
	Width       int    `json:"width" form:"width" query:"width"`
	Height      int    `json:"height" form:"height" query:"height"`
	// Design holds the JSON vector shapes when the plan was DRAWN in the in-app designer (empty
	// for an uploaded image). It lets the operator reopen and re-edit a drawn plan; on save the
	// designer re-rasterises to the image and rewrites this field.
	Design string `json:"design" form:"design" query:"design"`
	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
