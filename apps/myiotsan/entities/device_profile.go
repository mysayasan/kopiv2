package entities

// DeviceProfile is a template for a device TYPE: what it publishes, where, and in what
// shape. It is the abstraction mymatasan does not have, and it is what makes onboarding
// scale past a demo — without it, every one of a hundred identical door sensors would be
// configured by hand.
//
// A profile is shipped (Builtin) or site-authored. Builtin profiles are seeded from a
// catalog, the same way mymatasan seeds its detection-class labels.
type DeviceProfile struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Slug string `json:"slug" form:"slug" query:"slug" ukey:"slug" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Vendor and Description are for the human choosing a profile.
	Vendor      string `json:"vendor" form:"vendor" query:"vendor"`
	Description string `json:"description" form:"description" query:"description"`
	// TopicTemplate is the MQTT topic a device of this type publishes to, with {deviceKey}
	// substituted. The de-facto conventions (Zigbee2MQTT, Tasmota, Shelly) all fit this.
	TopicTemplate string `json:"topicTemplate" form:"topicTemplate" query:"topicTemplate"`
	// PayloadFormat is how to decode what arrives: "json" (the overwhelmingly common case)
	// or "raw" (the entire payload is one value). It applies to a PUSH transport only.
	PayloadFormat string `json:"payloadFormat" form:"payloadFormat" query:"payloadFormat"`
	// Transport is how a device of this TYPE is read. "" or "mqtt" = the device PUBLISHES to the
	// broker (the default, and everything shipped before P5). "modbus" = the app POLLS the device
	// over Modbus TCP. This is the field that turns a profile from a topic descriptor into a
	// driver descriptor without a second entity — the same "declare once" pattern one level down.
	Transport string `json:"transport" form:"transport" query:"transport"`
	// ModbusMode applies when Transport == "modbus":
	//   "sunspec" — self-describing: the driver walks the model chain and the keys are DISCOVERED,
	//               so a single profile decodes any compliant inverter/meter/battery. The declared
	//               telemetry keys only supply deadband/heartbeat/range for the keys it emits.
	//   "regmap"  — non-SunSpec: each telemetry key carries an explicit register binding (above).
	ModbusMode string `json:"modbusMode" form:"modbusMode" query:"modbusMode"`
	// ModbusBase is the SunSpec base register for ModbusMode == "sunspec"; 0 = auto-discover
	// (the driver tries 40000, 50000, 0). Ignored for a register-map profile.
	ModbusBase int `json:"modbusBase" form:"modbusBase" query:"modbusBase"`
	// PollSeconds is the Modbus poll cadence. 0 defaults to 5s. It governs bus load the way the
	// deadband governs storage: a wider interval is fewer round trips, not fewer stored rows.
	PollSeconds int `json:"pollSeconds" form:"pollSeconds" query:"pollSeconds"`
	// Builtin marks a shipped profile. Builtins can be used and copied but not deleted, so
	// a site cannot break its own onboarding by tidying up.
	Builtin   bool  `json:"builtin" form:"builtin" query:"builtin"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
