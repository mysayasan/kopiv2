package main

// plant.go is the simulated solar site: a PV array, a hybrid inverter, a battery, and a grid
// meter, wired together by simple physics and encoded into the SunSpec models every tick.
//
// The point of the physics is not fidelity — it is that the numbers MOVE and stay internally
// consistent (PV + battery + grid always balance the load), so a driver reading the registers
// sees a believable day: production rising to a noon peak, the battery charging on surplus and
// discharging after dark, the meter swinging between import and export. And crucially the CONTROL
// registers a client writes (export curtailment, battery mode) actually change what happens, so
// guarded-write-with-read-back-confirm can be exercised end to end.

import (
	"fmt"
	"math"
	"math/rand"
)

// Scale factors, fixed for the life of the run. Stored value = round(actual / 10^sf).
const (
	sfCurrent = -2 // 0.01 A
	sfVolt    = -1 // 0.1 V
	sfPower   = 0  // 1 W  (keep 1:1 so a reader's decoded W == the real watts; see README)
	sfFreq    = -2 // 0.01 Hz
	sfEnergy  = 0  // 1 Wh
	sfTemp    = -1 // 0.1 C
	sfPct     = 0  // 1 %
	sfPF      = -3 // 0.001 (power factor)
	sfBattV   = -1 // 0.1 V
)

// Inverter operating state (SunSpec model 103 "St" enum).
const (
	stOff      = 1
	stSleeping = 2
	stStarting = 3
	stMPPT     = 4 // producing normally
	stThrottled = 5 // curtailed by a WMaxLimPct write
	stStandby  = 8
)

// Battery charge status (SunSpec model 124 "ChaSt" enum).
const (
	chOff         = 1
	chEmpty       = 2
	chDischarging = 3
	chCharging    = 4
	chFull        = 5
	chHolding     = 6
)

// Config is the shape of the simulated plant, from the CLI flags.
type Config struct {
	PVPeakW   float64 // PV array / inverter nameplate, W
	LoadBaseW float64 // baseline house load, W
	BattWh    float64 // usable battery energy, Wh
	InitSoC   float64 // starting state of charge, %
	MinRsvPct float64 // reserve floor the battery will not discharge below, %
	Vnom      float64 // nominal per-phase voltage, V
	Fnom      float64 // nominal frequency, Hz
	Scenario  string  // sunny | cloudy | storm | night | outage
	Common    CommonInfo
}

// CommonInfo populates SunSpec model 1 (the device nameplate).
type CommonInfo struct {
	Mfg, Model, Version, Serial string
}

// Plant holds the placed models and the mutable simulation state.
type Plant struct {
	unitID                   byte
	b                        *Bank
	common, inv, ctl, sto, mtr *Model
	cfg                      Config

	tod   float64 // time of day, hours [0,24)
	soc   float64 // battery state of charge, %
	whInv float64 // inverter lifetime AC production, Wh
	whImp float64 // meter cumulative grid import, Wh
	whExp float64 // meter cumulative grid export, Wh

	// last-tick snapshot, for the status line
	last snapshot
}

type snapshot struct {
	pvW, loadW, battW, gridW float64
	curtailed                bool
	invSt, chSt              int
}

// buildPlant lays out the SunSpec chain into a fresh bank and returns the plant, ready to tick.
func buildPlant(unit byte, base int, cfg Config) *Plant {
	b := newBank(base + 400) // chain is ~283 regs; 400 leaves generous headroom

	// "SunS" marker, then the model chain, then the 0xFFFF terminator.
	b.set(base, sunsHi)
	b.set(base+1, sunsLo)
	cur := base + 2

	common, cur := place(b, cur, 1, commonPoints())
	inv, cur := place(b, cur, 103, inverterPoints())
	ctl, cur := place(b, cur, 123, controlPoints())
	sto, cur := place(b, cur, 124, storagePoints())
	mtr, cur := place(b, cur, 203, meterPoints())
	b.set(cur, endID)
	b.set(cur+1, 0)

	p := &Plant{
		unitID: unit,
		b:      b, common: common, inv: inv, ctl: ctl, sto: sto, mtr: mtr, cfg: cfg,
		tod: 6.0, // start at dawn so a fast run shows a full sunrise-to-dark arc
		soc: cfg.InitSoC,
	}
	p.setStatics()
	return p
}

