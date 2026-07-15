package main

import "testing"

// sumRegs is the data-block length a reader will find in the model's length register.
func sumRegs(pts []point) int {
	n := 0
	for _, p := range pts {
		n += p.regs
	}
	return n
}

// TestModelLengths pins each model's data-block length to the SunSpec spec. If a field is added,
// removed, or given the wrong width, the block length shifts and every point after it decodes from
// the wrong register — the exact silent corruption these constants guard against.
func TestModelLengths(t *testing.T) {
	cases := []struct {
		name string
		pts  []point
		want int
	}{
		{"common(1)", commonPoints(), 66},
		{"inverter(103)", inverterPoints(), 50},
		{"controls(123)", controlPoints(), 24},
		{"storage(124)", storagePoints(), 24},
		{"meter(203)", meterPoints(), 105},
	}
	for _, c := range cases {
		if got := sumRegs(c.pts); got != c.want {
			t.Errorf("%s length = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestChainWalkable builds the plant and walks the model chain exactly as a SunSpec reader would:
// verify the SunS marker, then read [id][len] and jump, and confirm the expected models appear in
// order and the chain terminates in 0xFFFF.
func TestChainWalkable(t *testing.T) {
	const base = 40000
	p := buildPlant(1, base, Config{PVPeakW: 10000, BattWh: 10000, InitSoC: 50, MinRsvPct: 20, Vnom: 230, Fnom: 50, Scenario: "sunny"})

	regs, ok := p.b.readRange(base, chainTop(base)-base)
	if !ok {
		t.Fatal("cannot read chain range")
	}
	if regs[0] != sunsHi || regs[1] != sunsLo {
		t.Fatalf("missing SunS marker: %#04x %#04x", regs[0], regs[1])
	}

	want := []int{1, 103, 123, 124, 203}
	off := 2
	for _, id := range want {
		gotID := int(regs[off])
		length := int(regs[off+1])
		if gotID != id {
			t.Fatalf("at offset %d: model id %d, want %d", off, gotID, id)
		}
		off += 2 + length
	}
	if regs[off] != endID {
		t.Fatalf("chain does not terminate in 0xFFFF, got %#04x at offset %d", regs[off], off)
	}
}

// TestControlWriteChangesPlant proves a curtailment write actually throttles the inverter — the
// read-back a guarded write confirms against is real, not cosmetic.
func TestControlWriteChangesPlant(t *testing.T) {
	const base = 40000
	p := buildPlant(1, base, Config{PVPeakW: 10000, LoadBaseW: 0, BattWh: 10000, InitSoC: 100, MinRsvPct: 20, Vnom: 230, Fnom: 50, Scenario: "sunny"})
	p.tod = 12 // solar noon: full PV

	// Battery full + no load => all PV would export. Run a tick uncurtailed.
	p.update(0.001)
	full := p.last.pvW
	if full < 5000 {
		t.Fatalf("expected strong noon production, got %.0fW", full)
	}

	// A client enables a 30%% export limit via model 123.
	p.ctl.setU16(p.b, "WMaxLimPct", 30)
	p.ctl.setU16(p.b, "WMaxLim_Ena", 1)
	p.update(0.001)
	if !p.last.curtailed {
		t.Fatal("inverter did not report curtailed after WMaxLim_Ena write")
	}
	if p.last.pvW > full*0.5 {
		t.Fatalf("curtailment did not reduce output: %.0fW (was %.0fW)", p.last.pvW, full)
	}
	if got := p.inv.getU16(p.b, "St"); got != stThrottled {
		t.Fatalf("operating state = %d, want THROTTLED(%d)", got, stThrottled)
	}
}
