package entities

// PtzTour is a guard tour: an ordered set of stored positions one PTZ camera visits, and
// how long it waits at each.
//
// WHAT IS NOT HERE IS THE POINT. The positions themselves live on the CAMERA — ONVIF
// presets, addressed by a token the device issues. This table stores only the itinerary.
// Mirroring the positions here would create a second answer to "where can this camera
// point", and the two part company the first time somebody uses the camera's own web page,
// which is how a large share of PTZ cameras in the field get set up. A tour therefore holds
// tokens it does not own and has to cope with one having been deleted — the same shape as
// WallLayout holding camera ids it does not own, and reported the same way.
type PtzTour struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId int64 `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required"`
	// Name is how a tour is chosen; unique per camera, enforced in the service so a clash
	// comes back as a sentence rather than a driver error.
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Stops is the ORDERED itinerary, encoded "presetToken:dwellSeconds" per stop,
	// comma-separated. A stop with dwell 0 uses the tour's DwellSeconds.
	//
	// A join table would need an order column and buy nothing: nothing queries a stop,
	// nothing joins on it, and the whole itinerary is read and written as one unit.
	Stops string `json:"stops" form:"stops" query:"stops"`
	// DwellSeconds is how long to hold a stop that does not name its own dwell.
	DwellSeconds int `json:"dwellSeconds" form:"dwellSeconds" query:"dwellSeconds"`
	// IsRunning is PERSISTED, not merely in memory, because an appliance that reboots at
	// 03:00 must come back doing what it was doing. A tour that stops at every power cut
	// is a tour an operator stops trusting, and nobody is awake to restart it.
	IsRunning bool `json:"isRunning" form:"isRunning" query:"isRunning"`

	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedName string `json:"createdName" form:"createdName" query:"createdName"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
