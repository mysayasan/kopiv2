package modbus

import (
	"fmt"

	"github.com/mysayasan/kopiv2/infra/iot/codec"
)

// A RegisterMap is how a NON-SunSpec device is read: an explicit, site-authored list of which
// register holds which value, in what integer type, at what scale. SunSpec devices are
// self-describing and need none of this; the many cheaper hybrids that expose a flat vendor
// register block (no "SunS" marker, no model chain) need exactly this. It is the data-driven
// "don't code per model" answer for the non-compliant case: a new device is a new map, not new code.
//
// This is the shape the future device_profile's Modbus binding takes — the thing telemetry_key.go
// lacks today (it only knows a JSON path).

// PType is the register encoding of a mapped point.
type PType int

const (
	PU16 PType = iota // unsigned 16-bit
	PI16              // signed 16-bit
	PU32              // unsigned 32-bit (two registers)
	PI32              // signed 32-bit (two registers)
)

// Point binds one telemetry key to a register.
type Point struct {
	Key      string  // the telemetry key emitted
	Register int     // starting holding-register address
	Type     PType   // how to decode the register(s)
	Scale    float64 // multiply the raw integer by this (e.g. 0.1 for a 0.1 W device)
	WordSwap bool    // true if a 32-bit value is little-word-first (low register first)
	Unit     string
}

// RegisterMap is the full point list for a device type.
type RegisterMap struct {
	Points []Point
}

// span returns the lowest register and the count needed to cover every point in one read, so the
// whole device is fetched in a single Modbus round trip rather than one per point.
func (m RegisterMap) span() (lo, count int, ok bool) {
	if len(m.Points) == 0 {
		return 0, 0, false
	}
	lo, hi := m.Points[0].Register, m.Points[0].Register
	for _, p := range m.Points {
		end := p.Register + p.width() - 1
		if p.Register < lo {
			lo = p.Register
		}
		if end > hi {
			hi = end
		}
	}
	return lo, hi - lo + 1, true
}

func (p Point) width() int {
	if p.Type == PU32 || p.Type == PI32 {
		return 2
	}
	return 1
}

// Read fetches the register span and decodes every point into a sample.
func (m RegisterMap) Read(r Reader) ([]codec.Sample, error) {
	lo, count, ok := m.span()
	if !ok {
		return nil, fmt.Errorf("modbus: empty register map")
	}
	regs, err := r.ReadHolding(lo, count)
	if err != nil {
		return nil, err
	}
	out := make([]codec.Sample, 0, len(m.Points))
	for _, p := range m.Points {
		i := p.Register - lo
		if i < 0 || i+p.width() > len(regs) {
			continue
		}
		scale := p.Scale
		if scale == 0 {
			scale = 1
		}
		out = append(out, codec.Sample{Key: p.Key, Num: decodeRaw(regs, i, p) * scale, IsNum: true})
	}
	return out, nil
}

func decodeRaw(regs []uint16, i int, p Point) float64 {
	switch p.Type {
	case PI16:
		return float64(int16(regs[i]))
	case PU32, PI32:
		hi, lo := regs[i], regs[i+1]
		if p.WordSwap {
			hi, lo = lo, hi
		}
		u := uint32(hi)<<16 | uint32(lo)
		if p.Type == PI32 {
			return float64(int32(u))
		}
		return float64(u)
	default: // PU16
		return float64(regs[i])
	}
}
