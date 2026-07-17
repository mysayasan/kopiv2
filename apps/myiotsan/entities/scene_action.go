package entities

// SceneAction is one step of a Scene: tell one device to run one of its profile's declared commands
// with one value. Ordinal orders the steps; they run in that order.
//
// Value is the same single scalar a DeviceCommand carries — a colour action holds the packed
// 0xRRGGBB integer, a dimmer holds a percentage — so a scene inherits every command kind for free
// and needs no per-kind machinery of its own.
type SceneAction struct {
	Id      int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	SceneId int64 `json:"sceneId" form:"sceneId" query:"sceneId" idx:"scene" validate:"required"`
	// Ordinal is the step order within the scene (0-based). Actions run low to high.
	Ordinal int `json:"ordinal" form:"ordinal" query:"ordinal"`
	// DeviceId and CommandName name the device and one of its declared commands. They are resolved
	// at RUN time, not stored-resolved, so a command whose bounds an admin later tightens is
	// re-checked on every run rather than frozen at authoring time.
	DeviceId    int64   `json:"deviceId" form:"deviceId" query:"deviceId" validate:"required"`
	CommandName string  `json:"commandName" form:"commandName" query:"commandName" validate:"required"`
	Value       float64 `json:"value" form:"value" query:"value"`
	CreatedBy   int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