// Plant satisfies the Device interface (see devices.go).
func (p *Plant) unit() byte  { return p.unitID }
func (p *Plant) bank() *Bank { return p.b }
func (p *Plant) label() string {
	return fmt.Sprintf("unit %d  hybrid inverter (%s %s)", p.unitID, p.cfg.Common.Mfg, p.cfg.Common.Model)
}
func (p *Plant) describe(addr int) string { return p.describeAddr(addr) }

// chainTop returns the address just past the terminator, for logging the served range.
func chainTop(base int) int {
	// SunS(2) + [1]:68 + [103]:52 + [123]:26 + [124]:26 + [203]:107 + end(2)
	return base + 2 + 68 + 52 + 26 + 26 + 107 + 2
}

// setStatics writes everything that never changes: nameplate strings, scale factors, and the
// battery's declared limits. Scale factors MUST be set or a reader divides by a zero-not-set SF
// and every value is wrong — a real footgun, so they live in one obvious place.
func (p *Plant) setStatics() {
	b := p.b
	c := p.common
	c.setStr(b, "Mn", p.cfg.Common.Mfg)
	c.setStr(b, "Md", p.cfg.Common.Model)
	c.setStr(b, "Vr", p.cfg.Common.Version)
	c.setStr(b, "SN", p.cfg.Common.Serial)
	c.setU16(b, "DA", 1)

	inv := p.inv
	inv.setSF(b, "A_SF", sfCurrent)
	inv.setSF(b, "V_SF", sfVolt)
	inv.setSF(b, "W_SF", sfPower)
	inv.setSF(b, "Hz_SF", sfFreq)
	inv.setSF(b, "VA_SF", sfPower)
	inv.setSF(b, "VAr_SF", sfPower)
	inv.setSF(b, "PF_SF", sfPF)
	inv.setSF(b, "WH_SF", sfEnergy)
	inv.setSF(b, "DCA_SF", sfCurrent)
	inv.setSF(b, "DCV_SF", sfVolt)
	inv.setSF(b, "DCW_SF", sfPower)
	inv.setSF(b, "Tmp_SF", sfTemp)

	ctl := p.ctl
	ctl.setSF(b, "WMaxLimPct_SF", sfPct) // percent, 1:1
	// A fresh device is NOT curtailing: full output, limit disabled.
	ctl.setU16(b, "WMaxLimPct", 100)
	ctl.setU16(b, "WMaxLim_Ena", 0)

	sto := p.sto
	sto.setSF(b, "WChaMax_SF", sfPower)
	sto.setSF(b, "MinRsvPct_SF", sfPct)
	sto.setSF(b, "ChaState_SF", sfPct)
	sto.setSF(b, "InBatV_SF", sfBattV)
	sto.setScaledU16(b, "WChaMax", p.maxChargeW(), sfPower)
	sto.setScaledU16(b, "MinRsvPct", p.cfg.MinRsvPct, sfPct)
	// StorCtl_Mod bit0 = charging enabled, bit1 = discharging enabled. Default 3 = normal auto.
	// A client that clears bit1 makes the battery HOLD (never discharge) — a visible control effect.
	sto.setU16(b, "StorCtl_Mod", 0x3)

	mtr := p.mtr
	mtr.setSF(b, "A_SF", sfCurrent)
	mtr.setSF(b, "V_SF", sfVolt)
	mtr.setSF(b, "W_SF", sfPower)
	mtr.setSF(b, "Hz_SF", sfFreq)
	mtr.setSF(b, "VA_SF", sfPower)
	mtr.setSF(b, "VAR_SF", sfPower)
	mtr.setSF(b, "PF_SF", sfPF)
	mtr.setSF(b, "TotWh_SF", sfEnergy)
}

func (p *Plant) maxChargeW() float64 {
	return p.cfg.BattWh * 0.5 // 0.5C charge/discharge ceiling
}

// describeAddr names a written register if it belongs to one of the control models, so a client's
// curtailment or battery-mode write can be logged meaningfully. Empty for everything else.
func (p *Plant) describeAddr(addr int) string {
	for _, m := range []struct {
		id int
		m  *Model
	}{{123, p.ctl}, {124, p.sto}} {
		for name, a := range m.m.addr {
			if a == addr {
				return fmt.Sprintf("model %d.%s", m.id, name)
			}
		}
	}
	return ""
}

