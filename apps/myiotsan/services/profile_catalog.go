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
	// Transport/ModbusMode/ModbusBase/PollSeconds describe a POLLED (Modbus) device type. They
	// are empty/zero for the MQTT sensors that make up most of this catalog, which the seeder
	// treats as the default "mqtt" transport. See entities.DeviceProfile.
	Transport   string
	ModbusMode  string
	ModbusBase  int
	PollSeconds int
	Keys        []SaveTelemetryKey
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

		{
			Slug:          "smart-lamp",
			Name:          "Smart lamp / bulb",
			Vendor:        "Generic (Zigbee2MQTT)",
			Description:   "A controllable light: on/off, brightness, colour temperature and RGB colour. The worked example for the home-automation command kinds — adapt the payloads to your bulb's firmware.",
			TopicTemplate: "zigbee2mqtt/{deviceKey}",
			Keys: []SaveTelemetryKey{
				// The bulb reports these back; each is a ConfirmKey below, so a command is CONFIRMED
				// only when the bulb says it changed — not when we manage to publish.
				{Key: "state", Label: "On/off", DataType: "bool", Deadband: 0, HeartbeatSeconds: 3600},
				{Key: "brightness", Label: "Brightness", Unit: "%", DataType: "number", Deadband: 2, HeartbeatSeconds: 3600, Min: 0, Max: 100},
				{Key: "color_temp", Label: "Colour temperature", Unit: "K", DataType: "number", Deadband: 50, HeartbeatSeconds: 3600, Min: 1000, Max: 10000},
			},
			Commands: []SaveProfileCommand{
				{Name: "power", Label: "Power", Kind: "switch", TopicTemplate: "zigbee2mqtt/{deviceKey}/set", PayloadTemplate: `{"state":{value}}`, ConfirmKey: "state"},
				{Name: "brightness", Label: "Brightness", Kind: "dimmer", TopicTemplate: "zigbee2mqtt/{deviceKey}/set", PayloadTemplate: `{"brightness":{value}}`, ConfirmKey: "brightness"},
				{Name: "color_temp", Label: "Colour temperature", Kind: "cct", Min: 2200, Max: 6500, TopicTemplate: "zigbee2mqtt/{deviceKey}/set", PayloadTemplate: `{"color_temp_k":{value}}`, ConfirmKey: "color_temp"},
				// Colour has no ConfirmKey: a bulb reports colour back per-channel, which one packed
				// float cannot be equality-confirmed against — "sent, never confirmed" is honest.
				{Name: "color", Label: "Colour", Kind: "color", TopicTemplate: "zigbee2mqtt/{deviceKey}/set", PayloadTemplate: `{"color":{"r":{r},"g":{g},"b":{b}}}`},
			},
		},

		// --- POLLED devices: the solar / industrial-protocol profiles (P5) --------------------
		//
		// Everything above PUBLISHES to the broker; these two are POLLED over Modbus TCP. They are
		// the first driver profiles, and they are deliberately a matched pair — one for each half
		// of the "don't code per model" design:
		//
		//   - generic-sunspec-solar is SELF-DESCRIBING. It declares NO register bindings; the driver
		//     walks the SunSpec model chain and discovers them. This ONE profile therefore reads any
		//     compliant inverter/meter/battery — SolarEdge, SMA, Fronius, most Sungrow, and the
		//     SunSpec-103 block a Huawei exposes — with no per-model work. Its keys exist only to
		//     attach a deadband/heartbeat/range to the datapoints worth storing.
		//   - huawei-sun2000 is the VENDOR-MAP path for the single most-installed inverter in the
		//     world (Huawei led global inverter shipments for a decade). It is NOT fully SunSpec —
		//     its battery (SOC/charge-discharge) and built-in meter live in vendor registers — so
		//     every key carries an explicit register binding. It is the worked example a site copies
		//     to onboard any other non-SunSpec hybrid: a new device is a new map, not new code.
		{
			Slug:        "generic-sunspec-solar",
			Name:        "Generic SunSpec inverter / meter / battery",
			Vendor:      "SunSpec (SolarEdge, SMA, Fronius, …)",
			Description: "Self-describing SunSpec device over Modbus TCP. Auto-discovers the model chain, so one profile reads any compliant inverter, meter or hybrid — no per-model register map.",
			Transport:   "modbus",
			ModbusMode:  "sunspec",
			ModbusBase:  0, // auto-discover (40000 / 50000 / 0)
			PollSeconds: 10,
			// Keys are the role-prefixed datapoints the SunSpec decoder emits (inv_/grid_/batt_/ctl_).
			// A device that lacks a block (a meter with no battery) simply never sends those keys.
			Keys: []SaveTelemetryKey{
				{Key: "inv_ac_power", Label: "Inverter AC power", Unit: "W", DataType: "number", Deadband: 50, HeartbeatSeconds: 300, Min: -100000, Max: 100000},
				{Key: "inv_dc_power", Label: "Inverter DC (PV) power", Unit: "W", DataType: "number", Deadband: 50, HeartbeatSeconds: 300, Min: 0, Max: 100000},
				{Key: "inv_ac_energy", Label: "Inverter lifetime yield", Unit: "Wh", DataType: "number", Deadband: 100, HeartbeatSeconds: 3600},
				{Key: "inv_frequency", Label: "Grid frequency", Unit: "Hz", DataType: "number", Deadband: 0.05, HeartbeatSeconds: 900, Min: 45, Max: 65},
				{Key: "inv_ac_voltage", Label: "Inverter AC voltage", Unit: "V", DataType: "number", Deadband: 2, HeartbeatSeconds: 900, Min: 0, Max: 500},
				{Key: "inv_temperature", Label: "Inverter temperature", Unit: "C", DataType: "number", Deadband: 1, HeartbeatSeconds: 900, Min: -40, Max: 120},
				{Key: "inv_operating_state", Label: "Inverter state", DataType: "number", Deadband: 0, HeartbeatSeconds: 600},
				{Key: "batt_soc", Label: "Battery state of charge", Unit: "%", DataType: "number", Deadband: 1, HeartbeatSeconds: 600, Min: 0, Max: 100},
				{Key: "batt_charge_status", Label: "Battery charge status", DataType: "number", Deadband: 0, HeartbeatSeconds: 600},
				{Key: "batt_battery_voltage", Label: "Battery voltage", Unit: "V", DataType: "number", Deadband: 2, HeartbeatSeconds: 900, Min: 0, Max: 1000},
				// The site's net grid flow: + import, - export. The sign is the whole story here.
				{Key: "grid_ac_power", Label: "Grid power (+import/-export)", Unit: "W", DataType: "number", Deadband: 50, HeartbeatSeconds: 300, Min: -100000, Max: 100000},
				{Key: "grid_energy_imported", Label: "Grid energy imported", Unit: "Wh", DataType: "number", Deadband: 100, HeartbeatSeconds: 3600},
				{Key: "grid_energy_exported", Label: "Grid energy exported", Unit: "Wh", DataType: "number", Deadband: 100, HeartbeatSeconds: 3600},
				// Curtailment set-point, read back so a guarded control write can be confirmed.
				{Key: "ctl_w_max_lim_pct", Label: "Export limit", Unit: "%", DataType: "number", Deadband: 0, HeartbeatSeconds: 900, Min: 0, Max: 100},
			},
		},
		{
			Slug:        "huawei-sun2000",
			Name:        "Huawei SUN2000 (hybrid inverter)",
			Vendor:      "Huawei",
			Description: "The world's most-installed PV inverter, read over Modbus TCP. The inverter block is SunSpec-ish but the LUNA battery and built-in power meter are vendor registers, so this profile maps them explicitly. Register map per Huawei's Solar Inverter Modbus Interface Definitions.",
			Transport:   "modbus",
			ModbusMode:  "regmap",
			PollSeconds: 10,
			// Register bindings from Huawei's published map. Scale is a MULTIPLIER (raw * scale):
			// a gain-of-10 register (0.1-unit) has scale 0.1. The battery and meter blocks sit far
			// from the inverter block (37xxx vs 32xxx); the driver reads them as separate clusters.
			Keys: []SaveTelemetryKey{
				{Key: "inv_active_power", Label: "Active power", Unit: "W", DataType: "number", Register: 32080, RegKind: "i32", ScaleFactor: 1, Deadband: 50, HeartbeatSeconds: 300, Min: -100000, Max: 100000},
				{Key: "inv_input_power", Label: "PV input power", Unit: "W", DataType: "number", Register: 32064, RegKind: "i32", ScaleFactor: 1, Deadband: 50, HeartbeatSeconds: 300, Min: 0, Max: 100000},
				{Key: "inv_pv1_voltage", Label: "PV1 voltage", Unit: "V", DataType: "number", Register: 32016, RegKind: "i16", ScaleFactor: 0.1, Deadband: 2, HeartbeatSeconds: 900, Min: 0, Max: 1500},
				{Key: "inv_pv1_current", Label: "PV1 current", Unit: "A", DataType: "number", Register: 32017, RegKind: "i16", ScaleFactor: 0.01, Deadband: 0.2, HeartbeatSeconds: 900, Min: 0, Max: 100},
				{Key: "inv_pv2_voltage", Label: "PV2 voltage", Unit: "V", DataType: "number", Register: 32018, RegKind: "i16", ScaleFactor: 0.1, Deadband: 2, HeartbeatSeconds: 900, Min: 0, Max: 1500},
				{Key: "inv_pv2_current", Label: "PV2 current", Unit: "A", DataType: "number", Register: 32019, RegKind: "i16", ScaleFactor: 0.01, Deadband: 0.2, HeartbeatSeconds: 900, Min: 0, Max: 100},
				{Key: "inv_grid_frequency", Label: "Grid frequency", Unit: "Hz", DataType: "number", Register: 32085, RegKind: "u16", ScaleFactor: 0.01, Deadband: 0.05, HeartbeatSeconds: 900, Min: 45, Max: 65},
				{Key: "inv_phase_a_voltage", Label: "Phase A voltage", Unit: "V", DataType: "number", Register: 32069, RegKind: "u16", ScaleFactor: 0.1, Deadband: 2, HeartbeatSeconds: 900, Min: 0, Max: 500},
				{Key: "inv_temperature", Label: "Internal temperature", Unit: "C", DataType: "number", Register: 32087, RegKind: "i16", ScaleFactor: 0.1, Deadband: 1, HeartbeatSeconds: 900, Min: -40, Max: 120},
				{Key: "inv_operating_state", Label: "Device status", DataType: "number", Register: 32089, RegKind: "u16", ScaleFactor: 1, Deadband: 0, HeartbeatSeconds: 600},
				{Key: "inv_yield_total", Label: "Lifetime yield", Unit: "kWh", DataType: "number", Register: 32106, RegKind: "u32", ScaleFactor: 0.01, Deadband: 1, HeartbeatSeconds: 3600},
				// LUNA battery — vendor registers, not SunSpec. Power is + charging / - discharging.
				{Key: "batt_soc", Label: "Battery SOC", Unit: "%", DataType: "number", Register: 37760, RegKind: "u16", ScaleFactor: 0.1, Deadband: 1, HeartbeatSeconds: 600, Min: 0, Max: 100},
				{Key: "batt_power", Label: "Battery power (+charge/-discharge)", Unit: "W", DataType: "number", Register: 37765, RegKind: "i32", ScaleFactor: 1, Deadband: 50, HeartbeatSeconds: 300, Min: -50000, Max: 50000},
				// Built-in power meter — + import / - export.
				{Key: "grid_active_power", Label: "Grid power (+import/-export)", Unit: "W", DataType: "number", Register: 37113, RegKind: "i32", ScaleFactor: 1, Deadband: 50, HeartbeatSeconds: 300, Min: -100000, Max: 100000},
			},
		},
	}
}
