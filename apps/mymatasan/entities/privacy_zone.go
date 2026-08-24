package entities

// PrivacyZone is a region of one camera's view that must not be seen (W3-6).
//
// It is ONE row consumed by TWO mechanisms that protect different things, and the
// difference matters enough to be stated wherever this is shown:
//
//   - The camera is asked to burn it in, as an ONVIF Media2 privacy mask. Then the pixels
//     never leave the sensor: the recording on disk does not contain them, an export cannot
//     leak them, and somebody who walks off with the drive has nothing.
//   - Every EXPORT redacts it, whether or not the camera agreed. That is a real protection
//     for the footage that leaves the building, and it is NOT the same as the first one —
//     the recording still holds the pixels, and anybody with access to the appliance can
//     still see them.
//
// A mask that is not burned in by the camera is a courtesy, not a privacy control. Which
// one an operator has is a fact about their hardware, so the product reports it per camera
// rather than implying the stronger claim. See services/privacy.go.
type PrivacyZone struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId int64 `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required"`
	// Name is what an operator calls it — "the neighbour's window", "the pavement".
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Polygon is the region, as a JSON array of [x,y] pairs normalized 0..1 with the
	// origin at the top left.
	//
	// The SAME encoding detection zones use, deliberately: they are drawn on the same
	// picture with the same editor, and a second convention would mean a zone that looks
	// right in one screen and is wrong in the other. ONVIF's own space is different in
	// origin, scale and the direction of y, and that conversion lives at the boundary
	// (onvif.MaskPointFromUnit) rather than in the stored value.
	Polygon string `json:"polygon" form:"polygon" query:"polygon" validate:"required"`
	// Style is how the region is obscured: color, blurred or pixelated. Cameras vary in
	// what they support, and a camera that cannot do the chosen style falls back rather
	// than refusing the zone — a solid box is still a mask.
	Style string `json:"style" form:"style" query:"style"`
	// Enabled allows a zone to be turned off without losing the drawing, which is what
	// somebody does while working out where a camera should look.
	Enabled bool `json:"enabled" form:"enabled" query:"enabled"`
	// MaskToken is the token the CAMERA issued for this zone's mask, when the camera
	// accepted one. Empty means the zone is export-only on this camera.
	//
	// Stored because it is the only handle for editing or removing the mask later — the
	// same reason a PTZ tour stores preset tokens. It is a handle we do not own, and a
	// camera that has been factory-reset will not know it; the service re-creates rather
	// than failing when a stored token has gone.
	MaskToken string `json:"maskToken" form:"maskToken" query:"maskToken"`

	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedName string `json:"createdName" form:"createdName" query:"createdName"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
