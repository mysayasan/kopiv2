package entities

// ProfileCommand declares something a device TYPE can be told to do — and, just as importantly,
// the bounds within which it may be told to do it.
//
// A device can only be commanded in ways its profile declares. There is no generic
// "publish this arbitrary payload to that topic" endpoint, and that is deliberate: an escape
// hatch like that would turn the appliance into a remote shell for the building's electrics,
// and every safety property below would be a suggestion rather than a rule.
type ProfileCommand struct {
	Id        int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	ProfileId int64 `json:"profileId" form:"profileId" query:"profileId" idx:"profile" validate:"required"`
	// Name is the command's identifier ("relay", "setpoint").
	Name  string `json:"name" form:"name" query:"name" validate:"required"`
	Label string `json:"label" form:"label" query:"label"`
	// Kind decides how the value is validated and rendered:
	//   "switch"   — 0/1
	//   "setpoint" — a number within Min..Max
	//   "dimmer" / "position" — a percentage 0..100 (brightness, blind position)
	//   "cct"      — colour temperature in Kelvin, a setpoint bounded by Min..Max
	//   "mode"     — one of the integer values enumerated in Options
	//   "color"    — an RGB colour packed into one integer (0xRRGGBB)
	// An unknown Kind is REFUSED, not silently passed — see services.validateValue.
	Kind string `json:"kind" form:"kind" query:"kind"`
	// --- MQTT actuation (Transport == "" or "mqtt") ---
	// TopicTemplate is where the command is published, with {deviceKey} substituted.
	TopicTemplate string `json:"topicTemplate" form:"topicTemplate" query:"topicTemplate"`

	// --- Modbus actuation (Transport == "modbus") ---
	// Transport decides HOW the command reaches the device: "" / "mqtt" PUBLISHES the payload
	// template (the default, everything before this); "modbus" writes a HOLDING REGISTER on the
	// polled device the profile addresses. A Modbus command is the write half of the same driver
	// the poller reads with, and it goes through the identical gates — see CommandService.Issue.
	Transport string `json:"transport" form:"transport" query:"transport"`
	// Register is the holding register written for Transport == "modbus". RegKind is how the value
	// is encoded (u16/i16 — single-register writes only for now), and ScaleFactor is the SAME
	// multiplier the read side uses, applied in reverse: the raw register value is round(value /
	// scaleFactor). Getting this wrong writes a wrong number to real hardware, so it is authored
	// per the vendor's Modbus map exactly like the read bindings on TelemetryKey.
	Register    int     `json:"register" form:"register" query:"register"`
	RegKind     string  `json:"regKind" form:"regKind" query:"regKind"`
	ScaleFactor float64 `json:"scaleFactor" form:"scaleFactor" query:"scaleFactor"`
	// PayloadTemplate is the message body. {value} is substituted for every kind; for a "color"
	// command {r}/{g}/{b} are also substituted with the unpacked 0..255 channels. JSON in practice.
	PayloadTemplate string `json:"payloadTemplate" form:"payloadTemplate" query:"payloadTemplate"`
	// Options enumerates the allowed values of a "mode" command as a JSON array of
	// {"value":<int>,"label":<string>}. Empty for every other kind. It is what turns a mode
	// command into a named dropdown rather than a raw number, and it BOUNDS a mode server-side the
	// way Min/Max bound a setpoint: a value not in the list is refused.
	Options string `json:"options" form:"options" query:"options"`
	// Min and Max bound a setpoint. THEY ARE ENFORCED SERVER-SIDE, not merely rendered as
	// input attributes in a form. A thermostat that accepts 200 degrees because a UI slider was
	// bypassed is a fire, and "the frontend validates it" is not a safety property.
	Min float64 `json:"min" form:"min" query:"min"`
	Max float64 `json:"max" form:"max" query:"max"`
	// ConfirmKey is the TELEMETRY key on which the device reports the resulting state, and it is
	// how a command is confirmed. Without it, "sent" is the best that can ever be said — and
	// "sent" is not "it happened". A relay command whose device reports `state` back is
	// confirmed when a reading on `state` matches the value that was asked for.
	ConfirmKey string `json:"confirmKey" form:"confirmKey" query:"confirmKey"`
	CreatedBy  int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy  int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt  int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