// update advances the simulation by dt hours and re-encodes every telemetry register. It takes
// the bank lock ONCE for the whole batch rather than per register.
func (p *Plant) update(dt float64) {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()

	p.tod = math.Mod(p.tod+dt, 24)

	// --- production, consumption, and the balance between them ---------------------------
	pvDC := p.pvCurve() * p.pvFactor()
	acFromPV := pvDC * 0.97 // inverter efficiency
	load := p.loadCurve()

	// Export curtailment: if a client has enabled the WMaxLimPct control, the inverter cannot
	// deliver more than that percent of nameplate. Excess PV is clipped (curtailed).
	curtailed := false
	if p.ctl.getU16(p.b, "WMaxLim_Ena") == 1 {
		cap := p.cfg.PVPeakW * 0.97 * float64(p.ctl.getU16(p.b, "WMaxLimPct")) / 100
		if acFromPV > cap {
			acFromPV = cap
			curtailed = true
		}
	}

	storMode := p.sto.getU16(p.b, "StorCtl_Mod")
	chargeOK := storMode&0x1 != 0
	dischargeOK := storMode&0x2 != 0

	// battNet: + charging into the battery, - discharging out of it.
	surplus := acFromPV - load
	var battNet float64
	switch {
	case surplus > 0 && chargeOK && p.soc < 100:
		battNet = math.Min(surplus, p.maxChargeW())
		if room := (100 - p.soc) / 100 * p.cfg.BattWh; battNet*dt > room {
			battNet = room / dt
		}
	case surplus < 0 && dischargeOK && p.soc > p.cfg.MinRsvPct:
		d := math.Min(-surplus, p.maxChargeW())
		if avail := (p.soc - p.cfg.MinRsvPct) / 100 * p.cfg.BattWh; d*dt > avail {
			d = avail / dt
		}
		battNet = -d
	}

	invAC := acFromPV - battNet // what the inverter puts on the AC bus (charging subtracts)
	grid := load - invAC        // + importing from grid, - exporting to grid
	if !p.gridUp() {
		grid = 0 // outage: the meter sees nothing; the imbalance is simply unmet
	}

	// --- integrate the accumulators ------------------------------------------------------
	p.soc = clampF(p.soc+battNet*dt/p.cfg.BattWh*100, 0, 100)
	p.whInv += math.Max(0, invAC) * dt
	p.whImp += math.Max(0, grid) * dt
	p.whExp += math.Max(0, -grid) * dt

	// --- encode the inverter (model 103) -------------------------------------------------
	v := p.cfg.Vnom + noise(0.4)
	f := p.cfg.Fnom + noise(0.02)
	p.encodeAC(p.inv, "A", "AphA", "AphB", "AphC", "PhVphA", "PhVphB", "PhVphC",
		"W", "VA", "VAr", "PF", "Hz", invAC, v, f)
	p.inv.setScaledU32(p.b, "WH", p.whInv, sfEnergy)
	dcv := 380 + noise(4)
	p.inv.setScaledU16(p.b, "DCV", dcv, sfVolt)
	p.inv.setScaledU16(p.b, "DCA", pvDC/math.Max(dcv, 1), sfCurrent)
	p.inv.setScaledI16(p.b, "DCW", pvDC, sfPower)
	p.inv.setScaledI16(p.b, "TmpCab", 25+invAC/math.Max(p.cfg.PVPeakW, 1)*22+noise(0.5), sfTemp)

	invSt := stMPPT
	switch {
	case curtailed:
		invSt = stThrottled
	case pvDC < 10 && math.Abs(battNet) < 10:
		invSt = stSleeping
	}
	p.inv.setU16(p.b, "St", uint16(invSt))

	// --- encode the meter (model 203) ----------------------------------------------------
	p.encodeAC(p.mtr, "A", "", "", "", "PhV", "", "",
		"W", "VA", "VAR", "PF", "Hz", grid, v, f)
	p.mtr.setScaledU32(p.b, "TotWhImp", p.whImp, sfEnergy)
	p.mtr.setScaledU32(p.b, "TotWhExp", p.whExp, sfEnergy)

	// --- encode the battery (model 124) --------------------------------------------------
	p.sto.setScaledU16(p.b, "ChaState", p.soc, sfPct)
	p.sto.setScaledU16(p.b, "InBatV", 48+p.soc/100*6, sfBattV)
	chSt := chHolding
	switch {
	case p.soc >= 99:
		chSt = chFull
	case p.soc <= p.cfg.MinRsvPct+0.5:
		chSt = chEmpty
	case battNet > 10:
		chSt = chCharging
	case battNet < -10:
		chSt = chDischarging
	}
	p.sto.setU16(p.b, "ChaSt", uint16(chSt))

	p.last = snapshot{pvW: acFromPV, loadW: load, battW: battNet, gridW: grid,
		curtailed: curtailed, invSt: invSt, chSt: chSt}
}

