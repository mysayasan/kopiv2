package services

// The shipped device catalog.
//
// These are the device types a building/security install actually has, expressed once so the
// hundredth door sensor is a name and a dropdown rather than a configuration exercise. The
// topic templates follow the conventions the devices really emit (Zigbee2MQTT, Tasmota,
// Shelly), so a device flashed with stock firmware works without a custom profile.
//
// THE DEADBANDS ARE THE POINT OF THIS FILE. Each one is a claim about how much change is worth
// a row, and they are what make a thousand samples a second collapse into a manageable write
// rate:
//
//   - A door contact has NO deadband and no heartbeat-suppression concern: every transition is
//     the event, and there are only a handful a day.
//   - A temperature probe has 0.2 degrees, which is coarser than its own noise floor. Without
//     it, a probe reporting 21.40, 21.41, 21.39 would write a row a second forever and say
//     nothing.
//   - A power meter has a wide deadband in watts, because a fridge compressor cycling is worth
//     a row and a phone charger's ripple is not.
//
// Heartbeats keep a flat line provably alive: a leak sensor that has been dry for a year still
// writes a row every 30 minutes, so "dry" and "dead" remain distinguishable.

type builtinProfile struct {
	Slug          string
	Name          string
	Vendor        string
	Description   string
	TopicTemplate string
	Keys          []SaveTelemetryKey
	// Commands is what a device of this type can be TOLD to do. Most of the catalog declares
	// none, and that is the correct default: a sensor that cannot be commanded cannot be
	// commanded WRONGLY, and the majority of a building's devices only ever need to be read.
	Commands []SaveProfileCommand
}

