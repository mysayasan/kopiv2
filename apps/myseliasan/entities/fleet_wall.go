package entities

// FleetWall is a named camera arrangement that SPANS APPLIANCES — the video wall a control
// room actually needs, and the one no single appliance can offer.
//
// mymatasan's own wall (W3-3b) arranges cameras that live on one recorder. That is the right
// thing for a recorder and it is not what a guard station watches: four buildings, four
// appliances, one screen. This is the fleet's version, and the only structural difference is
// that a tile names an APPLIANCE as well as a camera.
//
// Table `fleet_wall`, created by the auto-migrator.
type FleetWall struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Name is how a wall is chosen, so it is required and unique. Uniqueness is enforced in
	// the service rather than by an index, so a clash comes back as a sentence an operator
	// can act on instead of a driver error.
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Grid is a layout id ("2x2", "3x3", …), validated on save against the set the service
	// knows. An unknown grid is refused rather than stored and rendered as an empty screen.
	Grid string `json:"grid" form:"grid" query:"grid"`
	// Tiles is the ORDERED tile list, encoded "<nodeId>:<cameraId>,<nodeId>:<cameraId>,…".
	//
	// A join table would need an order column and buy nothing: this is a display
	// arrangement, not a relation anything queries. Nothing joins on it, nothing filters by
	// it, and the whole value is read and written as one unit every time. The node id is a
	// STRING because that is what a node id is throughout this control plane.
	Tiles string `json:"tiles" form:"tiles" query:"tiles"`
	// CycleSeconds auto-advances the wall through its pages when the tiles do not fit the
	// grid. 0 = do not cycle. This is the rotation a guard station runs unattended.
	CycleSeconds int `json:"cycleSeconds" form:"cycleSeconds" query:"cycleSeconds"`
	// AutoPopSeconds pulls a camera onto the visible page when it raises an alert, for this
	// many seconds. 0 = never.
	//
	// ON A FLEET WALL THIS IS THE WHOLE POINT. A single appliance can only pop a camera it
	// owns; the control plane sees every node's alerts in one feed, so a wall here can pull
	// up the camera that is raising the alarm in a building the operator was not looking at.
	// That is the thing an appliance vendor cannot do at all.
	AutoPopSeconds int `json:"autoPopSeconds" form:"autoPopSeconds" query:"autoPopSeconds"`
	// IsDefault marks the wall a screen opens with when nothing else is chosen. At most one
	// wall may hold it; the service clears the others rather than letting two rows claim it,
	// because "the default" with two answers is a screen that opens differently depending on
	// row order.
	IsDefault bool `json:"isDefault" form:"isDefault" query:"isDefault"`

	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedName string `json:"createdName" form:"createdName" query:"createdName"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
