package entities

// IotDevice is one physical sensor or actuator. It is myiotsan's `camera`.
//
// DeviceKey is the identity a device authenticates and publishes as — its MQTT client id
// and the variable segment of its topic. It is unique, and it is what makes the device's
// INVENTORY RECORD its credential record: a device that is not in this table cannot connect
// to the broker at all (see infra/iot/mqtt's ACL hook). There is no separate credential
// store to drift out of sync with the device list.
type IotDevice struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// DeviceKey is the device's wire identity (MQTT client id / topic segment).
	DeviceKey string `json:"deviceKey" form:"deviceKey" query:"deviceKey" ukey:"device_key" validate:"required"`
	// Protocol is how readings arrive: "mqtt" (the device publishes) or "http" (it POSTs).
	Protocol string `json:"protocol" form:"protocol" query:"protocol"`
	// Endpoint and Unit address a POLLED device — one whose profile has Transport == "modbus".
	// A Modbus device does not dial into the broker; the app dials OUT to it, so unlike an MQTT
	// device its network location is its own per-instance property (two identical inverters share
	// a profile but not an IP). Endpoint is "host:port"; Unit is the Modbus unit/slave id. Both
	// are empty/zero for an MQTT device, which is addressed by its DeviceKey instead.
	Endpoint string `json:"endpoint" form:"endpoint" query:"endpoint"`
	Unit     int    `json:"unit" form:"unit" query:"unit"`
	// Transport is how the app reaches a Modbus device: "" / "tcp" (Modbus TCP / MBAP, the default),
	// "rtutcp" (RTU frames over TCP — a transparent RS485→TCP gateway), or "serial" (RTU over a
	// serial line). For "serial", Endpoint holds the port name (COM3, /dev/ttyUSB0) instead of host:port,
	// and the Baud/Parity/DataBits/StopBits below set the line. Ignored for non-Modbus devices.
	Transport string `json:"transport" form:"transport" query:"transport"`
	Baud      int    `json:"baud" form:"baud" query:"baud"`
	Parity    string `json:"parity" form:"parity" query:"parity"`
	DataBits  int    `json:"dataBits" form:"dataBits" query:"dataBits"`
	StopBits  int    `json:"stopBits" form:"stopBits" query:"stopBits"`
	// ProfileId points at the DeviceProfile that declares this device's telemetry keys.
	// Without a profile a device can connect but nothing it publishes can be decoded.
	ProfileId int64 `json:"profileId" form:"profileId" query:"profileId" idx:"profile"`
	// PasswordHash is the bcrypt hash of the device's broker password. Never returned.
	PasswordHash string `json:"-" form:"passwordHash" query:"passwordHash"`
	// Tag groups devices ("floor-2", "cold-store") so a rule can scope to a set rather than
	// naming each device — the replacement for mymatasan's per-camera zone polygon.
	Tag      string `json:"tag" form:"tag" query:"tag" idx:"tag"`
	Location string `json:"location" form:"location" query:"location"`
	Vendor   string `json:"vendor" form:"vendor" query:"vendor"`
	Model    string `json:"model" form:"model" query:"model"`
	Serial   string `json:"serial" form:"serial" query:"serial"`
	Firmware string `json:"firmware" form:"firmware" query:"firmware"`
	Enabled  bool   `json:"enabled" form:"enabled" query:"enabled"`
	// ActuationEnabled is the per-device capability toggle. Devices are READ-ONLY by
	// default: a command is refused unless this is explicitly turned on, on top of the
	// admin-only RBAC rule. A bad relay write is physically dangerous in a way a bad PTZ
	// move is not, so it takes two deliberate acts to enable one.
	ActuationEnabled bool `json:"actuationEnabled" form:"actuationEnabled" query:"actuationEnabled"`
	// LastSeenAt is the last time ANY payload arrived from this device (unix seconds). It
	// is the offline detector's input, so it is written on every publish — but NOT through
	// the reading path, which is deadbanded and would otherwise let a device that reports
	// an unchanged value look dead.
	LastSeenAt int64 `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	// Health is the device's reachability: "online", "offline", "unknown".
	Health    string `json:"health" form:"health" query:"health"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
