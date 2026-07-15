package modbus

import (
	"math"
	"testing"
)

// w encodes a possibly-negative value as a two's-complement register (a constant conversion would
// overflow uint16 at compile time).
func w(v int) uint16 { return uint16(int16(v)) }

type fakeReader map[int]uint16

func (f fakeReader) ReadHolding(addr, qty int) ([]uint16, error) {
	out := make([]uint16, qty)
	for i := range out {
		out[i] = f[addr+i]
	}
	return out, nil
}

// TestRegisterMapDecode decodes a non-SunSpec vendor block: a 32-bit power, a signed 16-bit battery
// power, and a plain SoC — each with its own scale — exercising the manual-map path that compliant
// SunSpec devices don't need but cheap hybrids do.
func TestRegisterMapDecode(t *testing.T) {
	m := RegisterMap{Points: []Point{
		{Key: "pv_power", Register: 3, Type: PU32, Scale: 0.1},   // 0.1 W device
		{Key: "batt_power", Register: 14, Type: PI16, Scale: 1},  // signed W, + charge
		{Key: "soc", Register: 13, Type: PU16, Scale: 1},         // %
	}}

	// Ppv = 52340 (raw) -> hi/lo words; * 0.1 = 5234.0 W
	f := fakeReader{
		3:  uint16(52340 >> 16),
		4:  uint16(52340 & 0xFFFF),
		13: 87,                    // SoC 87 %
		14: w(-300),   // discharging 300 W
	}

	samples, err := m.Read(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := map[string]float64{}
	for _, s := range samples {
		got[s.Key] = s.Num
	}
	if math.Abs(got["pv_power"]-5234) > 0.5 {
		t.Errorf("pv_power = %v, want 5234", got["pv_power"])
	}
	if math.Abs(got["batt_power"]-(-300)) > 0.5 {
		t.Errorf("batt_power = %v, want -300", got["batt_power"])
	}
	if got["soc"] != 87 {
		t.Errorf("soc = %v, want 87", got["soc"])
	}
}

// TestRegisterMapSingleRead proves the whole map is fetched in ONE Modbus round trip: the span from
// the lowest to the highest register, not one read per point.
func TestRegisterMapSingleRead(t *testing.T) {
	m := RegisterMap{Points: []Point{
		{Key: "a", Register: 3, Type: PU32}, // spans 3..4
		{Key: "b", Register: 14, Type: PI16},
	}}
	lo, count, ok := m.span()
	if !ok || lo != 3 || count != 12 { // 3..14 inclusive = 12 registers
		t.Fatalf("span = (%d,%d,%v), want (3,12,true)", lo, count, ok)
	}
}
