package entities

// NodePlacement pins a node — or one camera on a node — to a spot on a floor plan. It is the
// FIRST myseliasan-owned record of a node's cameras: the control plane has no camera table,
// cameras are otherwise fetched live from the node over the tunnel. That live fetch returns
// nothing when the node is offline, so a placement carries a LastKnownName snapshot and is
// keyed on (NodeId, CameraId) to stay meaningful and renderable while the node is unreachable.
type NodePlacement struct {
	Id      int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	FloorId int64 `json:"floorId" form:"floorId" query:"floorId" idx:"floor" validate:"required"`
	// NodeId is always set. CameraId is empty for a placement of the node itself (a sensor
	// hub, or a camera node shown as one marker); non-empty pins a single camera on the node.
	//
	// (NodeId, CameraId) is UNIQUE across the whole fleet: a camera is in exactly one physical
	// place, so it can hold exactly one pin. Placing it somewhere else means unplacing it first.
	// The unique index is the backstop — the service refuses the second placement with a message
	// naming where the camera already sits, which is the error an operator can actually act on.
	// Empty CameraId participates in the key, so a node's own marker is single-placement too.
	NodeId   string `json:"nodeId" form:"nodeId" query:"nodeId" ukey:"camera" validate:"required"`
	CameraId string `json:"cameraId" form:"cameraId" query:"cameraId" ukey:"camera"`
	// LastKnownName is a snapshot of the node/camera label taken at placement time, shown when
	// the node is offline and the live name cannot be fetched.
	LastKnownName string `json:"lastKnownName" form:"lastKnownName" query:"lastKnownName"`
	// X/Y are the position in the floor image's pixel coordinate space (the OL pixel
	// projection), so a marker sits exactly where it was dropped regardless of zoom.
	X float64 `json:"x" form:"x" query:"x"`
	Y float64 `json:"y" form:"y" query:"y"`
	// Heading is the camera's facing direction in degrees clockwise from north (up on the plan
	// image); Fov is the field-of-view spread in degrees. Together they draw the coverage arc on
	// the plan. Fov of 0 means "no arc" (a plain marker) — used for node/sensor placements.
	Heading float64 `json:"heading" form:"heading" query:"heading"`
	Fov     float64 `json:"fov" form:"fov" query:"fov"`
	// MountHeight is how high the camera sits above the floor in METRES (a wall-mounted camera is
	// ~2.5m). Pitch is its downward tilt in degrees (0 = looking level, 90 = straight down).
	// Together with Heading+Fov they position the 3D coverage cone. 0 = use sensible defaults.
	MountHeight float64 `json:"mountHeight" form:"mountHeight" query:"mountHeight"`
	Pitch       float64 `json:"pitch" form:"pitch" query:"pitch"`
	CreatedBy   int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
