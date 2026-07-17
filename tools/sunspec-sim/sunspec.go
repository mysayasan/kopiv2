package main

// SunSpec information model, laid out over Modbus holding registers.
//
// SunSpec is NOT a wire protocol — it is a self-describing data model that rides on plain
// Modbus. A compliant device places, starting at a well-known base register (40000 / 50000 / 0),
// the ASCII marker "SunS" followed by a chain of MODELS:
//
//	[ "SunS" (2 regs) ] [ model ] [ model ] ... [ 0xFFFF ] [ 0x0000 ]
//
// and each model is:
//
//	[ modelID (1 reg) ] [ length L (1 reg) ] [ L registers of data ]
//
// A reader walks the chain: read the id, read the length, decode the L data registers using the
// STANDARD definition for that model id, then jump L+2 registers to the next model. That is the
// whole trick, and it is why one reader handles any SunSpec inverter, meter or battery without a
// per-model register map — exactly the "don't code per model" goal.
//
// This file builds a real chain (Common + Inverter + Storage + Immediate-Controls + Meter) into a
// register bank. The physics in plant.go pokes live values into it by name; the Modbus server in
// modbus.go serves it to a client.

import "sync"

const (
	sunsHi = 0x5375 // "Su"
	sunsLo = 0x6E53 // "nS"
	endID  = 0xFFFF // terminates the model chain
)

// Bank is the Modbus holding-register space. The slice index IS the Modbus register address, so a
// client asking for register 40000 reads regs[40000]. Allocating up to base+len is a few tens of
// KB — trivial, and it makes address→index a direct map with no offset bookkeeping.
type Bank struct {
	mu   sync.RWMutex
	regs []uint16
}

func newBank(topAddr int) *Bank {
	return &Bank{regs: make([]uint16, topAddr+8)}
}

// readRange copies [start, start+count) out under the read lock. ok is false if the range falls
// outside the bank, which the server turns into a Modbus "illegal data address" exception.
func (b *Bank) readRange(start, count int) (out []uint16, ok bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if start < 0 || count < 0 || start+count > len(b.regs) {
		return nil, false
	}
	out = make([]uint16, count)
	copy(out, b.regs[start:start+count])
	return out, true
}

// writeReg sets one register under the write lock (Modbus fn 6 / fn 16).
func (b *Bank) writeReg(addr int, v uint16) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if addr < 0 || addr >= len(b.regs) {
		return false
	}
	b.regs[addr] = v
	return true
}

// set / raw are the UNLOCKED accessors used by the plant, which takes the bank lock ONCE around
// its whole tick (encoding a couple of hundred registers) instead of locking per register.
func (b *Bank) set(addr int, v uint16) { b.regs[addr] = v }
func (b *Bank) raw(addr int) uint16    { return b.regs[addr] }

// --- model builder -------------------------------------------------------------------------

type ptKind int

const (
	kU16 ptKind = iota // unsigned 16-bit
	kI16               // signed 16-bit (two's complement)
	kU32               // unsigned 32-bit accumulator (acc32): hi word first
	kSF                // sunssf: signed scale-factor register
	kStr               // fixed-length string, 2 chars/reg, null-padded
	kPad               // reserved / not-populated registers
)

type point struct {
	name string
	kind ptKind
	regs int // register count (1 for u16/i16/sf, 2 for u32, N for a string/pad)
}

func u16(n string) point  { return point{n, kU16, 1} }
func i16(n string) point  { return point{n, kI16, 1} }
func u32(n string) point  { return point{n, kU32, 2} }
func sf(n string) point   { return point{n, kSF, 1} }
func str(n string, r int) point { return point{n, kStr, r} }
func pad(r int) point     { return point{"", kPad, r} }

// Model is one placed SunSpec model: its id, and the absolute Modbus address of every named point.
type Model struct {
	id   int
	addr map[string]int
	run  map[string]int // point name -> register count (for strings/multi-reg points)
	len  int
}

// place writes [id][len] and reserves the data block at cursor, recording each point's absolute
// address, and returns the cursor for the next model.
func place(b *Bank, cursor, id int, pts []point) (*Model, int) {
	length := 0
	for _, p := range pts {
		length += p.regs
	}
	b.set(cursor, uint16(id))
	b.set(cursor+1, uint16(length))
	m := &Model{id: id, addr: map[string]int{}, run: map[string]int{}, len: length}
	off := cursor + 2
	for _, p := range pts {
		if p.name != "" {
			m.addr[p.name] = off
			m.run[p.name] = p.regs
		}
		off += p.regs
	}
	return m, cursor + 2 + length
}

func (m *Model) at(name string) int {
	a, ok := m.addr[name]
	if !ok {
		panic("sunspec: unknown point " + name + " in model")
	}
	return a
}

func (m *Model) setU16(b *Bank, name string, v uint16) { b.set(m.at(name), v) }
func (m *Model) setI16(b *Bank, name string, v int16)  { b.set(m.at(name), uint16(v)) }
func (m *Model) setSF(b *Bank, name string, s int16)   { b.set(m.at(name), uint16(s)) }

func (m *Model) getU16(b *Bank, name string) uint16 { return b.raw(m.at(name)) }

func (m *Model) setU32(b *Bank, name string, v uint32) {
	a := m.at(name)
	b.set(a, uint16(v>>16))
	b.set(a+1, uint16(v&0xFFFF))
}

// setScaledU16 stores a real-world value against a scale factor: stored = round(actual / 10^sf).
// e.g. 6.34 A at sf=-2 stores 634. This is how SunSpec keeps integers exact without floats.
func (m *Model) setScaledU16(b *Bank, name string, actual float64, s int) {
	m.setU16(b, name, uint16(clampU16(scaleStore(actual, s))))
}
func (m *Model) setScaledI16(b *Bank, name string, actual float64, s int) {
	m.setI16(b, name, int16(clampI16(scaleStore(actual, s))))
}
func (m *Model) setScaledU32(b *Bank, name string, actual float64, s int) {
	v := scaleStore(actual, s)
	if v < 0 {
		v = 0
	}
	m.setU32(b, name, uint32(v))
}

func (m *Model) setStr(b *Bank, name, sVal string) {
	a := m.at(name)
	regs := m.run[name]
	buf := make([]byte, regs*2)
	copy(buf, sVal) // remainder stays 0 (null padding)
	for i := 0; i < regs; i++ {
		b.set(a+i, uint16(buf[2*i])<<8|uint16(buf[2*i+1]))
	}
}

// --- scale-factor helpers ------------------------------------------------------------------

// scaleStore converts a real value to its stored integer against a scale factor:
// stored = round(actual / 10^sf). e.g. 6.34 A at sf=-2 -> 634; 4200 W at sf=0 -> 4200.
func scaleStore(actual float64, s int) int64 {
	f := actual
	for i := 0; i < -s; i++ {
		f *= 10
	}
	for i := 0; i < s; i++ {
		f /= 10
	}
	if f >= 0 {
		return int64(f + 0.5)
	}
	return int64(f - 0.5)
}

func clampU16(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > 0xFFFF {
		return 0xFFFF
	}
	return v
}

func clampI16(v int64) int64 {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return v
}