// encodeAC writes a three-phase AC block (currents, voltages, powers, PF, frequency) for a signed
// real power `w`. Phase names left "" are skipped, which is how the single-value meter block reuses
// the same helper as the inverter's per-phase block.
func (p *Plant) encodeAC(m *Model, aTot, aA, aB, aC, vA, vB, vC, wKey, vaKey, varKey, pfKey, hzKey string, w, vph, f float64) {
	mag := math.Abs(w)
	aTotal := mag / math.Max(vph*3, 1)
	m.setScaledI16(p.b, wKey, w, sfPower)
	m.setScaledI16(p.b, vaKey, mag, sfPower)
	m.setScaledI16(p.b, varKey, mag*0.05, sfPower)
	m.setScaledI16(p.b, pfKey, 1.0, sfPF)
	m.setScaledU16(p.b, hzKey, f, sfFreq)
	m.setScaledU16(p.b, aTot, aTotal, sfCurrent)
	for _, k := range []string{aA, aB, aC} {
		if k != "" {
			m.setScaledU16(p.b, k, aTotal/3, sfCurrent)
		}
	}
	for _, k := range []string{vA, vB, vC} {
		if k != "" {
			m.setScaledU16(p.b, k, vph, sfVolt)
		}
	}
}

// --- environment curves --------------------------------------------------------------------

// pvCurve is a sunrise-to-sunset bell, zero in the dark.
func (p *Plant) pvCurve() float64 {
	const sunrise, sunset = 6.0, 18.0
	if p.tod < sunrise || p.tod > sunset {
		return 0
	}
	x := (p.tod - sunrise) / (sunset - sunrise) // 0..1 across the day
	return p.cfg.PVPeakW * math.Sin(math.Pi*x)
}

// loadCurve is a baseline plus a morning and an evening bump.
func (p *Plant) loadCurve() float64 {
	l := p.cfg.LoadBaseW
	l += 700 * gauss(p.tod, 7.5, 1.5)  // breakfast
	l += 1400 * gauss(p.tod, 19.5, 2.0) // evening peak
	return l + noise(30)
}

// pvFactor and gridUp apply the scenario. Weather scales PV; an outage drops the grid.
func (p *Plant) pvFactor() float64 {
	switch p.cfg.Scenario {
	case "cloudy":
		return 0.4
	case "storm":
		return 0.1 + 0.6*rand.Float64()
	case "night":
		return 0
	default: // sunny, outage
		return 1.0
	}
}

func (p *Plant) gridUp() bool { return p.cfg.Scenario != "outage" }

// status is the one-line live readout printed while the sim runs.
func (p *Plant) status() string {
	s := p.last
	return fmt.Sprintf("%02d:%02d  PV %5.0fW  Load %5.0fW  Batt %+5.0fW (SoC %4.1f%%)  Grid %+6.0fW  %s%s",
		int(p.tod), int(math.Mod(p.tod*60, 60)),
		s.pvW, s.loadW, s.battW, p.soc, s.gridW,
		invStateName(s.invSt), curtailFlag(s.curtailed))
}

func invStateName(st int) string {
	switch st {
	case stThrottled:
		return "THROTTLED"
	case stSleeping:
		return "SLEEPING"
	default:
		return "MPPT"
	}
}

func curtailFlag(c bool) string {
	if c {
		return " [curtailed]"
	}
	return ""
}

// --- small math helpers --------------------------------------------------------------------

func gauss(x, mu, sig float64) float64 {
	d := (x - mu) / sig
	return math.Exp(-0.5 * d * d)
}

func noise(amp float64) float64 { return (rand.Float64()*2 - 1) * amp }

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
