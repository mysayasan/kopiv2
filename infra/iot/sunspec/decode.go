package sunspec

import (
	"errors"
	"math"
	"strings"

	"github.com/mysayasan/kopiv2/infra/iot/codec"
)

// ErrNoMarker means the "SunS" identifier was not found at a candidate base — the device is not
// SunSpec (or not at that base), and the caller should try another base or the manual map path.
var ErrNoMarker = errors.New("sunspec: no SunS marker")

const (
	markerHi = 0x5375 // "Su"
	markerLo = 0x6E53 // "nS"
)

// Block is one discovered model instance: its id, where it sits, and its raw data registers.
type Block struct {
	ID     int
	Offset int
	Length int
	Regs   []uint16 // the DATA block (offset 0 == first data register, after id+length)
}

// Common is the device nameplate from model 1.
type Common struct {
	Mfg, Model, Version, Serial string
}

// Discover finds the SunSpec chain by trying the three well-known base registers in turn, and
// returns the base that worked plus the decoded model chain.
func Discover(r Reader) (base int, blocks []Block, err error) {
	for _, b := range []int{40000, 50000, 0} {
		blocks, err = Walk(r, b)
		if err == nil {
			return b, blocks, nil
		}
	}
	return 0, nil, ErrNoMarker
}

// Walk reads the model chain starting at base: verify the marker, then repeatedly read [id][length]
// and the data block, following the length to the next model until the 0xFFFF terminator.
func Walk(r Reader, base int) ([]Block, error) {
	marker, err := r.ReadHolding(base, 2)
	if err != nil {
		return nil, err
	}
	if marker[0] != markerHi || marker[1] != markerLo {
		return nil, ErrNoMarker
	}
	var blocks []Block
	off := base + 2
	for i := 0; i < 128; i++ { // bounded so a corrupt chain cannot loop forever
		hdr, err := r.ReadHolding(off, 2)
		if err != nil {
			return nil, err
		}
		id, length := int(hdr[0]), int(hdr[1])
		if id == 0xFFFF {
			break
		}
		var regs []uint16
		if length > 0 {
			if regs, err = r.ReadHolding(off+2, length); err != nil {
				return nil, err
			}
		}
		blocks = append(blocks, Block{ID: id, Offset: off, Length: length, Regs: regs})
		off += 2 + length
	}
	return blocks, nil
}

// DecodeCommon pulls the nameplate strings out of a model-1 block.
func DecodeCommon(b Block) Common {
	return Common{
		Mfg:     str(b.Regs, 0, 16),
		Model:   str(b.Regs, 16, 16),
		Version: str(b.Regs, 40, 8),
		Serial:  str(b.Regs, 48, 16),
	}
}

// DecodeDevice decodes every KNOWN model in a chain into samples, prefixing each key with the
// model's role ("inv_ac_power", "grid_ac_power", "batt_soc", ...). The prefix is what keeps a
// hybrid inverter's inverter block and its built-in grid meter from colliding on "ac_power".
func DecodeDevice(blocks []Block) []codec.Sample {
	var out []codec.Sample
	for _, b := range blocks {
		def, ok := registry[b.ID]
		if !ok {
			continue // unknown model — skip, do not guess
		}
		for _, f := range def.Fields {
			s, ok := decodeField(b.Regs, f)
			if !ok {
				continue
			}
			s.Key = def.Role + "_" + s.Key
			out = append(out, s)
		}
	}
	return out
}

// Roles reports which device roles a chain contains (inverter / meter / battery), for the workspace
// to suggest how the device slots into a system.
func Roles(blocks []Block) []string {
	seen := map[string]bool{}
	var roles []string
	for _, b := range blocks {
		if def, ok := registry[b.ID]; ok && def.Role != "ctl" && !seen[def.Role] {
			seen[def.Role] = true
			roles = append(roles, def.Role)
		}
	}
	return roles
}

func decodeField(regs []uint16, f Field) (codec.Sample, bool) {
	if f.Off < 0 || f.Off >= len(regs) {
		return codec.Sample{}, false
	}
	var raw float64
	switch f.Kind {
	case KI16:
		raw = float64(int16(regs[f.Off]))
	case KAcc32:
		if f.Off+1 >= len(regs) {
			return codec.Sample{}, false
		}
		raw = float64(uint32(regs[f.Off])<<16 | uint32(regs[f.Off+1]))
	default: // KU16, KEnum
		raw = float64(regs[f.Off])
	}
	if f.SFOff >= 0 && f.SFOff < len(regs) {
		raw *= math.Pow10(int(int16(regs[f.SFOff])))
	}
	return codec.Sample{Key: f.Key, Num: raw, IsNum: true}, true
}

// str decodes a fixed-length SunSpec string (2 chars/register, big-endian, null-padded).
func str(regs []uint16, off, n int) string {
	if off+n > len(regs) {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		w := regs[off+i]
		b = append(b, byte(w>>8), byte(w&0xff))
	}
	return strings.TrimRight(string(b), "\x00 ")
}
