package entities

// Site kinds. A site is not always a building: a park is one open ground surface with no storeys,
// and a traffic-light junction is a pole with cameras on it and no surface at all. One field keeps
// all three on the same map, the same overview and the same node assignment.
const (
	// SiteKindBuilding has one or more floors, drawn with walls/doors/stairs and stacked by
	// elevation in the 3D view. The default, and what an empty Kind means.
	SiteKindBuilding = "building"
	// SiteKindOutdoor is an open area — a park, yard, car park, campus. It holds exactly ONE plan
	// (its ground surface), which is why the wizard never asks it "how many areas"; in 3D it is a
	// flat ground plane with no storey above it.
	SiteKindOutdoor = "outdoor"
	// SiteKindPoint has NO plan: a junction, pole, gate, barrier. Its cameras reach it through the
	// owning node's SiteId, so clicking its marker opens the node's device card rather than a plan.
	SiteKindPoint = "point"
)

// NormalizeSiteKind maps whatever a client sent onto a known kind, defaulting to building. It is
// the ONE place that decides what a kind may be, so an unknown string can never reach the database
// and leave the frontend guessing how to draw the marker.
func NormalizeSiteKind(kind string) string {
	switch kind {
	case SiteKindOutdoor:
		return SiteKindOutdoor
	case SiteKindPoint:
		return SiteKindPoint
	default:
		return SiteKindBuilding
	}
}

// HasPlans reports whether a site of this kind owns floor plans at all. A point asset does not, so
// callers can skip the plan fetch (and the UI can skip the editor) instead of showing an empty one.
func HasPlans(kind string) bool { return NormalizeSiteKind(kind) != SiteKindPoint }

// Site is a named physical location that a fleet's cameras belong to. It is the container an
// operator drags cameras and nodes onto in the non-geographic (indoor) view, the counterpart to
// the lat/lon geographic map.
//
// Not every monitored place is a building. Kind says WHAT the site is, and everything downstream
// (how many plans it can hold, whether it stacks in 3D, what its map marker looks like, what a
// click on it opens) follows from that one field — see the SiteKind* constants.
type Site struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	// Kind is one of the SiteKind* values. Empty means SiteKindBuilding — every site that predates
	// this field is a building, so the zero value needs no backfill beyond NULL-safety.
	Kind string `json:"kind" form:"kind" query:"kind"`
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
	BgPath string `json:"-" form:"bgPath" query:"bgPath"`
	// HasPlanImage records whether this floor's picture came from an OPERATOR UPLOAD rather than
	// being the generated blank canvas. Both are stored identically — a blank area is a plain white
	// PNG on disk, not a NULL image — so without this flag the two are indistinguishable and the
	// editor cannot tell whether there is a plan to remove. Set by the upload paths, cleared by
	// AddBlankFloor and ClearFloorImage.
	HasPlanImage bool   `json:"hasPlanImage" form:"hasPlanImage" query:"hasPlanImage"`
	ContentType  string `json:"contentType" form:"contentType" query:"contentType"`
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
