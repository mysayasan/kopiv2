package sunspec

import (
	"math"
	"testing"
)

// w encodes a (possibly negative) value as a two's-complement register. Needed because a constant
// conversion like w(-2) overflows at compile time; routing through a func makes it a
// runtime conversion.
func w(v int) uint16 { return uint16(int16(v)) }

// fakeReader is an in-memory register bank; missing addresses read as 0.
type fakeReader map[int]uint16

func (f fakeReader) ReadHolding(addr, qty int) ([]uint16, error) {
	out := make([]uint16, qty)
	for i := range out {
		out[i] = f[addr+i]
	}
	return out, nil
}

func sampleMap(t *testing.T, blocks []Block) map[string]float64 {
	t.Helper()
	m := map[string]float64{}
	for _, s := range DecodeDevice(blocks) {
		m[s.Key] = s.Num
	}
	return m
}

// TestDecodeInverterScaling checks each integer kind and the scale-factor arithmetic that is the
// heart of SunSpec: a value register times 10^(its scale-factor register).
func TestDecodeInverterScaling(t *testing.T) {
	regs := make([]uint16, 50)
	// ac_power: W(off12) = 4200, W_SF(off13) = 0  -> 4200 W
	regs[12] = uint16(int16(4200))
	regs[13] = 0
	// ac_current: A(off0) = 634, A_SF(off4) = -2  -> 6.34 A
	regs[0] = 634
	regs[4] = w(-2)
	// ac_energy: WH(off22..23) = 123456, WH_SF(off24) = 0
	regs[22] = uint16(123456 >> 16)
	regs[23] = uint16(123456 & 0xFFFF)
	regs[24] = 0
	// temperature: TmpCab(off31) = -50 (i.e. -5.0 C), Tmp_SF(off35) = -1
	regs[31] = w(-50)
	regs[35] = w(-1)
	// operating_state: St(off36) = 4 (MPPT), no scale factor
	regs[36] = 4

	m := sampleMap(t, []Block{{ID: 103, Regs: regs}})
	assertNear(t, "inv_ac_power", m["inv_ac_power"], 4200)
	assertNear(t, "inv_ac_current", m["inv_ac_current"], 6.34)
	assertNear(t, "inv_ac_energy", m["inv_ac_energy"], 123456)
	assertNear(t, "inv_temperature", m["inv_temperature"], -5.0)
	assertNear(t, "inv_operating_state", m["inv_operating_state"], 4)
}

// TestDecodeRolePrefix proves a hybrid chain (inverter + meter + battery) does NOT collide on
// "ac_power": the role prefix keeps inverter output and grid flow distinct.
func TestDecodeRolePrefix(t *testing.T) {
	inv := make([]uint16, 50)
	inv[12] = uint16(int16(5000)) // inverter producing 5000 W
	mtr := make([]uint16, 105)
	mtr[16] = w(-1500) // meter exporting 1500 W
	sto := make([]uint16, 24)
	sto[6] = 73 // SoC 73 %

	m := sampleMap(t, []Block{{ID: 103, Regs: inv}, {ID: 203, Regs: mtr}, {ID: 124, Regs: sto}})
	assertNear(t, "inv_ac_power", m["inv_ac_power"], 5000)
	assertNear(t, "grid_ac_power", m["grid_ac_power"], -1500)
	assertNear(t, "batt_soc", m["batt_soc"], 73)
}

// TestWalkChain builds a minimal SunSpec chain in a fake bank and walks it the way the driver does.
func TestWalkChain(t *testing.T) {
	const base = 40000
	f := fakeReader{
		base:     markerHi,
		base + 1: markerLo,
		base + 2: 103, // model id
		base + 3: 50,  // length
	}
	f[base+4+12] = uint16(int16(4200)) // W in the data block
	f[base+2+2+50] = 0xFFFF            // terminator right after the model

	base2, blocks, err := Discover(f)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if base2 != base {
		t.Fatalf("discovered base %d, want %d", base2, base)
	}
	if len(blocks) != 1 || blocks[0].ID != 103 {
		t.Fatalf("expected one model 103, got %+v", blocks)
	}
	if got := sampleMap(t, blocks)["inv_ac_power"]; math.Abs(got-4200) > 0.5 {
		t.Fatalf("inv_ac_power = %v, want 4200", got)
	}
}

// TestNoMarker: a device without "SunS" is reported as such (so the caller can fall back to a map).
func TestNoMarker(t *testing.T) {
	if _, _, err := Discover(fakeReader{}); err != ErrNoMarker {
		t.Fatalf("want ErrNoMarker, got %v", err)
	}
}

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
