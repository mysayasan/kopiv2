package entities

// Site is a named physical location — a building, campus, or yard — that groups one or
// more floor plans. It is the container an operator drags cameras and nodes onto in the
// non-geographic (indoor) view, the counterpart to the lat/lon geographic map.
type Site struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	// Icon is the building glyph shown on the geo map and pickers — a single emoji chosen at
	// creation (🏢 office, 🏭 factory, 🏠 home, …). Emoji so it needs no image asset and renders
	// natively in the OpenLayers canvas marker. Empty = the default building glyph.
	Icon string `json:"icon" form:"icon" query:"icon"`
	// Ordinal orders sites in the picker; lower shows first.
	Ordinal int `json:"ordinal" form:"ordinal" query:"ordinal"`
	// Lat/Lon are the building's position on the geographic fleet map, set by an operator
	// dragging its marker. This is the KEY that makes the map a digital twin: a building is a
	// physical place where cameras live, so it — not the node appliance — is what anchors the
	// cameras geographically. A node (NVR/hub) can sit anywhere (a rack, another building, off
	// site); its cameras' true location is this site + the floor placement.
	//
	// MapPlaced distinguishes "deliberately positioned" from the (0,0) zero value (a real point
	// in the Gulf of Guinea), exactly as ManagedNode does — an unplaced site is simply absent
	// from the map until an operator drops it.
	Lat       float64 `json:"lat" form:"lat" query:"lat"`
	Lon       float64 `json:"lon" form:"lon" query:"lon"`
	MapPlaced bool    `json:"mapPlaced" form:"mapPlaced" query:"mapPlaced"`
	CreatedBy int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
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
	ImagePath string `json:"-" form:"imagePath" query:"imagePath"`
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
	// Grid holds the JSON grid the operator paints in the 3D editor: cell size (in the SAME pixel
	// space as Width/Height and placement X/Y) plus the wall/floor cells. Empty when no 3D layout
	// has been authored — the 3D view then falls back to a perimeter box. Shape:
	//   {"cellPx":32,"cols":32,"rows":24,"walls":[[c,r],...],"floors":[[c,r],...]}
	Grid string `json:"grid" form:"grid" query:"grid"`
	// Scale is the real-world size of one image pixel in METRES (metres-per-pixel). 0 = unset, in
	// which case the 3D view assumes a nominal building size so proportions still read correctly.
	// With a real scale set, wall/mount heights and camera coverage are physically accurate.
	Scale float64 `json:"scale" form:"scale" query:"scale"`
	// WallHeight is the extruded wall height in METRES for the 3D view. 0 = use the default storey
	// height. Metres so it is meaningful regardless of Scale.
	WallHeight float64 `json:"wallHeight" form:"wallHeight" query:"wallHeight"`
	// Elevation is this floor's base height in METRES above the building's ground, used to STACK
	// floors vertically in the 3D view (ground floor 0, first floor ~WallHeight, …). 0 = derive
	// from Ordinal at render time.
	Elevation float64 `json:"elevation" form:"elevation" query:"elevation"`
	CreatedBy int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
