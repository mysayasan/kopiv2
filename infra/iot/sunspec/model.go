// Package sunspec decodes the SunSpec information model that rides on Modbus holding registers.
//
// A SunSpec device publishes, at a well-known base register, the ASCII marker "SunS" followed by
// a self-describing chain of MODELS — [id][length][data...] — terminated by 0xFFFF. Because each
// model's layout is standardised by id, a reader that knows the standard models decodes ANY
// compliant inverter, meter or battery with no per-vendor register map. That is the whole value:
// one driver, every SunSpec device.
//
// This package knows the common integer models a solar site uses. Vendors that are NOT SunSpec are
// handled by the manual register map in infra/iot/modbus, not here.
package sunspec

// Reader is the minimal Modbus surface the decoder needs: read N holding registers from an address.
// infra/iot/modbus.Client satisfies it, and tests supply an in-memory fake.
type Reader interface {
	ReadHolding(addr, qty int) ([]uint16, error)
}

// Kind is how a field's registers are interpreted.
type Kind int

const (
	KU16   Kind = iota // unsigned 16-bit
	KI16               // signed 16-bit
	KAcc32             // unsigned 32-bit accumulator, hi word first
	KEnum              // unsigned 16-bit status/enum (never scaled)
)

// Field is one decoded datapoint within a model: the telemetry key it becomes, where its value and
// its scale factor live (as register offsets into the model's DATA block), and its unit.
type Field struct {
	Key   string
	Off   int    // offset of the value register(s) within the data block
	Kind  Kind
	SFOff int    // offset of the scale-factor register, or -1 for none
	Unit  string
}

// ModelDef is the subset of a SunSpec model this package decodes.
type ModelDef struct {
	ID     int
	Role   string // "inv" | "grid" | "batt" | "ctl" — prefixes the emitted keys so a hybrid
	// inverter's built-in meter (model 203) does not collide with its inverter block (model 103).
	Name   string
	Fields []Field
}

// Inverter models 101 (single phase) / 102 (split) / 103 (three phase), integer + scale factor —
// identical field layout, differing only in how many phase registers are populated.
var inverterFields = []Field{
	{Key: "ac_power", Off: 12, Kind: KI16, SFOff: 13, Unit: "W"},
	{Key: "ac_current", Off: 0, Kind: KU16, SFOff: 4, Unit: "A"},
	{Key: "ac_voltage", Off: 8, Kind: KU16, SFOff: 11, Unit: "V"}, // PhVphA
	{Key: "frequency", Off: 14, Kind: KU16, SFOff: 15, Unit: "Hz"},
	{Key: "ac_energy", Off: 22, Kind: KAcc32, SFOff: 24, Unit: "Wh"},
	{Key: "dc_power", Off: 29, Kind: KI16, SFOff: 30, Unit: "W"},
	{Key: "dc_voltage", Off: 27, Kind: KU16, SFOff: 28, Unit: "V"},
	{Key: "temperature", Off: 31, Kind: KI16, SFOff: 35, Unit: "C"},
	{Key: "operating_state", Off: 36, Kind: KEnum, SFOff: -1},
}

// Meter models 201 / 202 / 203 / 204 (single / split / wye / delta), integer + scale factor.
var meterFields = []Field{
	{Key: "ac_power", Off: 16, Kind: KI16, SFOff: 20, Unit: "W"}, // + import, - export
	{Key: "ac_current", Off: 0, Kind: KI16, SFOff: 4, Unit: "A"},
	{Key: "ac_voltage", Off: 5, Kind: KI16, SFOff: 13, Unit: "V"},
	{Key: "frequency", Off: 14, Kind: KI16, SFOff: 15, Unit: "Hz"},
	{Key: "energy_exported", Off: 36, Kind: KAcc32, SFOff: 52, Unit: "Wh"},
	{Key: "energy_imported", Off: 44, Kind: KAcc32, SFOff: 52, Unit: "Wh"},
}

// Storage model 124 (battery control).
var storageFields = []Field{
	{Key: "soc", Off: 6, Kind: KU16, SFOff: 20, Unit: "%"},   // ChaState
	{Key: "charge_status", Off: 9, Kind: KEnum, SFOff: -1},   // ChaSt
	{Key: "storage_ctl_mode", Off: 3, Kind: KEnum, SFOff: -1}, // StorCtl_Mod (read back after a write)
	{Key: "battery_voltage", Off: 8, Kind: KU16, SFOff: 22, Unit: "V"},
	{Key: "charge_max", Off: 0, Kind: KU16, SFOff: 16, Unit: "W"},
}

// Immediate controls model 123 — the writable curtailment block; decoded so a guarded write can be
// confirmed by reading the value back.
var controlFields = []Field{
	{Key: "w_max_lim_pct", Off: 3, Kind: KU16, SFOff: 21, Unit: "%"},
	{Key: "w_max_lim_ena", Off: 7, Kind: KEnum, SFOff: -1},
}

var registry = func() map[int]ModelDef {
	m := map[int]ModelDef{}
	for _, id := range []int{101, 102, 103} {
		m[id] = ModelDef{ID: id, Role: "inv", Name: "inverter", Fields: inverterFields}
	}
	for _, id := range []int{201, 202, 203, 204} {
		m[id] = ModelDef{ID: id, Role: "grid", Name: "meter", Fields: meterFields}
	}
	m[124] = ModelDef{ID: 124, Role: "batt", Name: "storage", Fields: storageFields}
	m[123] = ModelDef{ID: 123, Role: "ctl", Name: "controls", Fields: controlFields}
	return m
}()

// Known reports whether a model id is one this package decodes.
func Known(id int) bool { _, ok := registry[id]; return ok }
