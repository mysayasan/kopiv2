package entities

// DeviceReading is one persisted sample. This is the hot table — everything else in the
// schema is small and this one grows forever — so it is deliberately narrow.
//
// Rows exist only for samples that PASSED the deadband (see TelemetryKey). This is therefore
// not a record of every packet the broker saw; it is a record of every time something
// changed. That distinction is the storage design.
//
// The (device, key, time) index is what every read wants: "this device's temperature over
// the last day", "the rollup worker's next batch". Without it, charting a month of data is a
// full scan of the largest table in the database.
type DeviceReading struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	DeviceId int64 `json:"deviceId" form:"deviceId" query:"deviceId" idx:"dev_key_time" validate:"required"`
	// Key is the telemetry key name, DENORMALIZED from TelemetryKey on purpose: a reading
	// must stay readable after its profile is edited or its key deleted. History does not get
	// to become unreadable because somebody renamed a field.
	Key string `json:"key" form:"key" query:"key" idx:"dev_key_time,key"`
	// Ts is the reading's time in unix MILLIseconds. Sub-second resolution matters here —
	// two sensor transitions inside one second are two different events.
	Ts int64 `json:"ts" form:"ts" query:"ts" idx:"dev_key_time,ts"`
	// Num holds a numeric or boolean reading (bool as 0/1); Str holds a string reading. Which
	// one is meaningful is decided by the key's DataType.
	Num float64 `json:"num" form:"num" query:"num"`
	Str string  `json:"str" form:"str" query:"str"`
	// Suspect marks a reading outside the key's declared Min/Max. It is STORED, not dropped:
	// a sensor reporting nonsense is itself the finding, and discarding it silently would
	// hide a failing device.
	Suspect bool `json:"suspect" form:"suspect" query:"suspect"`
}
