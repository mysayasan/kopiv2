package modbus

import (
	"fmt"
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

// bankReader answers holding and input registers from two separate banks, so a test can prove a
// map with mixed Input points reads each from the correct function code.
type bankReader struct {
	holding map[int]uint16
	input   map[int]uint16
	reads   []string // "H@addr" / "I@addr" per call, to assert the bank each cluster hit
}

func (b *bankReader) ReadHolding(addr, qty int) ([]uint16, error) {
	b.reads = append(b.reads, fmt.Sprintf("H@%d", addr))
	out := make([]uint16, qty)
	for i := range out {
		out[i] = b.holding[addr+i]
	}
	return out, nil
}

func (b *bankReader) ReadInput(addr, qty int) ([]uint16, error) {
	b.reads = append(b.reads, fmt.Sprintf("I@%d", addr))
	out := make([]uint16, qty)
	for i := range out {
		out[i] = b.input[addr+i]
	}
	return out, nil
}

// TestRegisterMapFloatAndInput exercises the two additions the cheap-meter/Sungrow profiles need:
// an IEEE-754 float32 point, and a point read from the INPUT bank (fn 4) rather than holding (fn 3).
// An Eastron SDM630 reports 230.0 V as float32 big-endian; a Sungrow-style word-swapped u32 reports
// power low-word-first. The two banks must be read by different function codes and never merged.
func TestRegisterMapFloatAndInput(t *testing.T) {
	// 230.0f32 big-endian = 0x43660000 -> hi=0x4366, lo=0x0000.
	m := RegisterMap{Points: []Point{
		{Key: "voltage", Register: 0, Type: PF32, Scale: 1, Input: true},           // SDM630 float, input bank
		{Key: "dc_power", Register: 5016, Type: PU32, Scale: 1, WordSwap: true, Input: true}, // Sungrow word-swap, input
		{Key: "ems_mode", Register: 13049, Type: PU16, Scale: 1},                    // a holding-bank point
	}}
	r := &bankReader{
		input: map[int]uint16{
			0: 0x4366, 1: 0x0000, // 230.0 V
			5016: 0x1388, 5017: 0x0000, // word-swapped: low word first (0x1388=5000) -> 5000 W
		},
		holding: map[int]uint16{13049: 2},
	}
	samples, err := m.Read(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := map[string]float64{}
	for _, s := range samples {
		got[s.Key] = s.Num
	}
	if math.Abs(got["voltage"]-230) > 0.01 {
		t.Errorf("voltage = %v, want 230", got["voltage"])
	}
	if got["dc_power"] != 5000 {
		t.Errorf("dc_power = %v, want 5000 (word-swapped)", got["dc_power"])
	}
	if got["ems_mode"] != 2 {
		t.Errorf("ems_mode = %v, want 2", got["ems_mode"])
	}
	// The holding point and the input points must be fetched by different function codes.
	var sawH, sawI bool
	for _, rd := range r.reads {
		if rd[0] == 'H' {
			sawH = true
		}
		if rd[0] == 'I' {
			sawI = true
		}
	}
	if !sawH || !sawI {
		t.Errorf("expected both a holding and an input read, got %v", r.reads)
	}
}

// TestRegisterMapInputWithoutReader proves a map that binds an input-register point but is handed a
// holding-only reader fails loudly rather than silently reading the wrong bank.
func TestRegisterMapInputWithoutReader(t *testing.T) {
	m := RegisterMap{Points: []Point{{Key: "v", Register: 0, Type: PU16, Input: true}}}
	if _, err := m.Read(fakeReader{}); err == nil {
		t.Fatal("expected an error when an input point is read by a holding-only reader")
	}
}

// countingReader records every read so a test can assert how many round trips a map costs, and
// rejects any read wider than the Modbus 125-register limit — the exact failure a naive single-span
// read of a Huawei-style scattered map would hit against real hardware.
type countingReader struct {
	regs  map[int]uint16
	reads [][2]int // {addr, qty} per call
}

func (c *countingReader) ReadHolding(addr, qty int) ([]uint16, error) {
	if qty < 1 || qty > maxReadRegisters {
		return nil, fmt.Errorf("qty %d exceeds modbus limit", qty)
	}
	c.reads = append(c.reads, [2]int{addr, qty})
	out := make([]uint16, qty)
	for i := range out {
		out[i] = c.regs[addr+i]
	}
	return out, nil
}

// TestRegisterMapClusters proves a map whose points are scattered wider than the 125-register read
// limit — a Huawei SUN2000 keeps its inverter block near 32000 and its battery near 37000 — is read
// as several bounded requests, one per block, rather than one impossible 5,700-register read.
func TestRegisterMapClusters(t *testing.T) {
	m := RegisterMap{Points: []Point{
		{Key: "ac_power", Register: 32080, Type: PI32, Scale: 1},    // inverter block
		{Key: "pv1_voltage", Register: 32016, Type: PI16, Scale: 0.1},
		{Key: "batt_soc", Register: 37760, Type: PU16, Scale: 0.1},  // battery block, ~5700 regs away
		{Key: "batt_power", Register: 37765, Type: PI32, Scale: 1},
	}}
	r := &countingReader{regs: map[int]uint16{
		32016: w(3600),                       // 360.0 V
		32080: 0, 32081: 5000,                // 5000 W
		37760: 875,                            // 87.5 %
		37765: 0xFFFF, 37766: w(-1200),       // -1200 W discharge (i32: hi=0xFFFF, lo=0xFB50)
	}}
	samples, err := m.Read(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(r.reads) != 2 {
		t.Fatalf("expected 2 clustered reads (inverter + battery), got %d: %v", len(r.reads), r.reads)
	}
	got := map[string]float64{}
	for _, s := range samples {
		got[s.Key] = s.Num
	}
	if math.Abs(got["pv1_voltage"]-360) > 0.5 {
		t.Errorf("pv1_voltage = %v, want 360", got["pv1_voltage"])
	}
	if got["ac_power"] != 5000 {
		t.Errorf("ac_power = %v, want 5000", got["ac_power"])
	}
	if math.Abs(got["batt_soc"]-87.5) > 0.05 {
		t.Errorf("batt_soc = %v, want 87.5", got["batt_soc"])
	}
	if got["batt_power"] != -1200 {
		t.Errorf("batt_power = %v, want -1200", got["batt_power"])
	}
}
