package services

import (
	"math"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/infra/iot/modbus"
)

// fakeBank is a Modbus reader backed by a raw register map; it satisfies modbus.Reader so a test can
// decode a RegisterMap without a live device.
type fakeBank map[int]uint16

func (f fakeBank) ReadHolding(addr, qty int) ([]uint16, error) {
	out := make([]uint16, qty)
	for i := range out {
		out[i] = f[addr+i]
	}
	return out, nil
}

func toEntityKeys(keys []SaveTelemetryKey) []*entities.TelemetryKey {
	out := make([]*entities.TelemetryKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, &entities.TelemetryKey{
			Key: k.Key, Unit: k.Unit, Register: k.Register,
			RegKind: k.RegKind, ScaleFactor: k.ScaleFactor, WordSwap: k.WordSwap,
		})
	}
	return out
}

func findBuiltin(t *testing.T, slug string) builtinProfile {
	t.Helper()
	for _, b := range builtinProfiles() {
		if b.Slug == slug {
			return b
		}
	}
	t.Fatalf("builtin profile %q not found", slug)
	return builtinProfile{}
}

// TestPtypeOf pins the register-kind mapping and its rejection of nonsense.
func TestPtypeOf(t *testing.T) {
	cases := map[string]modbus.PType{"u16": modbus.PU16, "I16": modbus.PI16, " u32 ": modbus.PU32, "i32": modbus.PI32}
	for in, want := range cases {
		got, err := ptypeOf(in)
		if err != nil || got != want {
			t.Errorf("ptypeOf(%q) = (%v,%v), want (%v,nil)", in, got, err, want)
		}
	}
	if _, err := ptypeOf("f32"); err == nil {
		t.Error("ptypeOf(f32) should error")
	}
}

// TestGenericSunSpecHasNoRegisterBindings guards the design: a SunSpec profile is DISCOVERED, so it
// must not carry register bindings — building a register map from it is correctly an error, because
// its keys are decoded from the walked model chain, not from declared registers.
func TestGenericSunSpecHasNoRegisterBindings(t *testing.T) {
	b := findBuiltin(t, "generic-sunspec-solar")
	if b.Transport != "modbus" || b.ModbusMode != "sunspec" {
		t.Fatalf("generic-sunspec-solar transport/mode = %q/%q, want modbus/sunspec", b.Transport, b.ModbusMode)
	}
	if _, err := registerMapFromKeys(toEntityKeys(b.Keys)); err == nil {
		t.Error("a SunSpec profile should declare no register bindings, so registerMapFromKeys must error")
	}
}

// TestHuaweiBuiltinDecodes is the hermetic end-to-end proof of the shipped huawei-sun2000 profile:
// its declared keys become a register map, and that map decodes a bank of Huawei-format raw
// registers into the right physical values — including the scale and the two's-complement SIGN on a
// grid export and a battery discharge, the classic solar footgun. It needs neither a DB nor the
// simulator, so it runs in the normal unit suite.
func TestHuaweiBuiltinDecodes(t *testing.T) {
	b := findBuiltin(t, "huawei-sun2000")
	if b.Transport != "modbus" || b.ModbusMode != "regmap" {
		t.Fatalf("huawei-sun2000 transport/mode = %q/%q, want modbus/regmap", b.Transport, b.ModbusMode)
	}
	m, err := registerMapFromKeys(toEntityKeys(b.Keys))
	if err != nil {
		t.Fatalf("build register map from huawei builtin: %v", err)
	}

	// A bank in Huawei's published units: raw = real / scale, 32-bit values hi-word-first.
	i32 := func(v int32) (uint16, uint16) { u := uint32(v); return uint16(u >> 16), uint16(u) }
	acHi, acLo := i32(5000)      // 5000 W active
	pinHi, pinLo := i32(6100)    // 6100 W PV input
	gridHi, gridLo := i32(-5200) // 5200 W EXPORT (negative)
	battHi, battLo := i32(-1800) // 1800 W DISCHARGE (negative)
	yHi, yLo := func() (uint16, uint16) { u := uint32(1234567); return uint16(u >> 16), uint16(u) }()
	bank := fakeBank{
		32080: acHi, 32081: acLo,
		32064: pinHi, 32065: pinLo,
		32016: uint16(int16(3602)), // 360.2 V (i16, 0.1)
		32085: 4998,                // 49.98 Hz (u16, 0.01)
		32087: uint16(int16(455)),  // 45.5 C  (i16, 0.1)
		32106: yHi, 32107: yLo,     // 12345.67 kWh (u32, 0.01)
		37760: 855,                 // 85.5 %  (u16, 0.1)
		37765: battHi, 37766: battLo,
		37113: gridHi, 37114: gridLo,
	}

	got := map[string]float64{}
	samples, err := m.Read(bank)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, s := range samples {
		got[s.Key] = s.Num
	}

	want := map[string]float64{
		"inv_active_power":   5000,
		"inv_input_power":    6100,
		"inv_pv1_voltage":    360.2,
		"inv_grid_frequency": 49.98,
		"inv_temperature":    45.5,
		"inv_yield_total":    12345.67,
		"batt_soc":           85.5,
		"batt_power":         -1800, // discharge stays negative
		"grid_active_power":  -5200, // export stays negative
	}
	for k, w := range want {
		if math.Abs(got[k]-w) > 0.01 {
			t.Errorf("%s = %v, want %v", k, got[k], w)
		}
	}
}
