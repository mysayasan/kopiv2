package entities

// TelemetryKey declares one datapoint a profile's devices report: its name, its unit, how to
// pull it out of the payload, and — the important one — its DEADBAND.
//
// THE DEADBAND IS THE STORAGE DESIGN. 100 devices x 10 keys at 1 Hz is 1,000 rows a second,
// which SQLite will not absorb and which no amount of batching makes free. But a building
// sensor is near-static almost all of the time: a room is 21.4 degrees for an hour, a door is
// closed all night. Persisting a reading only when it MOVES by more than the deadband (or
// when the heartbeat elapses, so a flat line is still provably alive) collapses that by one
// to two orders of magnitude, and loses nothing an operator would ask about later.
//
// This is why a time-series database is not needed, and not wanted: adding one would break
// the single-binary, air-gapped deployment that IS the product.
type TelemetryKey struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	ProfileId int64  `json:"profileId" form:"profileId" query:"profileId" idx:"profile" validate:"required"`
	Key       string `json:"key" form:"key" query:"key" validate:"required"`
	Label     string `json:"label" form:"label" query:"label"`
	Unit      string `json:"unit" form:"unit" query:"unit"`
	// DataType is "number", "bool" or "string". It decides which column a reading lands in
	// and which rule conditions may be applied to it.
	DataType string `json:"dataType" form:"dataType" query:"dataType"`
	// JsonPath is the dotted path into the payload ("battery.level"). Empty means the key
	// name is itself a top-level field.
	JsonPath string `json:"jsonPath" form:"jsonPath" query:"jsonPath"`
	// Deadband is the smallest absolute change worth persisting. 0 means store every sample —
	// correct for a door contact, where every transition matters, and wrong for a temperature
	// probe, where every flicker of sensor noise would become a row.
	Deadband float64 `json:"deadband" form:"deadband" query:"deadband"`
	// HeartbeatSeconds forces a row even when the value has not moved, so a flat line is
	// distinguishable from a dead device and a chart has a point to draw. 0 disables it.
	HeartbeatSeconds int `json:"heartbeatSeconds" form:"heartbeatSeconds" query:"heartbeatSeconds"`
	// Min and Max are the plausible range. A reading outside it is STORED but flagged: a
	// sensor reporting -3000 degrees is broken, not cold, and dropping it would hide that.
	Min       float64 `json:"min" form:"min" query:"min"`
	Max       float64 `json:"max" form:"max" query:"max"`
	CreatedBy int64   `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64   `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64   `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
