package modbus

import (
	"fmt"
	"math"
	"sort"

	"github.com/mysayasan/kopiv2/infra/iot/codec"
)

// inputReader is the optional extension a Reader implements to also read INPUT registers (fn 4).
// Holding-only devices (and the SunSpec decoder) need only Reader.ReadHolding; the cheaper meters
// and many hybrids (Eastron SDM630, Sungrow SH) expose their measurements as input registers, and
// a register map that binds any such point requires a reader that can reach them.
type inputReader interface {
	ReadInput(addr, qty int) ([]uint16, error)
}

// maxReadRegisters is the Modbus limit on a single read (fn 3/4): 125 registers. A vendor map
// whose points are scattered wider than that cannot be one round trip, so Read clusters them.
const maxReadRegisters = 125

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
	PF32              // IEEE-754 32-bit float (two registers) — the encoding cheap meters use
)

// Point binds one telemetry key to a register.
type Point struct {
	Key      string  // the telemetry key emitted
	Register int     // starting register address
	Type     PType   // how to decode the register(s)
	Scale    float64 // multiply the raw integer by this (e.g. 0.1 for a 0.1 W device)
	WordSwap bool    // true if a 32-bit value is little-word-first (low register first)
	// Input reads this point from INPUT registers (fn 4) instead of holding registers (fn 3).
	// Vendors split their maps: Huawei's is all holding, but an Eastron meter and a Sungrow SH
	// keep measurements in the input bank. Points of different Input never share a read.
	Input bool
	Unit  string
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
	if p.Type == PU32 || p.Type == PI32 || p.Type == PF32 {
		return 2
	}
	return 1
}

// clusters groups the points into read windows each no wider than maxReadRegisters. A tightly
// packed map (a cheap inverter's flat block) yields ONE cluster and one round trip, the common
// case. A device that scatters its blocks across the address space — a Huawei SUN2000 keeps its
// inverter block near register 32000 and its battery near 37000 — yields a cluster per block, so
// it is read in a few bounded requests rather than one 5,000-register read the protocol forbids.
func (m RegisterMap) clusters() [][]Point {
	if len(m.Points) == 0 {
		return nil
	}
	// A holding read (fn 3) and an input read (fn 4) are different function codes over the same
	// address space, so a point in each bank can never share one round trip even at the same
	// address. Partition by bank first, then window each partition by the 125-register limit.
	var holding, input []Point
	for _, p := range m.Points {
		if p.Input {
			input = append(input, p)
		} else {
			holding = append(holding, p)
		}
	}
	var out [][]Point
	out = append(out, windowClusters(holding)...)
	out = append(out, windowClusters(input)...)
	return out
}

// windowClusters groups one bank's points into read windows each no wider than maxReadRegisters.
func windowClusters(pts []Point) [][]Point {
	if len(pts) == 0 {
		return nil
	}
	pts = append([]Point(nil), pts...)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Register < pts[j].Register })
	var out [][]Point
	cur := []Point{pts[0]}
	lo := pts[0].Register
	for _, p := range pts[1:] {
		end := p.Register + p.width() - 1
		if end-lo+1 > maxReadRegisters {
			out = append(out, cur)
			cur, lo = []Point{p}, p.Register
			continue
		}
		cur = append(cur, p)
	}
	return append(out, cur)
}

// Read fetches each cluster and decodes every point into a sample. Points whose registers all fit
// within one 125-register window cost a single round trip; a scattered map costs one per cluster.
func (m RegisterMap) Read(r Reader) ([]codec.Sample, error) {
	clusters := m.clusters()
	if len(clusters) == 0 {
		return nil, fmt.Errorf("modbus: empty register map")
	}
	out := make([]codec.Sample, 0, len(m.Points))
	for _, cl := range clusters {
		lo, hi := cl[0].Register, cl[0].Register
		for _, p := range cl {
			if end := p.Register + p.width() - 1; end > hi {
				hi = end
			}
		}
		var regs []uint16
		var err error
		if cl[0].Input {
			// clusters() guarantees a cluster is homogeneous in Input, so cl[0] decides the bank.
			ir, ok := r.(inputReader)
			if !ok {
				return nil, fmt.Errorf("modbus: register map binds input-register (fn4) points but the reader cannot read input registers")
			}
			regs, err = ir.ReadInput(lo, hi-lo+1)
		} else {
			regs, err = r.ReadHolding(lo, hi-lo+1)
		}
		if err != nil {
			return nil, err
		}
		for _, p := range cl {
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
	}
	return out, nil
}

func decodeRaw(regs []uint16, i int, p Point) float64 {
	switch p.Type {
	case PI16:
		return float64(int16(regs[i]))
	case PU32, PI32, PF32:
		hi, lo := regs[i], regs[i+1]
		if p.WordSwap {
			hi, lo = lo, hi
		}
		u := uint32(hi)<<16 | uint32(lo)
		switch p.Type {
		case PI32:
			return float64(int32(u))
		case PF32:
			return float64(math.Float32frombits(u))
		default:
			return float64(u)
		}
	default: // PU16
		return float64(regs[i])
	}
}