func builtinProfiles() []builtinProfile {
	return []builtinProfile{
		{
			Slug:          "door-contact",
			Name:          "Door / window contact",
			Vendor:        "Generic (Zigbee2MQTT)",
			Description:   "Magnetic open/closed contact. The core sensor of an intrusion install.",
			TopicTemplate: "zigbee2mqtt/{deviceKey}",
			Keys: []SaveTelemetryKey{
				// No deadband: an open and a close are both events, and there are a few a day.
				{Key: "contact", Label: "Contact", DataType: "bool", Deadband: 0, HeartbeatSeconds: 3600},
				{Key: "battery", Label: "Battery", Unit: "%", DataType: "number", Deadband: 5, HeartbeatSeconds: 21600, Min: 0, Max: 100},
				{Key: "linkquality", Label: "Link quality", DataType: "number", Deadband: 20, HeartbeatSeconds: 21600},
			},
		},
		{
			Slug:          "pir-motion",
			Name:          "PIR motion sensor",
			Vendor:        "Generic (Zigbee2MQTT)",
			Description:   "Passive infrared occupancy sensor.",
			TopicTemplate: "zigbee2mqtt/{deviceKey}",
			Keys: []SaveTelemetryKey{
				{Key: "occupancy", Label: "Occupancy", DataType: "bool", Deadband: 0, HeartbeatSeconds: 3600},
				{Key: "illuminance", Label: "Illuminance", Unit: "lx", DataType: "number", Deadband: 50, HeartbeatSeconds: 3600},
				{Key: "battery", Label: "Battery", Unit: "%", DataType: "number", Deadband: 5, HeartbeatSeconds: 21600, Min: 0, Max: 100},
			},
		},
		{
			Slug:          "temp-humidity",
			Name:          "Temperature / humidity sensor",
			Vendor:        "Generic (Zigbee2MQTT)",
			Description:   "Ambient temperature and relative humidity. Cold-store and comfort monitoring.",
			TopicTemplate: "zigbee2mqtt/{deviceKey}",
			Keys: []SaveTelemetryKey{
				// 0.2C is deliberately coarser than the sensor's own noise. This one deadband is
				// the difference between a row a second and a row when the room actually changes.
				{Key: "temperature", Label: "Temperature", Unit: "C", DataType: "number", Deadband: 0.2, HeartbeatSeconds: 900, Min: -50, Max: 100},
				{Key: "humidity", Label: "Humidity", Unit: "%", DataType: "number", Deadband: 1, HeartbeatSeconds: 900, Min: 0, Max: 100},
				{Key: "battery", Label: "Battery", Unit: "%", DataType: "number", Deadband: 5, HeartbeatSeconds: 21600, Min: 0, Max: 100},
			},
		},
		{
			Slug:          "smoke-detector",
			Name:          "Smoke / heat detector",
			Vendor:        "Generic (Zigbee2MQTT)",
			Description:   "Life-safety detector. Never deadband the alarm state.",
			TopicTemplate: "zigbee2mqtt/{deviceKey}",
			Keys: []SaveTelemetryKey{
				// A life-safety signal. Zero deadband, and the heartbeat is short: a smoke
				// detector that has gone quiet is an emergency in itself, and the offline rule
				// can only see that if the device is expected to speak often.
				{Key: "smoke", Label: "Smoke", DataType: "bool", Deadband: 0, HeartbeatSeconds: 600},
				{Key: "temperature", Label: "Temperature", Unit: "C", DataType: "number", Deadband: 1, HeartbeatSeconds: 900, Min: -50, Max: 200},
				{Key: "battery", Label: "Battery", Unit: "%", DataType: "number", Deadband: 5, HeartbeatSeconds: 21600, Min: 0, Max: 100},
			},
		},
		{
			Slug:          "water-leak",
			Name:          "Water leak sensor",
			Vendor:        "Generic (Zigbee2MQTT)",
			Description:   "Flood/leak detection under plant, in risers and server rooms.",
			TopicTemplate: "zigbee2mqtt/{deviceKey}",
			Keys: []SaveTelemetryKey{
				// Dry for years, then one transition that matters enormously. The heartbeat is
				// what keeps "dry" distinguishable from "dead battery".
				{Key: "water_leak", Label: "Water leak", DataType: "bool", Deadband: 0, HeartbeatSeconds: 1800},
				{Key: "battery", Label: "Battery", Unit: "%", DataType: "number", Deadband: 5, HeartbeatSeconds: 21600, Min: 0, Max: 100},
			},
		},
		{
			Slug:          "power-meter",
			Name:          "Power / energy meter",
			Vendor:        "Shelly / Tasmota",
			Description:   "Single-phase power and cumulative energy.",
			TopicTemplate: "{deviceKey}/status",
			Keys: []SaveTelemetryKey{
				// Wide in watts: a compressor cycling is worth a row; a charger's ripple is not.
				{Key: "power", Label: "Power", Unit: "W", DataType: "number", JsonPath: "apower", Deadband: 10, HeartbeatSeconds: 300, Min: 0, Max: 30000},
				{Key: "voltage", Label: "Voltage", Unit: "V", DataType: "number", Deadband: 2, HeartbeatSeconds: 900, Min: 0, Max: 500},
				{Key: "current", Label: "Current", Unit: "A", DataType: "number", Deadband: 0.2, HeartbeatSeconds: 900, Min: 0, Max: 100},
				// Cumulative and monotonic: a coarse deadband still captures the curve.
				{Key: "energy", Label: "Energy", Unit: "Wh", DataType: "number", JsonPath: "aenergy.total", Deadband: 10, HeartbeatSeconds: 3600},
			},
		},
		{
			Slug:          "smart-relay",
			Name:          "Smart relay / switch",
			Vendor:        "Shelly / Tasmota",
			Description:   "A controllable relay. THE ONE PROFILE IN THIS CATALOG THAT CAN ACT ON THE WORLD — everything else only reports.",
			TopicTemplate: "{deviceKey}/status/switch:0",
			Keys: []SaveTelemetryKey{
				// The device reports its own state here, and this is what CONFIRMS a command.
				// Without it, "we published a message" is the best that could ever be said, and
				// that is not the same as "the relay closed".
				{Key: "output", Label: "Output", DataType: "bool", Deadband: 0, HeartbeatSeconds: 300},
				{Key: "power", Label: "Power", Unit: "W", DataType: "number", JsonPath: "apower", Deadband: 10, HeartbeatSeconds: 300, Min: 0, Max: 30000},
				{Key: "temperature", Label: "Temperature", Unit: "C", DataType: "number", Deadband: 1, HeartbeatSeconds: 900, Min: -20, Max: 120},
			},
			Commands: []SaveProfileCommand{
				{
					Name: "output", Label: "Relay", Kind: "switch",
					TopicTemplate:   "{deviceKey}/rpc",
					PayloadTemplate: `{"method":"Switch.Set","params":{"id":0,"on":{value}}}`,
					// The device reports back on "output", so a command is CONFIRMED only when
					// the relay itself says it changed — not when we manage to publish.
					ConfirmKey: "output",
				},
			},
		},
		{
			Slug:          "access-reader",
			Name:          "Access control reader",
			Vendor:        "Generic",
			Description:   "Badge reader. The 'no badge swipe' half of a cross-domain intrusion rule.",
			TopicTemplate: "access/{deviceKey}/event",
			Keys: []SaveTelemetryKey{
				// A string key: the credential presented. Compared by equality, never averaged.
				{Key: "badge", Label: "Badge", DataType: "string", Deadband: 0},
				{Key: "granted", Label: "Access granted", DataType: "bool", Deadband: 0},
				{Key: "tamper", Label: "Tamper", DataType: "bool", Deadband: 0, HeartbeatSeconds: 3600},
			},
		},
	}
}
